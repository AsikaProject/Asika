package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) isAdmin(c telebot.Context) bool {
	if len(b.adminIDs) == 0 && len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return true
	}
	return b.adminIDs[c.Sender().ID]
}

func (b *Bot) isOperator(c telebot.Context) bool {
	if b.isAdmin(c) {
		return true
	}
	if len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return false
	}
	return b.operatorIDs[c.Sender().ID]
}

func (b *Bot) isViewer(c telebot.Context) bool {
	if b.isOperator(c) {
		return true
	}
	return b.viewerIDs[c.Sender().ID]
}

// getUserRole returns the role name for the sender: "admin", "operator", or "viewer"
func (b *Bot) getUserRole(c telebot.Context) string {
	if b.isAdmin(c) {
		return "admin"
	}
	if b.isOperator(c) {
		return "operator"
	}
	return "viewer"
}

func (b *Bot) requireAdmin(c telebot.Context) bool {
	if !b.isAdmin(c) {
		c.Send("Access denied. Admin only.")
		return false
	}
	return true
}

func (b *Bot) requireOperator(c telebot.Context) bool {
	if !b.isOperator(c) {
		c.Send("Access denied. Operator or Admin only.")
		return false
	}
	return true
}

func (b *Bot) handleStart(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	userID := c.Sender().ID
	username := c.Sender().Username
	msg := fmt.Sprintf(
		"<b>Welcome to Asika Bot</b>\n\nHello @%s (ID: %d)\n\nUse /help to see available commands.\n\nYou have admin privileges.",
		html.EscapeString(username), userID,
	)
	return c.Send(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleHelp(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	help := `<b>Asika Bot Commands</b>

📋 <b>PR Management</b>
/prs repo_group — List PRs
/pr repo_group number — Show PR details
/approve repo_group pr_id — Approve a PR
/close repo_group pr_id — Close a PR
/reopen repo_group pr_id — Reopen a PR (spam recovery)
/spam repo_group pr_id — Mark PR as spam

📊 <b>Queue</b>
/queue repo_group — Show merge queue
/recheck repo_group — Trigger queue recheck

⚙️ <b>Config</b>
/config — Show current config (masked)

🧹 <b>Stale PRs</b>
/stale repo_group — Show stale PRs
/unstale repo_group pr_number — Remove stale label

🔄 <b>Rebase</b>
/rebase repo_group pr_number — Rebase a PR onto its base branch

🍒 <b>Cherry-pick</b>
/cherrypick repo_group pr_number target_branch — Cherry-pick a merged PR

📈 <b>Stats</b>
/stats — Show DORA metrics

ℹ️ <b>Info</b>
/version — Show version info`
	return c.Send(help, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

const prsPerPage = 10

func (b *Bot) handleListPRs(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	repoGroup := ""
	page := 0
	for i := 1; i < len(args); i++ {
		if n, err := strconv.Atoi(args[i]); err == nil && n > 0 {
			page = n - 1
		} else {
			repoGroup = args[i]
		}
	}
	if repoGroup == "" {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			return c.Send("No repo groups configured.")
		}
		repoGroup = groups[0].Name
	}
	prs := b.fetchPRsForGroup(repoGroup)
	if len(prs) == 0 {
		return c.Send(fmt.Sprintf("No PRs found for repo group <b>%s</b>.", html.EscapeString(repoGroup)),
			&telebot.SendOptions{ParseMode: telebot.ModeHTML})
	}
	return b.sendPRsPage(c, repoGroup, prs, page)
}

func (b *Bot) handleShowPR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /pr repo_group pr_number")
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		return c.Send("Invalid PR number.")
	}
	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && pr.PRNumber == prNumber {
			found = &pr
		}
		return nil
	})
	if found == nil {
		return c.Send(fmt.Sprintf("PR #%d not found in repo group <b>%s</b>.", prNumber, html.EscapeString(repoGroup)),
			&telebot.SendOptions{ParseMode: telebot.ModeHTML})
	}
	msg := fmt.Sprintf(
		"<b>PR #%d</b> — %s\n\n  Author: %s\n  State: %s\n  Platform: %s\n  Repo Group: %s\n  Labels: %s\n  Spam: %v\n  Created: %s\n",
		found.PRNumber, html.EscapeString(found.Title),
		html.EscapeString(found.Author), found.State, found.Platform,
		found.RepoGroup, html.EscapeString(strings.Join(found.Labels, ", ")),
		found.SpamFlag, found.CreatedAt.Format(time.RFC3339),
	)
	selector := &telebot.ReplyMarkup{}
	btnApprove := selector.Data("✅ Approve", "approve", fmt.Sprintf("approve:%s#%s", repoGroup, found.ID))
	btnClose := selector.Data("❌ Close", "close", fmt.Sprintf("close:%s#%s", repoGroup, found.ID))
	btnSpam := selector.Data("🚫 Spam", "spam", fmt.Sprintf("spam:%s#%s", repoGroup, found.ID))
	btnReopen := selector.Data("🔄 Reopen", "reopen", fmt.Sprintf("reopen:%s#%s", repoGroup, found.ID))
	selector.Inline(selector.Row(btnApprove, btnClose), selector.Row(btnSpam, btnReopen))
	return c.Send(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML, ReplyMarkup: selector})
}

func (b *Bot) handleApprovePR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /approve repo_group pr_id")
	}
	repoGroup := args[1]
	prID := args[2]
	pr, err := commonutil.GetPRByID(repoGroup, prID)
	if err != nil || pr == nil {
		return c.Send("PR not found.")
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found.")
	}
	client := b.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return c.Send(fmt.Sprintf("No client configured for platform %s.", pr.Platform))
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return c.Send("Cannot resolve repository.")
	}
	ctx := context.Background()
	if err := client.ApprovePR(ctx, owner, repo, pr.PRNumber); err != nil {
		slog.Error("telegram bot: approve failed", "error", err)
		db.AppendAuditLog("error", "PR approve failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		return c.Send(fmt.Sprintf("Failed to approve PR: %v", err))
	}
	pr.IsApproved = true
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
		return c.Send(fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber))
	}
	return c.Send(fmt.Sprintf("PR #%d approved.", pr.PRNumber))
}

func (b *Bot) handleClosePR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /close repo_group pr_id")
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return c.Send("PR not found.")
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found.")
	}
	client := b.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return c.Send("No client configured for platform.")
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		return c.Send(fmt.Sprintf("Failed to close PR: %v", err))
	}
	pr.State = "closed"
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram",
	})
	return c.Send(fmt.Sprintf("PR #%d closed.", pr.PRNumber))
}

func (b *Bot) handleReopenPR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /reopen repo_group pr_id")
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return c.Send("PR not found.")
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found.")
	}
	client := b.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return c.Send("No client configured for platform.")
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ReopenPR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
		})
		return c.Send(fmt.Sprintf("Failed to reopen PR: %v", err))
	}
	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber), data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram",
	})
	return c.Send(fmt.Sprintf("PR #%d reopened.", pr.PRNumber))
}

func (b *Bot) handleMarkSpam(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /spam repo_group pr_id")
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return c.Send("PR not found.")
	}
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram",
	})
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group != nil {
		client := b.clients[platforms.PlatformType(pr.Platform)]
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "telegram", "error": err.Error(),
				})
			}
		}
	}
	if b.notifier != nil {
		title := fmt.Sprintf("[Spam Alert] PR #%d by %s", pr.PRNumber, pr.Author)
		body := fmt.Sprintf("PR #%d \"%s\" by %s marked as spam via Telegram.\nRepo: %s | Platform: %s",
			pr.PRNumber, pr.Title, pr.Author, pr.RepoGroup, pr.Platform)
		b.notifier.Send(context.Background(), title, body)
	}
	return c.Send(fmt.Sprintf("PR #%d marked as spam.", pr.PRNumber))
}

func (b *Bot) handleShowQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) > 0 {
			repoGroup = groups[0].Name
		}
	}
	var items []models.QueueItem
	db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if repoGroup == "" || item.RepoGroup == repoGroup || strings.HasPrefix(string(key), repoGroup+"#") {
			items = append(items, item)
		}
		return nil
	})
	if len(items) == 0 {
		return c.Send(fmt.Sprintf("Queue empty for <b>%s</b>.", html.EscapeString(repoGroup)),
			&telebot.SendOptions{ParseMode: telebot.ModeHTML})
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Merge Queue — %s</b>\n\n", html.EscapeString(repoGroup)))
	for _, item := range items {
		statusEmoji := "⏳"
		switch item.Status {
		case "done":
			statusEmoji = "✅"
		case "failed":
			statusEmoji = "❌"
		case "merging":
			statusEmoji = "🔄"
		}
		sb.WriteString(fmt.Sprintf("%s %s (%s) — %s\n", statusEmoji, item.PRID, item.Status, item.AddedAt.Format(time.RFC3339)))
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleClearQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) > 0 {
			repoGroup = groups[0].Name
		}
	}
	if repoGroup == "" {
		return c.Send("No repo group configured.")
	}
	if b.queueMgr == nil {
		return c.Send("Queue manager not initialized.")
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to clear queue: %v", err))
	}
	return c.Send(fmt.Sprintf("Queue cleared for <b>%s</b>. %d items removed.", html.EscapeString(repoGroup), count),
		&telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleRemoveFromQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /queue_remove <repo_group> <pr_id)")
	}
	if b.queueMgr == nil {
		return c.Send("Queue manager not initialized.")
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		return c.Send(fmt.Sprintf("Failed to remove: %v", err))
	}
	return c.Send(fmt.Sprintf("Removed <b>%s</b> from queue.", html.EscapeString(args[2])),
		&telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleRecheckQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	if b.queueMgr == nil {
		return c.Send("Queue manager not initialized.")
	}
	go b.queueMgr.CheckQueue()
	return c.Send("Queue recheck triggered.")
}

func (b *Bot) handleShowConfig(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	cfg := config.Current()
	if cfg == nil {
		return c.Send("Config not loaded.")
	}
	groups := config.GetRepoGroups(cfg)
	var sb strings.Builder
	sb.WriteString("<b>Current Config</b>\n\n")
 	sb.WriteString(fmt.Sprintf("  Server: %s (%s)\n", cfg.Server.Listen, cfg.Server.Mode))
	sb.WriteString(fmt.Sprintf("  CPU Threads: min=%d max=%d\n", cfg.Server.MinProcs, cfg.Server.MaxProcs))
	sb.WriteString(fmt.Sprintf("  DB: %s\n", cfg.Database.Path))
	sb.WriteString(fmt.Sprintf("  Events: %s\n", cfg.Events.Mode))
	sb.WriteString(fmt.Sprintf("  Spam: enabled=%v\n", cfg.Spam.Enabled))
	sb.WriteString(fmt.Sprintf("  Notify channels: %d\n", len(cfg.Notify)))
	sb.WriteString(fmt.Sprintf("  Label rules: %d\n", len(cfg.LabelRules)))
	sb.WriteString(fmt.Sprintf("  Repo groups: %d\n", len(groups)))
	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("    - %s (%s)\n", g.Name, g.Mode))
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleStaleCheck(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	}
	dryRun := len(args) > 2 && args[2] == "--dry-run"
	cfg := config.Current()
	if cfg == nil || !cfg.Stale.Enabled {
		return c.Send("Stale PR management is not enabled in config.")
	}
	var groups []models.RepoGroup
	if repoGroup != "" {
		g := config.GetRepoGroupByName(cfg, repoGroup)
		if g == nil {
			return c.Send("Repo group not found: " + repoGroup)
		}
		groups = []models.RepoGroup{*g}
	} else {
		groups = config.GetRepoGroups(cfg)
	}
	var lines []string
	if dryRun {
		lines = append(lines, "<b>Stale PR Dry Run:</b>")
	} else {
		lines = append(lines, "<b>Stale PR Check Results:</b>")
	}
	for _, group := range groups {
		prs, err := b.fetchOpenPRs(&group)
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: error listing PRs", group.Name))
			continue
		}
		for _, pr := range prs {
			days := commonutil.InactivityDays(pr.UpdatedAt)
			hasStale := commonutil.HasLabelStr(pr.Labels, cfg.Stale.StaleLabel, "stale")
			isExempt := false
			for _, exempt := range cfg.Stale.ExemptLabels {
				if commonutil.HasLabelStr(pr.Labels, exempt, "") {
					isExempt = true
					break
				}
			}
			if cfg.Stale.SkipDraftPRs && pr.IsDraft {
				continue
			}
			if isExempt {
				continue
			}
			if hasStale && cfg.Stale.DaysUntilClose > 0 && days >= cfg.Stale.DaysUntilStale+cfg.Stale.DaysUntilClose {
				lines = append(lines, fmt.Sprintf("- [CLOSE] #%d %s (%s, %dd stale)",
					pr.PRNumber, html.EscapeString(commonutil.Truncate(pr.Title, 40)), group.Name, days))
			} else if !hasStale && days >= cfg.Stale.DaysUntilStale {
				lines = append(lines, fmt.Sprintf("- [MARK] #%d %s (%s, %dd inactive)",
					pr.PRNumber, html.EscapeString(commonutil.Truncate(pr.Title, 40)), group.Name, days))
			}
		}
	}
	if len(lines) == 1 {
		return c.Send("No stale PRs found.")
	}
	return c.Send(strings.Join(lines, "\n"), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleUnstale(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /unstale repo_group pr_number")
	}
	repoGroup := args[1]
	prNumber := args[2]
	cfg := config.Current()
	if cfg == nil {
		return c.Send("Config not loaded.")
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found: " + repoGroup)
	}
	label := cfg.Stale.StaleLabel
	if label == "" {
		label = "stale"
	}
	removed := false
	for _, pt := range platforms.GroupPlatforms(group) {
		client, ok := b.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := client.RemoveLabel(ctx, owner, repo, commonutil.ParseInt(prNumber), label)
		cancel()
		if err != nil {
			continue
		}
		removed = true
	}
	if !removed {
		return c.Send("Failed to remove stale label.")
	}
	return c.Send("Stale label removed from PR #" + prNumber + " in " + repoGroup)
}

func (b *Bot) handleRebasePR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /rebase repo_group pr_number")
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		return c.Send("Invalid PR number.")
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found: " + repoGroup)
	}
	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && pr.PRNumber == prNumber {
			found = &pr
		}
		return nil
	})
	if found == nil {
		return c.Send(fmt.Sprintf("PR #%d not found in repo group %s", prNumber, repoGroup))
	}
	if found.State != "open" {
		return c.Send(fmt.Sprintf("PR #%d is not open (state: %s)", prNumber, found.State))
	}
	platform := found.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client, ok := b.clients[platforms.PlatformType(platform)]
	if !ok {
		return c.Send("Platform client not available: " + platform)
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return c.Send("Cannot resolve repo for platform: " + platform)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, prNumber)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to get branch info: %v", err))
	}
	if !branchInfo.MaintainerCanModify {
		return c.Send("⚠️ Rebase not allowed: PR author has not enabled 'allow edits from maintainers'. Please ask the author to enable it on the PR page.")
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/rebase", b.cfg.Server.Listen, repoGroup, prNumber)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Rebase request failed: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return c.Send("Rebase completed (async)")
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		return c.Send("✅ " + msg)
	}
	if errMsg, ok := result["error"].(string); ok {
		return c.Send("❌ Rebase failed: " + errMsg)
	}
	if msg, ok := result["message"].(string); ok {
		return c.Send("ℹ️ " + msg)
	}
	return c.Send("Rebase request submitted.")
}

func (b *Bot) handleCherryPickPR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 4 {
		return c.Send("Usage: /cherrypick repo_group pr_number target_branch")
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		return c.Send("Invalid PR number.")
	}
	targetBranch := args[3]
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found: " + repoGroup)
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/cherry-pick", b.cfg.Server.Listen, repoGroup, prNumber)
	body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return c.Send(fmt.Sprintf("Error: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Cherry-pick request failed: %v", err))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		return c.Send("Cherry-pick completed (async)")
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		return c.Send("🍒 " + msg)
	}
	if errMsg, ok := result["error"].(string); ok {
		return c.Send("❌ Cherry-pick failed: " + errMsg)
	}
	if msg, ok := result["message"].(string); ok {
		return c.Send("ℹ️ " + msg)
	}
	return c.Send("Cherry-pick request submitted.")
}

func (b *Bot) handleUsage(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to fetch usage: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return c.Send("Error parsing usage response")
	}
	var sb strings.Builder
	sb.WriteString("<b>💻 System Usage</b>\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: <b>%.1f%%</b>\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: <b>%v</b>\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: <b>%v</b>\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: <b>%v</b>\n", v))
	}
	sb.WriteString("\n<b>Memory</b>\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: <b>%s</b>\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: <b>%s</b>\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: <b>%s</b>\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: <b>%s</b>\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: <b>%.1f%%</b>\n", utils.ToFloat64(pct)))
			}
		}
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) handleStats(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to fetch stats: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return c.Send("Error parsing stats response")
	}
	var sb strings.Builder
	sb.WriteString("<b>📊 DORA Metrics</b>\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: <b>%.2f</b>\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: <b>%s</b>\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: <b>%.1f%%</b>\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: <b>%s</b>\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n<b>Overview</b>\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: <b>%v</b>\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: <b>%v</b>\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: <b>%v</b>\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: <b>%v</b>\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n<b>By Repo Group</b>\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: <b>%v</b>\n", html.EscapeString(k), v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n<b>By Platform</b>\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: <b>%v</b>\n", k, v))
		}
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleVersion(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	return c.Send(fmt.Sprintf("<b>Asika</b>\nVersion: <code>%s</code>", version.Version),
		&telebot.SendOptions{ParseMode: telebot.ModeHTML})
}
