package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platformutil"
)

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
