package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
)

func (b *Bot) listPRsText(repoGroup string) string {
	if repoGroup == "" {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			return "No repo groups configured."
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
		return fmt.Sprintf("No PRs in %s", repoGroup)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("PRs in %s:\n", repoGroup))
	for _, pr := range prs {
		emoji := "O"
		switch pr.State {
		case "merged":
			emoji = "M"
		case "closed":
			emoji = "X"
		case "spam":
			emoji = "!"
		}
		sb.WriteString(fmt.Sprintf("  %s #%d %s - %s (%s)\n",
			emoji, pr.PRNumber, commonutil.Truncate(pr.Title, 35), pr.Author, pr.State))
	}
	return sb.String()
}

func (b *Bot) showPRText(repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return fmt.Sprintf("PR %s not found in %s", prID, repoGroup)
	}
	var desc string
	if pr.Body != "" {
		lines := strings.Split(pr.Body, "\n")
		if len(lines) > 5 {
			desc = strings.Join(lines[:5], "\n") + "\n..."
		} else {
			desc = pr.Body
		}
	}
	var events string
	if len(pr.Events) > 0 {
		var sb strings.Builder
		for _, ev := range pr.Events {
			sb.WriteString(fmt.Sprintf("  • %s by %s at %s\n", ev.Action, ev.Actor, ev.Timestamp.Format("01-02 15:04")))
		}
		events = sb.String()
	}
	msg := fmt.Sprintf(
		"PR #%d - %s\n  Author: %s | State: %s\n  Platform: %s | Labels: %s",
		pr.PRNumber, pr.Title, pr.Author, pr.State,
		pr.Platform, strings.Join(pr.Labels, ", "),
	)
	if pr.MergeCommitSHA != "" {
		msg += fmt.Sprintf("\n  Merge Commit: %s", pr.MergeCommitSHA[:8])
	}
	if desc != "" {
		msg += "\n\nDescription:\n" + desc
	}
	if events != "" {
		msg += "\n\nEvents:\n" + events
	}
	switch pr.State {
	case "open":
		msg += "\n\nAvailable actions: approve / close [reason] / spam / rebase"
	case "closed", "spam":
		msg += "\n\nAvailable actions: reopen"
	case "merged":
		msg += "\n\nAvailable actions: revert / cherrypick <target_branch>"
	}
	return msg
}

func (b *Bot) doApprove(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if err := client.ApprovePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR approve failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	pr.IsApproved = true
	pr.Events = append(pr.Events, models.PREvent{Timestamp: time.Now(), Action: "approved", Actor: senderID})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	if prData != nil {
		db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	}
	addedToQueue := false
	if b.queueMgr != nil {
		if pr.State != "" && pr.State != "open" {
			slog.Info("feishu bot: skipping queue add for non-open PR", "pr_number", pr.PRNumber, "state", pr.State)
		} else {
			if err := b.queueMgr.AddToQueue(pr); err != nil {
				slog.Warn("feishu bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
			} else {
				addedToQueue = true
				go b.queueMgr.CheckQueue()
			}
		}
	}
	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "added_to_queue": addedToQueue,
	})
	if addedToQueue {
		return fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber)
	}
	return fmt.Sprintf("PR #%d approved.", pr.PRNumber)
}

func (b *Bot) doClose(senderID, repoGroup, prID string, reasonParts []string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	reason := strings.Join(reasonParts, " ")
	if reason != "" {
		_ = client.CreateLabel(ctx, owner, repo, reason, "ededed", "Close reason: "+reason)
		_ = client.AddLabel(ctx, owner, repo, pr.PRNumber, reason, "ededed")
	}
	pr.State = "closed"
	pr.CloseReason = reason
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "reason": reason,
	})
	if reason != "" {
		return fmt.Sprintf("PR #%d closed with reason: %s", pr.PRNumber, reason)
	}
	return fmt.Sprintf("PR #%d closed.", pr.PRNumber)
}

func (b *Bot) doReopen(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if err := client.ReopenPR(context.Background(), owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber), data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu",
	})
	return fmt.Sprintf("PR #%d reopened.", pr.PRNumber)
}

func (b *Bot) doRevert(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	if pr.State != "merged" {
		return fmt.Sprintf("PR #%d is not merged (state: %s)", pr.PRNumber, pr.State)
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	revertPR, err := client.RevertPR(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		db.AppendAuditLog("error", "PR revert failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	if revertPR != nil {
		revertData, _ := json.Marshal(revertPR)
		revertKey := fmt.Sprintf("%s#%s#%d", revertPR.RepoGroup, revertPR.Platform, revertPR.PRNumber)
		db.PutPRWithIndex(revertKey, revertData, revertPR.ID, revertPR.RepoGroup, revertPR.PRNumber)
		if b.queueMgr != nil {
			if err := b.queueMgr.AddToQueue(revertPR); err != nil {
				slog.Warn("feishu bot: failed to add revert PR to queue", "error", err, "pr_number", revertPR.PRNumber)
			} else {
				go b.queueMgr.CheckQueue()
			}
		}
	}
	_ = client.CommentPR(ctx, owner, repo, pr.PRNumber, fmt.Sprintf("Revert PR #%d has been created by %s via Feishu.", pr.PRNumber, senderID))
	db.AppendAuditLog("info", "PR reverted", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu",
	})
	if b.notifier != nil {
		title := fmt.Sprintf("[Revert] PR #%d reverted", pr.PRNumber)
		body := fmt.Sprintf("PR #%d \"%s\" reverted by %s via Feishu.\nRepo: %s | Platform: %s",
			pr.PRNumber, pr.Title, senderID, pr.RepoGroup, pr.Platform)
		b.notifier.Send(context.Background(), title, body)
	}
	return fmt.Sprintf("PR #%d revert initiated.", pr.PRNumber)
}

func (b *Bot) doMarkSpam(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu",
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
		client := b.getClient(pr.Platform)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
				})
			}
		}
	}
	if b.notifier != nil {
		title := fmt.Sprintf("[Spam Alert] PR #%d", pr.PRNumber)
		body := fmt.Sprintf("PR #%d \"%s\" by %s marked as spam via Feishu.", pr.PRNumber, pr.Title, pr.Author)
		b.notifier.Send(context.Background(), title, body)
	}
	return fmt.Sprintf("PR #%d marked as spam.", pr.PRNumber)
}

func (b *Bot) doRebase(senderID, repoGroup, prNumberStr string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}
	prNumber := commonutil.ParseInt(prNumberStr)
	if prNumber == 0 {
		return "Invalid PR number: " + prNumberStr
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
		return fmt.Sprintf("PR #%d not found in %s", prNumber, repoGroup)
	}
	if found.State != "open" {
		return fmt.Sprintf("PR #%d is not open (state: %s)", prNumber, found.State)
	}
	platform := found.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client, ok := b.clients[platforms.PlatformType(platform)]
	if !ok {
		return "Platform client not available: " + platform
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return "Cannot resolve repo for platform: " + platform
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, prNumber)
	if err != nil {
		return fmt.Sprintf("Failed to get branch info: %v", err)
	}
	if !branchInfo.MaintainerCanModify {
		return "Rebase not allowed: PR author has not enabled 'allow edits from maintainers'. Please ask the author to enable it on the PR page."
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/rebase", cfg.Server.Listen, repoGroup, prNumber)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Rebase request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Rebase request submitted (async)."
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		return msg
	}
	if errMsg, ok := result["error"].(string); ok {
		return "Rebase failed: " + errMsg
	}
	return "Rebase request submitted."
}

func (b *Bot) doCherryPick(senderID, repoGroup, prNumberStr, targetBranch string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}
	prNumber := commonutil.ParseInt(prNumberStr)
	if prNumber == 0 {
		return "Invalid PR number: " + prNumberStr
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/cherry-pick", cfg.Server.Listen, repoGroup, prNumber)
	body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Cherry-pick request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		return "Cherry-pick request submitted (async)."
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		return msg
	}
	if errMsg, ok := result["error"].(string); ok {
		return "Cherry-pick failed: " + errMsg
	}
	return "Cherry-pick request submitted."
}
