package discord

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

	"github.com/bwmarrin/discordgo"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	commonutil "asika/common/platformutil"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) handleHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	help := `**Asika Bot Commands**

**PR Management**
!prs [repo_group] — List PRs
!pr <repo_group> <number> — Show PR details
!approve <repo_group> <pr_id> — Approve a PR
!close <repo_group> <pr_id> — Close a PR
!reopen <repo_group> <pr_id> — Reopen a PR (spam recovery)
!spam <repo_group> <pr_id> — Mark PR as spam

**Queue**
!queue [repo_group] — Show merge queue
!recheck [repo_group] — Trigger queue recheck

**Config**
!config — Show current config (masked)

**Rebase / Cherry-pick**
!rebase repo_group pr_number — Rebase a PR onto its base branch
!cherry-pick repo_group pr_number target_branch — Cherry-pick a merged PR

**Info**
!version — Show version info`
	s.ChannelMessageSend(m.ChannelID, help)
}

func (b *Bot) handleListPRs(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			s.ChannelMessageSend(m.ChannelID, "No repo groups configured.")
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
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No PRs found for repo group **%s**.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**PRs in %s**\n\n", repoGroup))
	for _, pr := range prs {
		statusEmoji := "🔵"
		switch pr.State {
		case "merged":
			statusEmoji = "🟣"
		case "closed":
			statusEmoji = "🔴"
		case "spam":
			statusEmoji = "⚠️"
		}
		sb.WriteString(fmt.Sprintf("%s **#%d** %s — by %s (%s/%s)\n",
			statusEmoji, pr.PRNumber, commonutil.Truncate(pr.Title, 40), pr.Author, pr.Platform, pr.State))
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleShowPR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!pr <repo_group> <pr_number>`")
		return
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "Invalid PR number.")
		return
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
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d not found in repo group **%s**.", prNumber, repoGroup))
		return
	}
	msg := fmt.Sprintf(
		"**PR #%d** — %s\n\n  Author: %s\n  State: %s\n  Platform: %s\n  Repo Group: %s\n  Labels: %s\n  Spam: %v\n  Created: %s\n",
		found.PRNumber, found.Title, found.Author, found.State, found.Platform,
		found.RepoGroup, strings.Join(found.Labels, ", "), found.SpamFlag, found.CreatedAt.Format(time.RFC3339),
	)
	s.ChannelMessageSend(m.ChannelID, msg)
}

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
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
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
		s.ChannelMessageSend(m.ChannelID, "Usage: `!close <repo_group> <pr_id>`")
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
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord", "error": err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to close PR: %v", err))
		return
	}
	pr.State = "closed"
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "discord",
	})
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d closed.", pr.PRNumber))
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

func (b *Bot) handleShowQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Queue empty for **%s**.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Merge Queue — %s**\n\n", repoGroup))
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
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleRecheckQueue(s *discordgo.Session, m *discordgo.MessageCreate) {
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}
	go b.queueMgr.CheckQueue()
	s.ChannelMessageSend(m.ChannelID, "Queue recheck triggered.")
}

func (b *Bot) handleClearQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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
		s.ChannelMessageSend(m.ChannelID, "No repo group configured.")
		return
	}
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to clear queue: %v", err))
		return
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Queue cleared for **%s**. %d items removed.", repoGroup, count))
}

func (b *Bot) handleRemoveFromQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: !queue_remove <repo_group> <pr_id>")
		return
	}
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to remove: %v", err))
		return
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Removed **%s** from queue.", args[2]))
}

func (b *Bot) handleShowConfig(s *discordgo.Session, m *discordgo.MessageCreate) {
	cfg := config.Current()
	if cfg == nil {
		s.ChannelMessageSend(m.ChannelID, "Config not loaded.")
		return
	}
	groups := config.GetRepoGroups(cfg)
	var sb strings.Builder
	sb.WriteString("**Current Config**\n\n")
	sb.WriteString(fmt.Sprintf("  Server: %s (%s)\n", cfg.Server.Listen, cfg.Server.Mode))
	sb.WriteString(fmt.Sprintf("  DB: %s\n", cfg.Database.Path))
	sb.WriteString(fmt.Sprintf("  Events: %s\n", cfg.Events.Mode))
	sb.WriteString(fmt.Sprintf("  Spam: enabled=%v\n", cfg.Spam.Enabled))
	sb.WriteString(fmt.Sprintf("  Notify channels: %d\n", len(cfg.Notify)))
	sb.WriteString(fmt.Sprintf("  Label rules: %d\n", len(cfg.LabelRules)))
	sb.WriteString(fmt.Sprintf("  Repo groups: %d\n", len(groups)))
	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("    - %s (%s)\n", g.Name, g.Mode))
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
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

func (b *Bot) handleUsage(s *discordgo.Session, m *discordgo.MessageCreate) {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to fetch usage: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing usage response")
		return
	}
	var sb strings.Builder
	sb.WriteString("**💻 System Usage**\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: **%.1f%%**\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: **%v**\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: **%v**\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: **%v**\n", v))
	}
	sb.WriteString("\n**Memory**\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: **%s**\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: **%s**\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: **%s**\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: **%s**\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: **%.1f%%**\n", utils.ToFloat64(pct)))
			}
		}
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleStats(s *discordgo.Session, m *discordgo.MessageCreate) {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to fetch stats: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing stats response")
		return
	}
	var sb strings.Builder
	sb.WriteString("**📊 DORA Metrics**\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: **%.2f**\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: **%s**\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: **%.1f%%**\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: **%s**\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n**Overview**\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: **%v**\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: **%v**\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: **%v**\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: **%v**\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n**By Repo Group**\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: **%v**\n", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n**By Platform**\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: **%v**\n", k, v))
		}
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleVersion(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("**Asika**\nVersion: `%s`", version.Version))
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
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
