package platform

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
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/common/utils"
	"asika/common/version"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

// DiscordBot wraps the Discord bot with Asika management functionality
type DiscordBot struct {
	session      *discordgo.Session
	cfg          *models.Config
	clients      map[platforms.PlatformType]platforms.PlatformClient
	queueMgr     *queue.Manager
	syncerRef    *syncer.Syncer
	spamDetector *syncer.SpamDetector
	notifier     *notifier.DiscordNotifier
	adminIDs     map[string]bool
	stop         chan struct{}
}

// SetSession sets the Discord session
func (b *DiscordBot) SetSession(s *discordgo.Session) {
	b.session = s
}

// NewDiscordBot creates a new Discord bot
func NewDiscordBot(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	discordNotifier *notifier.DiscordNotifier,
	adminIDs []string,
) *DiscordBot {
	b := &DiscordBot{
		cfg:          cfg,
		clients:      clients,
		queueMgr:     queueMgr,
		syncerRef:    syncerRef,
		spamDetector: spamDetector,
		notifier:     discordNotifier,
		adminIDs:     make(map[string]bool),
		stop:         make(chan struct{}),
	}
	for _, id := range adminIDs {
		b.adminIDs[id] = true
	}
	return b
}

// Start starts the bot and registers command handlers
func (b *DiscordBot) Start() {
	if b.session == nil {
		slog.Warn("discord bot: no session, skipping start")
		return
	}

	slog.Info("starting discord interactive bot")

	b.registerCommands()

	go func() {
		b.session.Open()
	}()
}

// Stop stops the bot gracefully
func (b *DiscordBot) Stop() {
	close(b.stop)
	if b.session != nil {
		b.session.Close()
	}
	slog.Info("discord bot stopped")
}

// registerCommands registers all bot command handlers
func (b *DiscordBot) registerCommands() {
	b.session.AddHandler(b.handleMessageCreate)
}

// isAdmin checks if the sender is an authorized admin
func (b *DiscordBot) isAdmin(userID string) bool {
	if len(b.adminIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}

func (b *DiscordBot) requireAdmin(userID string) bool {
	if !b.isAdmin(userID) {
		return false
	}
	return true
}

// handleMessageCreate handles incoming messages
func (b *DiscordBot) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if !b.requireAdmin(m.Author.ID) {
		return
	}

	content := strings.TrimSpace(m.Content)
	if content == "" {
		return
	}

	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "!help":
		b.handleHelp(s, m)
	case "!prs":
		b.handleListPRs(s, m, parts)
	case "!pr":
		b.handleShowPR(s, m, parts)
	case "!approve":
		b.handleApprovePR(s, m, parts)
	case "!close":
		b.handleClosePR(s, m, parts)
	case "!reopen":
		b.handleReopenPR(s, m, parts)
	case "!spam":
		b.handleMarkSpam(s, m, parts)
	case "!queue":
		b.handleShowQueue(s, m, parts)
	case "!recheck":
		b.handleRecheckQueue(s, m)
	case "!queue_clear":
		b.handleClearQueue(s, m, parts)
	case "!queue_remove":
		b.handleRemoveFromQueue(s, m, parts)
	case "!config":
		b.handleShowConfig(s, m)
	case "!rebase":
		b.handleRebasePR(s, m, parts)
	case "!cherry-pick":
		b.handleCherryPickPR(s, m, parts)
	case "!stats":
		b.handleStats(s, m)
	case "!version":
		b.handleVersion(s, m)
	default:
		if strings.HasPrefix(cmd, "!") {
			s.ChannelMessageSend(m.ChannelID, "Unknown command. Use !help for available commands.")
		}
	}
}

// handleHelp handles !help command
func (b *DiscordBot) handleHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
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

// handleListPRs handles !prs command
func (b *DiscordBot) handleListPRs(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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
			statusEmoji, pr.PRNumber, truncate(pr.Title, 40), pr.Author, pr.Platform, pr.State))
	}

	s.ChannelMessageSend(m.ChannelID, sb.String())
}

// handleShowPR handles !pr command
func (b *DiscordBot) handleShowPR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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
		"**PR #%d** — %s\n\n"+
			"  Author: %s\n"+
			"  State: %s\n"+
			"  Platform: %s\n"+
			"  Repo Group: %s\n"+
			"  Labels: %s\n"+
			"  Spam: %v\n"+
			"  Created: %s\n",
		found.PRNumber, found.Title,
		found.Author, found.State, found.Platform,
		found.RepoGroup, strings.Join(found.Labels, ", "),
		found.SpamFlag,
		found.CreatedAt.Format(time.RFC3339),
	)

	s.ChannelMessageSend(m.ChannelID, msg)
}

// handleApprovePR handles !approve command
func (b *DiscordBot) handleApprovePR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!approve <repo_group> <pr_id>`")
		return
	}

	repoGroup := args[1]
	prID := args[2]

	pr, err := getPRByID(repoGroup, prID)
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
			"pr_number":  pr.PRNumber,
			"repo_group": pr.RepoGroup,
			"platform":   pr.Platform,
			"actor":      "discord",
			"error":      err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to approve PR: %v", err))
		return
	}

	pr.IsApproved = true
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "approved",
		Actor:     m.Author.Username,
	})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number":     pr.PRNumber,
		"repo_group":    pr.RepoGroup,
		"platform":      pr.Platform,
		"actor":         "discord",
		"added_to_queue": true,
	})

	if b.queueMgr != nil {
		if err := b.queueMgr.AddToQueue(pr); err != nil {
			slog.Warn("discord bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
		} else {
			go b.queueMgr.CheckQueue()
		}
	}

	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber))
}

// handleClosePR handles !close command
func (b *DiscordBot) handleClosePR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!close <repo_group> <pr_id>`")
		return
	}

	repoGroup := args[1]
	prID := args[2]

	pr, _ := getPRByID(repoGroup, prID)
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
			"pr_number":  pr.PRNumber,
			"repo_group": pr.RepoGroup,
			"platform":   pr.Platform,
			"actor":      "discord",
			"error":      err.Error(),
		})
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to close PR: %v", err))
		return
	}

	pr.State = "closed"
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number":  pr.PRNumber,
		"repo_group": pr.RepoGroup,
		"platform":   pr.Platform,
		"actor":      "discord",
	})

	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d closed.", pr.PRNumber))
}

// handleReopenPR handles !reopen command
func (b *DiscordBot) handleReopenPR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!reopen <repo_group> <pr_id>`")
		return
	}

	repoGroup := args[1]
	prID := args[2]

	pr, _ := getPRByID(repoGroup, prID)
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
			"pr_number":  pr.PRNumber,
			"repo_group": pr.RepoGroup,
			"platform":   pr.Platform,
			"actor":      "discord",
			"error":      err.Error(),
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
		"pr_number":  pr.PRNumber,
		"repo_group": pr.RepoGroup,
		"platform":   pr.Platform,
		"actor":      "discord",
	})

	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d reopened.", pr.PRNumber))
}

// handleMarkSpam handles !spam command
func (b *DiscordBot) handleMarkSpam(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!spam <repo_group> <pr_id>`")
		return
	}

	repoGroup := args[1]
	prID := args[2]

	pr, _ := getPRByID(repoGroup, prID)
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
		"pr_number":  pr.PRNumber,
		"repo_group": pr.RepoGroup,
		"platform":   pr.Platform,
		"actor":      "discord",
	})

	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group != nil {
		client := b.getClientForPlatform(pr.Platform)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number":  pr.PRNumber,
					"repo_group": pr.RepoGroup,
					"platform":   pr.Platform,
					"actor":      "discord",
					"error":      err.Error(),
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

// handleShowQueue handles !queue command
func (b *DiscordBot) handleShowQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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

// handleRecheckQueue handles !recheck command
func (b *DiscordBot) handleRecheckQueue(s *discordgo.Session, m *discordgo.MessageCreate) {
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}

	go b.queueMgr.CheckQueue()
	s.ChannelMessageSend(m.ChannelID, "Queue recheck triggered.")
}

// handleClearQueue handles !queue_clear command
func (b *DiscordBot) handleClearQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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

// handleRemoveFromQueue handles !queue_remove <repo_group> <pr_id> command
func (b *DiscordBot) handleRemoveFromQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
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

// handleShowConfig handles !config command
func (b *DiscordBot) handleShowConfig(s *discordgo.Session, m *discordgo.MessageCreate) {
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

// getClientForPlatform returns the platform client
func (b *DiscordBot) getClientForPlatform(platform string) platforms.PlatformClient {
	if b.clients == nil {
		return nil
	}
	return b.clients[platforms.PlatformType(platform)]
}

// handleRebasePR handles !rebase command
func (b *DiscordBot) handleRebasePR(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: !rebase repo_group pr_number")
		return
	}

	repoGroup := parts[1]
	var prNumber int
	fmt.Sscanf(parts[2], "%d", &prNumber)
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

	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/rebase",
		b.cfg.Server.Listen, repoGroup, prNumber)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.cfg.Auth.JWTSecret)

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

// handleStats handles !stats command
func (b *DiscordBot) handleStats(s *discordgo.Session, m *discordgo.MessageCreate) {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.cfg.Auth.JWTSecret)

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

// handleVersion handles !version command
func (b *DiscordBot) handleVersion(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("**Asika**\nVersion: `%s`", version.Version))
}

// handleCherryPickPR handles !cherry-pick command
func (b *DiscordBot) handleCherryPickPR(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 4 {
		s.ChannelMessageSend(m.ChannelID, "Usage: !cherry-pick repo_group pr_number target_branch")
		return
	}

	repoGroup := parts[1]
	var prNumber int
	fmt.Sscanf(parts[2], "%d", &prNumber)
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

	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/cherry-pick",
		b.cfg.Server.Listen, repoGroup, prNumber)
	body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.Auth.JWTSecret)

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
