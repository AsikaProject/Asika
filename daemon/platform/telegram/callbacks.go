package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
)

func (b *Bot) handleCallback(c telebot.Context) error {
	cb := c.Callback()
	if cb == nil {
		return nil
	}
	if !b.isAdmin(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "Access denied."})
	}
	data := cb.Data
	if len(data) > 0 && data[0] == '\f' {
		if idx := strings.Index(data, "|"); idx >= 0 {
			data = data[idx+1:]
		}
	}
	if strings.HasPrefix(data, "prs_page:") {
		parts := strings.SplitN(data, ":", 3)
		if len(parts) == 3 {
			rg := strings.ReplaceAll(parts[1], "%7C", "|")
			page, _ := strconv.Atoi(parts[2])
			prs := b.fetchPRsForGroup(rg)
			if len(prs) > 0 {
				return b.editPRsPage(c, rg, prs, page)
			}
		}
		return c.Respond(&telebot.CallbackResponse{Text: "No PRs found."})
	}
	if strings.HasPrefix(data, "close_reason:") {
		return b.handleCloseReasonCallback(c, data)
	}
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid callback."})
	}
	action := parts[0]
	payload := parts[1]
	idx := strings.LastIndex(payload, "#")
	if idx < 0 {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid payload."})
	}
	repoGroup := payload[:idx]
	prID := payload[idx+1:]
	pr, err := commonutil.GetPRByID(repoGroup, prID)
	if err != nil || pr == nil {
		return c.Respond(&telebot.CallbackResponse{Text: "PR not found."})
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Repo group not found."})
	}
	client := b.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return c.Respond(&telebot.CallbackResponse{Text: "No platform client."})
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	switch action {
	case "approve":
		return b.callbackApprove(c, pr, client, owner, repo, ctx)
	case "close":
		return b.callbackClose(c, pr, client, owner, repo, ctx)
	case "reopen":
		return b.callbackReopen(c, pr, client, owner, repo, ctx)
	case "spam":
		return b.callbackSpam(c, pr, client, owner, repo, ctx)
	case "revert":
		return b.callbackRevert(c, pr, client, owner, repo, ctx)
	default:
		return c.Respond(&telebot.CallbackResponse{Text: "Unknown action."})
	}
}

func (b *Bot) handleCloseReasonCallback(c telebot.Context, data string) error {
	rest := strings.TrimPrefix(data, "close_reason:")
	reasonIdx := strings.Index(rest, ":")
	if reasonIdx < 0 {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid close reason callback."})
	}
	reason := rest[:reasonIdx]
	payload := rest[reasonIdx+1:]
	idx := strings.LastIndex(payload, "#")
	if idx < 0 {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid payload."})
	}
	repoGroup := payload[:idx]
	prID := payload[idx+1:]
	pr, err := commonutil.GetPRByID(repoGroup, prID)
	if err != nil || pr == nil {
		return c.Respond(&telebot.CallbackResponse{Text: "PR not found."})
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Repo group not found."})
	}
	client := b.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return c.Respond(&telebot.CallbackResponse{Text: "No platform client."})
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		msg := fmt.Sprintf("Failed: %v", err)
		if len(msg) > 200 {
			msg = msg[:197] + "..."
		}
		return c.Respond(&telebot.CallbackResponse{Text: msg})
	}
	if reason != "custom" && reason != "" {
		if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, reason, ""); err != nil {
			slog.Warn("telegram bot: failed to add close reason label", "error", err, "label", reason, "pr_number", pr.PRNumber)
		}
	}
	pr.State = "closed"
	pr.CloseReason = reason
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "close_reason": reason,
	})
	return c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("Closed ❌ Reason: %s", reason)})
}

func (b *Bot) callbackApprove(c telebot.Context, pr *models.PRRecord, client platforms.PlatformClient, owner, repo string, ctx context.Context) error {
	if pr.State == "merged" || pr.State == "closed" {
		return c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("Cannot approve %s PR.", pr.State)})
	}
	if err := client.ApprovePR(ctx, owner, repo, pr.PRNumber); err != nil {
		msg := fmt.Sprintf("Failed: %v", err)
		if len(msg) > 200 {
			msg = msg[:197] + "..."
		}
		return c.Respond(&telebot.CallbackResponse{Text: msg})
	}
	pr.IsApproved = true
	actor := c.Sender().Username
	if actor == "" {
		actor = fmt.Sprintf("%d", c.Sender().ID)
	}
	pr.Events = append(pr.Events, models.PREvent{Timestamp: time.Now(), Action: "approved", Actor: actor})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	addedToQueue := false
	if b.queueMgr != nil {
		if pr.State != "" && pr.State != "open" {
			slog.Info("telegram bot: skipping queue add for non-open PR", "pr_number", pr.PRNumber, "state", pr.State)
		} else {
			if err := b.queueMgr.AddToQueue(pr); err != nil {
				slog.Warn("telegram bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
			} else {
				addedToQueue = true
				go b.queueMgr.CheckQueue()
			}
		}
	}
	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "added_to_queue": addedToQueue,
	})
	if addedToQueue {
		return c.Respond(&telebot.CallbackResponse{Text: "Approved ✅ Added to queue."})
	}
	return c.Respond(&telebot.CallbackResponse{Text: "Approved ✅"})
}

func (b *Bot) callbackClose(c telebot.Context, pr *models.PRRecord, client platforms.PlatformClient, owner, repo string, ctx context.Context) error {
	if pr.State == "closed" || pr.State == "merged" {
		return c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("PR is already %s.", pr.State)})
	}
	cfg := config.Current()
	reasons := []string{"duplicate", "invalid", "wontfix", "stale"}
	if cfg != nil && len(cfg.CloseReasons.Reasons) > 0 {
		reasons = cfg.CloseReasons.Reasons
	}
	selector := &telebot.ReplyMarkup{}
	payload := fmt.Sprintf("%s#%s", pr.RepoGroup, pr.ID)
	var rows []telebot.Row
	var currentRow []telebot.Btn
	for _, reason := range reasons {
		label := strings.ReplaceAll(reason, "_", " ")
		btn := selector.Data("❌ "+label, "close_reason", fmt.Sprintf("close_reason:%s:%s", reason, payload))
		currentRow = append(currentRow, btn)
		if len(currentRow) == 2 {
			rows = append(rows, selector.Row(currentRow...))
			currentRow = nil
		}
	}
	btnCustom := selector.Data("✏️ Custom", "close_reason", fmt.Sprintf("close_reason:custom:%s", payload))
	currentRow = append(currentRow, btnCustom)
	if len(currentRow) == 2 {
		rows = append(rows, selector.Row(currentRow...))
		currentRow = nil
	}
	if len(currentRow) > 0 {
		rows = append(rows, selector.Row(currentRow...))
	}
	selector.Inline(rows...)
	return c.Edit("Select a close reason for the PR:", &telebot.SendOptions{ReplyMarkup: selector})
}

func (b *Bot) callbackReopen(c telebot.Context, pr *models.PRRecord, client platforms.PlatformClient, owner, repo string, ctx context.Context) error {
	if pr.State == "merged" {
		return c.Respond(&telebot.CallbackResponse{Text: "Cannot reopen merged PR."})
	}
	if pr.State == "open" {
		return c.Respond(&telebot.CallbackResponse{Text: "PR is already open."})
	}
	if err := client.ReopenPR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		msg := fmt.Sprintf("Failed: %v", err)
		if len(msg) > 200 {
			msg = msg[:197] + "..."
		}
		return c.Respond(&telebot.CallbackResponse{Text: msg})
	}
	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram",
	})
	return c.Respond(&telebot.CallbackResponse{Text: "Reopened 🔄"})
}

func (b *Bot) callbackSpam(c telebot.Context, pr *models.PRRecord, client platforms.PlatformClient, owner, repo string, ctx context.Context) error {
	if pr.State == "closed" || pr.State == "merged" {
		return c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("PR is already %s.", pr.State)})
	}
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram",
	})
	existing, _ := db.GetSpamAuthor(pr.Author, pr.Platform)
	if existing != nil {
		existing.Count++
		existing.LastSeen = time.Now()
		db.PutSpamAuthor(existing)
	} else {
		db.PutSpamAuthor(&models.SpamAuthor{
			Author:    pr.Author,
			Platform:  pr.Platform,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
		})
	}
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		msg := fmt.Sprintf("Marked spam but close failed: %v", err)
		if len(msg) > 200 {
			msg = msg[:197] + "..."
		}
		return c.Respond(&telebot.CallbackResponse{Text: msg})
	}
	return c.Respond(&telebot.CallbackResponse{Text: "Marked as spam 🚫"})
}

func (b *Bot) callbackRevert(c telebot.Context, pr *models.PRRecord, client platforms.PlatformClient, owner, repo string, ctx context.Context) error {
	if pr.State != "merged" {
		return c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("Cannot revert %s PR.", pr.State)})
	}
	revertPR, err := client.RevertPR(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		db.AppendAuditLog("error", "PR revert failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		msg := fmt.Sprintf("Failed: %v", err)
		if len(msg) > 200 {
			msg = msg[:197] + "..."
		}
		return c.Respond(&telebot.CallbackResponse{Text: msg})
	}
	actor := c.Sender().Username
	if actor == "" {
		actor = fmt.Sprintf("%d", c.Sender().ID)
	}
	if revertPR != nil {
		revertPR.RepoGroup = pr.RepoGroup
		revertPR.Platform = pr.Platform
		revertPR.ID = fmt.Sprintf("%d", revertPR.PRNumber)
		revertPRData, _ := json.Marshal(revertPR)
		revertKey := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, revertPR.PRNumber)
		db.PutPRWithIndex(revertKey, revertPRData, revertPR.ID, pr.RepoGroup, revertPR.PRNumber)
		if b.queueMgr != nil {
			if err := b.queueMgr.AddToQueue(revertPR); err != nil {
				slog.Warn("telegram bot: failed to add revert PR to queue", "error", err, "pr_number", revertPR.PRNumber)
			} else {
				go b.queueMgr.CheckQueue()
			}
		}
		if err := client.CommentPR(ctx, owner, repo, pr.PRNumber,
			fmt.Sprintf("This PR has been reverted by %s. Revert PR: #%d", actor, revertPR.PRNumber)); err != nil {
			slog.Warn("telegram bot: failed to comment on reverted PR", "error", err, "pr_number", pr.PRNumber)
		}
		if b.notifier != nil {
			title := fmt.Sprintf("[Revert] PR #%d reverted", pr.PRNumber)
			body := fmt.Sprintf("PR #%d \"%s\" was reverted by %s.\nRevert PR: #%d\nRepo: %s | Platform: %s",
				pr.PRNumber, pr.Title, actor, revertPR.PRNumber, pr.RepoGroup, pr.Platform)
			b.notifier.Send(ctx, title, body)
		}
		db.AppendAuditLog("info", "PR reverted", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "revert_pr_number": revertPR.PRNumber,
		})
		return c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("Reverted ↩️ Revert PR: #%d", revertPR.PRNumber)})
	}
	db.AppendAuditLog("info", "PR revert requested", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram",
	})
	return c.Respond(&telebot.CallbackResponse{Text: "Revert requested ↩️"})
}

func (b *Bot) handleText(c telebot.Context) error {
	text := strings.TrimSpace(c.Text())
	lower := strings.ToLower(text)
	switch lower {
	case "help", "menu":
		return b.handleHelp(c)
	case "prs", "list":
		return b.handleListPRs(c)
	case "queue":
		return b.handleShowQueue(c)
	case "config":
		return b.handleShowConfig(c)
	case "version", "v":
		return b.handleVersion(c)
	}
	c.Send("Unknown command. Try /help for available commands.")
	return nil
}

func (b *Bot) sendPRsPage(c telebot.Context, repoGroup string, prs []models.PRRecord, page int) error {
	totalPages := (len(prs) + prsPerPage - 1) / prsPerPage
	if page >= totalPages {
		page = totalPages - 1
	}
	if page < 0 {
		page = 0
	}
	start := page * prsPerPage
	end := start + prsPerPage
	if end > len(prs) {
		end = len(prs)
	}
	pagePRs := prs[start:end]
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>PRs in %s</b> (%d/%d)\n\n", html.EscapeString(repoGroup), page+1, totalPages))
	for _, pr := range pagePRs {
		statusEmoji := "🔵"
		switch pr.State {
		case "merged":
			statusEmoji = "🟣"
		case "closed":
			statusEmoji = "🔴"
		case "spam":
			statusEmoji = "⚠️"
		}
		title := pr.Title
		if len(title) > 35 {
			title = title[:32] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s <b>#%d</b> %s — <i>%s</i> [%s]\n",
			statusEmoji, pr.PRNumber, html.EscapeString(title), html.EscapeString(pr.Author), pr.State))
	}
	markup := &telebot.ReplyMarkup{}
	escapeRG := strings.ReplaceAll(repoGroup, "|", "%7C")
	var btns []telebot.Btn
	if page > 0 {
		btns = append(btns, markup.Data(fmt.Sprintf("‹ Page %d", page), "prs_page", fmt.Sprintf("prs_page:%s:%d", escapeRG, page-1)))
	}
	btns = append(btns, markup.Data(fmt.Sprintf("📄 %d/%d", page+1, totalPages), "prs_page", fmt.Sprintf("prs_page:%s:%d", escapeRG, page)))
	if page < totalPages-1 {
		btns = append(btns, markup.Data(fmt.Sprintf("Page %d ›", page+2), "prs_page", fmt.Sprintf("prs_page:%s:%d", escapeRG, page+1)))
	}
	markup.Inline(markup.Row(btns...))
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML, ReplyMarkup: markup})
}

func (b *Bot) editPRsPage(c telebot.Context, repoGroup string, prs []models.PRRecord, page int) error {
	totalPages := (len(prs) + prsPerPage - 1) / prsPerPage
	if page >= totalPages {
		page = totalPages - 1
	}
	if page < 0 {
		page = 0
	}
	start := page * prsPerPage
	end := start + prsPerPage
	if end > len(prs) {
		end = len(prs)
	}
	pagePRs := prs[start:end]
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>PRs in %s</b> (%d/%d)\n\n", html.EscapeString(repoGroup), page+1, totalPages))
	for _, pr := range pagePRs {
		statusEmoji := "🔵"
		switch pr.State {
		case "merged":
			statusEmoji = "🟣"
		case "closed":
			statusEmoji = "🔴"
		case "spam":
			statusEmoji = "⚠️"
		}
		title := pr.Title
		if len(title) > 35 {
			title = title[:32] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s <b>#%d</b> %s — <i>%s</i> [%s]\n",
			statusEmoji, pr.PRNumber, html.EscapeString(title), html.EscapeString(pr.Author), pr.State))
	}
	markup := &telebot.ReplyMarkup{}
	escapeRG := strings.ReplaceAll(repoGroup, "|", "%7C")
	var btns []telebot.Btn
	if page > 0 {
		btns = append(btns, markup.Data(fmt.Sprintf("‹ Page %d", page), "prs_page", fmt.Sprintf("prs_page:%s:%d", escapeRG, page-1)))
	}
	btns = append(btns, markup.Data(fmt.Sprintf("📄 %d/%d", page+1, totalPages), "prs_page", fmt.Sprintf("prs_page:%s:%d", escapeRG, page)))
	if page < totalPages-1 {
		btns = append(btns, markup.Data(fmt.Sprintf("Page %d ›", page+2), "prs_page", fmt.Sprintf("prs_page:%s:%d", escapeRG, page+1)))
	}
	markup.Inline(markup.Row(btns...))
	return c.Edit(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML, ReplyMarkup: markup})
}
