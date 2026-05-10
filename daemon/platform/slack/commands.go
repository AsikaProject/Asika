package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platformutil"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) handleHelp(ev *slack.MessageEvent, client *socketmode.Client) {
	help := `*Asika Bot Commands*

*PR Management*
prs [repo_group] — List PRs
pr <repo_group> <number> — Show PR details
approve <repo_group> <pr_id> — Approve a PR
close <repo_group> <pr_id> [reason] — Close a PR
reopen <repo_group> <pr_id> — Reopen a PR (spam recovery)
spam <repo_group> <pr_id> — Mark PR as spam
revert <repo_group> <pr_id> — Revert a merged PR

*Queue*
queue [repo_group] — Show merge queue
recheck [repo_group] — Trigger queue recheck

*Config*
config — Show current config (masked)

*Rebase / Cherry-pick*
rebase repo_group pr_number — Rebase a PR onto its base branch
cherry-pick repo_group pr_number target_branch — Cherry-pick a merged PR

*Info*
version — Show version info`
	b.postMessage(client, ev.Channel, help)
}

func (b *Bot) handleListPRs(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			b.postMessage(client, ev.Channel, "No repo groups configured.")
			return
		}
		repoGroup = groups[0].Name
	}
	var prs []models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup || repoGroup == "" {
			prs = append(prs, pr)
		}
		return nil
	})
	if len(prs) == 0 {
		b.postMessage(client, ev.Channel, fmt.Sprintf("No PRs found for repo group *%s*.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*PRs in %s*\n\n", repoGroup))
	for _, pr := range prs {
		stateEmoji := map[string]string{"merged": "🟣", "closed": "🔴", "spam": "⚠️"}
		emoji := "🔵"
		if e, ok := stateEmoji[pr.State]; ok {
			emoji = e
		}
		sb.WriteString(fmt.Sprintf("%s *#%d* %s — by %s (%s/%s)\n",
			emoji, pr.PRNumber, utils.TruncateString(pr.Title, 40), pr.Author, pr.Platform, pr.State))
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleShowPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: pr <repo_group> <number>")
		return
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Invalid PR number: %s", args[2]))
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Repo group *%s* not found.", repoGroup))
		return
	}
	platform := config.GetPlatformForGroup(group)
	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Cannot resolve repo for platform: %s", platform))
		return
	}
	pClient := b.getClientForPlatform(platform)
	if pClient == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Platform client not available: %s", platform))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pr, err := pClient.GetPR(ctx, owner, repo, prNumber)
	if err != nil || pr == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d not found in %s.", prNumber, repoGroup))
		return
	}
	text := fmt.Sprintf("*PR #%d — %s*\nState: %s\nAuthor: %s\nPlatform: %s\nURL: %s",
		pr.PRNumber, pr.Title, pr.State, pr.Author, pr.Platform, pr.HTMLURL)
	b.postMessage(client, ev.Channel, text)
}

func (b *Bot) handleApprovePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: approve <repo_group> <pr_id>")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, err := platformutil.GetPRByID(repoGroup, prID)
	if err != nil || pr == nil {
		b.postMessage(client, ev.Channel, "PR not found.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		b.postMessage(client, ev.Channel, "Repo group not found.")
		return
	}
	pClient := b.getClientForPlatform(pr.Platform)
	if pClient == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("No client configured for platform %s.", pr.Platform))
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := pClient.ApprovePR(ctx, owner, repo, pr.PRNumber); err != nil {
		slog.Error("slack bot: approve failed", "error", err)
		db.AppendAuditLog("error", "PR approve failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "error": err.Error(),
		})
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to approve PR: %v", err))
		return
	}
	pr.IsApproved = true
	pr.Events = append(pr.Events, models.PREvent{Timestamp: time.Now(), Action: "approved", Actor: ev.User})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	addedToQueue := false
	if b.queueMgr != nil {
		if pr.State != "" && pr.State != "open" {
			slog.Info("slack bot: skipping queue add for non-open PR", "pr_number", pr.PRNumber, "state", pr.State)
		} else {
			if err := b.queueMgr.AddToQueue(pr); err != nil {
				slog.Warn("slack bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
			} else {
				addedToQueue = true
				go b.queueMgr.CheckQueue()
			}
		}
	}
	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "added_to_queue": addedToQueue,
	})
	if addedToQueue {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber))
	} else {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d approved.", pr.PRNumber))
	}
}

func (b *Bot) handleClosePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: close <repo_group> <pr_id> [reason]")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	reason := ""
	if len(args) > 3 {
		reason = strings.Join(args[3:], " ")
	}
	pr, _ := platformutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		b.postMessage(client, ev.Channel, "PR not found.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		b.postMessage(client, ev.Channel, "Repo group not found.")
		return
	}
	pClient := b.getClientForPlatform(pr.Platform)
	if pClient == nil {
		b.postMessage(client, ev.Channel, "No client configured for platform.")
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := pClient.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "error": err.Error(),
		})
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to close PR: %v", err))
		return
	}
	if reason != "" {
		_ = pClient.CreateLabel(ctx, owner, repo, reason, "ededed", "Close reason: "+reason)
		_ = pClient.AddLabel(ctx, owner, repo, pr.PRNumber, reason, "ededed")
	}
	pr.State = "closed"
	pr.CloseReason = reason
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "reason": reason,
	})
	if reason != "" {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d closed with reason: %s", pr.PRNumber, reason))
	} else {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d closed.", pr.PRNumber))
	}
}

func (b *Bot) handleReopenPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: reopen <repo_group> <pr_id>")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := platformutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		b.postMessage(client, ev.Channel, "PR not found.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		b.postMessage(client, ev.Channel, "Repo group not found.")
		return
	}
	pClient := b.getClientForPlatform(pr.Platform)
	if pClient == nil {
		b.postMessage(client, ev.Channel, "No client configured for platform.")
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := pClient.ReopenPR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "error": err.Error(),
		})
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to reopen PR: %v", err))
		return
	}
	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber), data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack",
	})
	b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d reopened.", pr.PRNumber))
}

func (b *Bot) handleMarkSpam(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: spam <repo_group> <pr_id>")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := platformutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		b.postMessage(client, ev.Channel, "PR not found.")
		return
	}
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack",
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
		pClient := b.getClientForPlatform(pr.Platform)
		if pClient != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := pClient.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "error": err.Error(),
				})
			}
		}
	}
	if b.notifier != nil {
		title := fmt.Sprintf("[Spam Alert] PR #%d by %s", pr.PRNumber, pr.Author)
		body := fmt.Sprintf("PR #%d \"%s\" by %s marked as spam via Slack.\nRepo: %s | Platform: %s",
			pr.PRNumber, pr.Title, pr.Author, pr.RepoGroup, pr.Platform)
		b.notifier.Send(context.Background(), title, body)
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d marked as spam.", pr.PRNumber))
}

func (b *Bot) handleRevertPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: revert <repo_group> <pr_id>")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := platformutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		b.postMessage(client, ev.Channel, "PR not found.")
		return
	}
	if pr.State != "merged" {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d is not merged (state: %s).", pr.PRNumber, pr.State))
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		b.postMessage(client, ev.Channel, "Repo group not found.")
		return
	}
	pClient := b.getClientForPlatform(pr.Platform)
	if pClient == nil {
		b.postMessage(client, ev.Channel, "No client configured for platform.")
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	revertPR, err := pClient.RevertPR(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		db.AppendAuditLog("error", "PR revert failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack", "error": err.Error(),
		})
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to revert PR: %v", err))
		return
	}
	if revertPR != nil {
		revertData, _ := json.Marshal(revertPR)
		revertKey := fmt.Sprintf("%s#%s#%d", revertPR.RepoGroup, revertPR.Platform, revertPR.PRNumber)
		db.PutPRWithIndex(revertKey, revertData, revertPR.ID, revertPR.RepoGroup, revertPR.PRNumber)
		if b.queueMgr != nil {
			if err := b.queueMgr.AddToQueue(revertPR); err != nil {
				slog.Warn("slack bot: failed to add revert PR to queue", "error", err, "pr_number", revertPR.PRNumber)
			} else {
				go b.queueMgr.CheckQueue()
			}
		}
	}
	_ = pClient.CommentPR(ctx, owner, repo, pr.PRNumber, fmt.Sprintf("Revert PR #%d has been created by %s via Slack.", pr.PRNumber, ev.User))
	db.AppendAuditLog("info", "PR reverted", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "slack",
	})
	if b.notifier != nil {
		title := fmt.Sprintf("[Revert] PR #%d reverted", pr.PRNumber)
		body := fmt.Sprintf("PR #%d \"%s\" reverted by %s via Slack.\nRepo: %s | Platform: %s",
			pr.PRNumber, pr.Title, ev.User, pr.RepoGroup, pr.Platform)
		b.notifier.Send(context.Background(), title, body)
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d revert initiated.", pr.PRNumber))
}

func (b *Bot) handleShowQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	}
	items, err := b.queueMgr.GetQueueItems(repoGroup)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Error fetching queue: %v", err))
		return
	}
	if len(items) == 0 {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Queue is empty for repo group *%s*.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*Merge Queue for %s*\n\n", repoGroup))
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. %s [%s]\n", i+1, item.PRID, item.Status))
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleRecheckQueue(ev *slack.MessageEvent, client *socketmode.Client) {
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	go b.queueMgr.CheckQueue()
	b.postMessage(client, ev.Channel, "Queue recheck triggered.")
}

func (b *Bot) handleClearQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
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
		b.postMessage(client, ev.Channel, "No repo group configured.")
		return
	}
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to clear queue: %v", err))
		return
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("Queue cleared for *%s*. %d items removed.", repoGroup, count))
}

func (b *Bot) handleRemoveFromQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: queue_remove <repo_group> <pr_id>")
		return
	}
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to remove: %v", err))
		return
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("Removed *%s* from queue.", args[2]))
}

func (b *Bot) handleShowConfig(ev *slack.MessageEvent, client *socketmode.Client) {
	cfg := b.cfg
	text := fmt.Sprintf("*Asika Config*\nListen: %s\nMode: %s\nCPU Threads: min=%d max=%d\nRepo Groups: %d",
		cfg.Server.Listen, cfg.Server.Mode, cfg.Server.MinProcs, cfg.Server.MaxProcs, len(cfg.RepoGroups))
	b.postMessage(client, ev.Channel, text)
}

func (b *Bot) handleRebasePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: rebase <repo_group> <pr_number>")
		return
	}
	b.postMessage(client, ev.Channel, "Rebase via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleCherryPickPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 4 {
		b.postMessage(client, ev.Channel, "Usage: cherry-pick <repo_group> <pr_number> <target_branch>")
		return
	}
	b.postMessage(client, ev.Channel, "Cherry-pick via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleStats(ev *slack.MessageEvent, client *socketmode.Client) {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch stats: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		b.postMessage(client, ev.Channel, "Error parsing stats response")
		return
	}
	var sb strings.Builder
	sb.WriteString("*📊 DORA Metrics*\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: *%.2f*\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: *%s*\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: *%.1f%%*\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: *%s*\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n*Overview*\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: *%v*\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: *%v*\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: *%v*\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: *%v*\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n*By Repo Group*\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: *%v*\n", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n*By Platform*\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: *%v*\n", k, v))
		}
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleUsage(ev *slack.MessageEvent, client *socketmode.Client) {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch usage: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		b.postMessage(client, ev.Channel, "Error parsing usage response")
		return
	}
	var sb strings.Builder
	sb.WriteString("*💻 System Usage*\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: *%.1f%%*\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: *%v*\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: *%v*\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: *%v*\n", v))
	}
	sb.WriteString("\n*Memory*\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: *%s*\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: *%.1f%%*\n", utils.ToFloat64(pct)))
			}
		}
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) handleVersion(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, fmt.Sprintf("*Asika*\nVersion: `%s`", version.Version))
}
