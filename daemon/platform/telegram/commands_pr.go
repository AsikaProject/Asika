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
)

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
	payload := fmt.Sprintf("%s#%s", repoGroup, found.ID)
	switch found.State {
	case "open":
		btnApprove := selector.Data("✅ Approve", "approve", "approve:"+payload)
		btnClose := selector.Data("❌ Close", "close", "close:"+payload)
		btnSpam := selector.Data("🚫 Spam", "spam", "spam:"+payload)
		selector.Inline(selector.Row(btnApprove, btnClose), selector.Row(btnSpam))
	case "closed", "spam":
		btnReopen := selector.Data("🔄 Reopen", "reopen", "reopen:"+payload)
		selector.Inline(selector.Row(btnReopen))
	case "merged":
		btnRevert := selector.Data("↩️ Revert", "revert", "revert:"+payload)
		selector.Inline(selector.Row(btnRevert))
	}
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

func (b *Bot) handleRevertPR(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /revert repo_group pr_number")
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		return c.Send("Invalid PR number.")
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
		return c.Send(fmt.Sprintf("PR #%d not found in repo group <b>%s</b>.", prNumber, html.EscapeString(repoGroup)),
			&telebot.SendOptions{ParseMode: telebot.ModeHTML})
	}
	if found.State != "merged" {
		return c.Send(fmt.Sprintf("PR #%d is not merged (state: %s).", prNumber, found.State))
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found.")
	}
	client := b.clients[platforms.PlatformType(found.Platform)]
	if client == nil {
		return c.Send("No client configured for platform.")
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, found.Platform)
	ctx := context.Background()
	revertPR, err := client.RevertPR(ctx, owner, repo, found.PRNumber)
	if err != nil {
		db.AppendAuditLog("error", "PR revert failed", map[string]interface{}{
			"pr_number": found.PRNumber, "repo_group": found.RepoGroup, "platform": found.Platform, "actor": "telegram", "error": err.Error(),
		})
		return c.Send(fmt.Sprintf("Failed to revert PR: %v", err))
	}
	actor := c.Sender().Username
	if actor == "" {
		actor = fmt.Sprintf("%d", c.Sender().ID)
	}
	if revertPR != nil {
		revertPR.RepoGroup = repoGroup
		revertPR.Platform = found.Platform
		revertPR.ID = fmt.Sprintf("%d", revertPR.PRNumber)
		revertPRData, _ := json.Marshal(revertPR)
		revertKey := fmt.Sprintf("%s#%s#%d", repoGroup, found.Platform, revertPR.PRNumber)
		db.PutPRWithIndex(revertKey, revertPRData, revertPR.ID, repoGroup, revertPR.PRNumber)
		if b.queueMgr != nil {
			if err := b.queueMgr.AddToQueue(revertPR); err != nil {
				slog.Warn("telegram bot: failed to add revert PR to queue", "error", err, "pr_number", revertPR.PRNumber)
			} else {
				go b.queueMgr.CheckQueue()
			}
		}
		if err := client.CommentPR(ctx, owner, repo, found.PRNumber,
			fmt.Sprintf("This PR has been reverted by %s. Revert PR: #%d", actor, revertPR.PRNumber)); err != nil {
			slog.Warn("telegram bot: failed to comment on reverted PR", "error", err, "pr_number", found.PRNumber)
		}
		if b.notifier != nil {
			title := fmt.Sprintf("[Revert] PR #%d reverted", found.PRNumber)
			body := fmt.Sprintf("PR #%d \"%s\" was reverted by %s.\nRevert PR: #%d\nRepo: %s | Platform: %s",
				found.PRNumber, found.Title, actor, revertPR.PRNumber, repoGroup, found.Platform)
			b.notifier.Send(ctx, title, body)
		}
		db.AppendAuditLog("info", "PR reverted", map[string]interface{}{
			"pr_number": found.PRNumber, "repo_group": found.RepoGroup, "platform": found.Platform, "actor": "telegram", "revert_pr_number": revertPR.PRNumber,
		})
		return c.Send(fmt.Sprintf("PR #%d reverted. Revert PR: #%d", found.PRNumber, revertPR.PRNumber))
	}
	db.AppendAuditLog("info", "PR revert requested", map[string]interface{}{
		"pr_number": found.PRNumber, "repo_group": found.RepoGroup, "platform": found.Platform, "actor": "telegram",
	})
	return c.Send(fmt.Sprintf("PR #%d revert requested.", found.PRNumber))
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
