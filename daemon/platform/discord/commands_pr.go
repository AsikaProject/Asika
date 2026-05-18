package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	commonutil "asika/common/platformutil"
)

func (b *Bot) handleApprovePR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!approve <repo_group> <pr_id>`")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, err := commonutil.GetPRByID(repoGroup, prID)
	if err != nil || pr == nil {
		s.ChannelMessageSend(m.ChannelID, "PR not found.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		s.ChannelMessageSend(m.ChannelID, "Repo group not found.")
		return
	}
	client := b.getClientForPlatform(pr.Platform)
	if client == nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No client configured for platform %s.", pr.Platform))
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ApprovePR(ctx, owner, repo, pr.PRNumber); err != nil {
		slog.Error("discord bot: approve failed", "error", err)
		db.AppendAuditLog("error", "PR approve failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "error": err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to approve PR: %v", err))
		return
	}
	pr.IsApproved = true
	pr.Events = append(pr.Events, models.PREvent{Timestamp: time.Now(), Action: "approved", Actor: m.Author.Username})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	if prData != nil {
		db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	}
	addedToQueue := false
	if b.queueMgr != nil {
		if pr.State != "" && pr.State != "open" {
			slog.Info("discord bot: skipping queue add for non-open PR", "pr_number", pr.PRNumber, "state", pr.State)
		} else {
			if err := b.queueMgr.AddToQueue(pr); err != nil {
				slog.Warn("discord bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
			} else {
				addedToQueue = true
				go b.queueMgr.CheckQueue()
			}
		}
	}
	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "added_to_queue": addedToQueue,
	})
	if addedToQueue {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber))
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d approved.", pr.PRNumber))
	}
}

func (b *Bot) handleClosePR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!close <repo_group> <pr_id> [reason]`")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	reason := ""
	if len(args) > 3 {
		reason = strings.Join(args[3:], " ")
	}
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		s.ChannelMessageSend(m.ChannelID, "PR not found.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		s.ChannelMessageSend(m.ChannelID, "Repo group not found.")
		return
	}
	client := b.getClientForPlatform(pr.Platform)
	if client == nil {
		s.ChannelMessageSend(m.ChannelID, "No client configured for platform.")
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "error": err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to close PR: %v", err))
		return
	}
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
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "reason": reason,
	})
	if reason != "" {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d closed with reason: %s", pr.PRNumber, reason))
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d closed.", pr.PRNumber))
	}
}

func (b *Bot) handleReopenPR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!reopen <repo_group> <pr_id>`")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		s.ChannelMessageSend(m.ChannelID, "PR not found.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		s.ChannelMessageSend(m.ChannelID, "Repo group not found.")
		return
	}
	client := b.getClientForPlatform(pr.Platform)
	if client == nil {
		s.ChannelMessageSend(m.ChannelID, "No client configured for platform.")
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	if err := client.ReopenPR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "error": err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to reopen PR: %v", err))
		return
	}
	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber), data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord",
	})
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d reopened.", pr.PRNumber))
}

func (b *Bot) handleRevertPR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!revert <repo_group> <pr_id>`")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		s.ChannelMessageSend(m.ChannelID, "PR not found.")
		return
	}
	if pr.State != "merged" {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d is not merged (state: %s).", pr.PRNumber, pr.State))
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		s.ChannelMessageSend(m.ChannelID, "Repo group not found.")
		return
	}
	client := b.getClientForPlatform(pr.Platform)
	if client == nil {
		s.ChannelMessageSend(m.ChannelID, "No client configured for platform.")
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	ctx := context.Background()
	revertPR, err := client.RevertPR(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		db.AppendAuditLog("error", "PR revert failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "error": err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to revert PR: %v", err))
		return
	}
	if revertPR != nil {
		revertData, _ := json.Marshal(revertPR)
		revertKey := fmt.Sprintf("%s#%s#%d", revertPR.RepoGroup, revertPR.Platform, revertPR.PRNumber)
		db.PutPRWithIndex(revertKey, revertData, revertPR.ID, revertPR.RepoGroup, revertPR.PRNumber)
		if b.queueMgr != nil {
			if err := b.queueMgr.AddToQueue(revertPR); err != nil {
				slog.Warn("discord bot: failed to add revert PR to queue", "error", err, "pr_number", revertPR.PRNumber)
			} else {
				go b.queueMgr.CheckQueue()
			}
		}
	}
	_ = client.CommentPR(ctx, owner, repo, pr.PRNumber, fmt.Sprintf("Revert PR #%d has been created by %s via Discord.", pr.PRNumber, m.Author.Username))
	db.AppendAuditLog("info", "PR reverted", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord",
	})
	if b.notifier != nil {
		title := fmt.Sprintf("[Revert] PR #%d reverted", pr.PRNumber)
		body := fmt.Sprintf("PR #%d \"%s\" reverted by %s via Discord.\nRepo: %s | Platform: %s",
			pr.PRNumber, pr.Title, m.Author.Username, pr.RepoGroup, pr.Platform)
		b.notifier.Send(context.Background(), title, body)
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d revert initiated.", pr.PRNumber))
}

func (b *Bot) handleMarkSpam(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!spam <repo_group> <pr_id>`")
		return
	}
	repoGroup := args[1]
	prID := args[2]
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		s.ChannelMessageSend(m.ChannelID, "PR not found.")
		return
	}
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord",
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
		client := b.getClientForPlatform(pr.Platform)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "error": err.Error(),
				})
			}
		}
	}
	if b.notifier != nil {
		title := fmt.Sprintf("[Spam Alert] PR #%d by %s", pr.PRNumber, pr.Author)
		body := fmt.Sprintf("PR #%d \"%s\" by %s marked as spam via Discord.\nRepo: %s | Platform: %s",
			pr.PRNumber, pr.Title, pr.Author, pr.RepoGroup, pr.Platform)
		b.notifier.Send(context.Background(), title, body)
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d marked as spam.", pr.PRNumber))
}

func (b *Bot) handleRebasePR(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: !rebase repo_group pr_number")
		return
	}
	repoGroup := parts[1]
	prNumber := commonutil.ParseInt(parts[2])
	if prNumber == 0 {
		s.ChannelMessageSend(m.ChannelID, "Invalid PR number.")
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		s.ChannelMessageSend(m.ChannelID, "Repo group not found: "+repoGroup)
		return
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
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d not found in %s", prNumber, repoGroup))
		return
	}
	if found.State != "open" {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d is not open (state: %s)", prNumber, found.State))
		return
	}
	platform := found.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client := b.getClientForPlatform(platform)
	if client == nil {
		s.ChannelMessageSend(m.ChannelID, "Platform client not available: "+platform)
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		s.ChannelMessageSend(m.ChannelID, "Cannot resolve repo for platform: "+platform)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, prNumber)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to get branch info: %v", err))
		return
	}
	if !branchInfo.MaintainerCanModify {
		s.ChannelMessageSend(m.ChannelID, "⚠️ Rebase not allowed: PR author has not enabled 'allow edits from maintainers'. Please ask the author to enable it on the PR page.")
		return
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/rebase", b.cfg.Server.Listen, repoGroup, prNumber)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Rebase request failed: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Rebase completed (async)")
		return
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		s.ChannelMessageSend(m.ChannelID, "✅ "+msg)
	} else if errMsg, ok := result["error"].(string); ok {
		s.ChannelMessageSend(m.ChannelID, "❌ Rebase failed: "+errMsg)
	} else {
		s.ChannelMessageSend(m.ChannelID, "Rebase request submitted.")
	}
}

func (b *Bot) handleCherryPickPR(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 4 {
		s.ChannelMessageSend(m.ChannelID, "Usage: !cherry-pick repo_group pr_number target_branch")
		return
	}
	repoGroup := parts[1]
	prNumber := commonutil.ParseInt(parts[2])
	if prNumber == 0 {
		s.ChannelMessageSend(m.ChannelID, "Invalid PR number.")
		return
	}
	targetBranch := parts[3]
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		s.ChannelMessageSend(m.ChannelID, "Repo group not found: "+repoGroup)
		return
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/cherry-pick", b.cfg.Server.Listen, repoGroup, prNumber)
	body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Cherry-pick request failed: %v", err))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Cherry-pick completed (async)")
		return
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		s.ChannelMessageSend(m.ChannelID, "🍒 "+msg)
	} else if errMsg, ok := result["error"].(string); ok {
		s.ChannelMessageSend(m.ChannelID, "❌ Cherry-pick failed: "+errMsg)
	} else {
		s.ChannelMessageSend(m.ChannelID, "Cherry-pick request submitted.")
	}
}
