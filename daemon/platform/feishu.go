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

// Bot wraps the Feishu/Lark bot with Asika management functionality.
type FeishuBot struct {
	cfg          *models.Config
	clients      map[platforms.PlatformType]platforms.PlatformClient
	queueMgr     *queue.Manager
	syncerRef    *syncer.Syncer
	spamDetector *syncer.SpamDetector
	notifier     *notifier.FeishuNotifier
	adminIDs     map[string]bool
	stop         chan struct{}
	feishuCfg    models.FeishuConfig
}

// NewBot creates a new Feishu bot.
func NewFeishuBot(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	n *notifier.FeishuNotifier,
) *FeishuBot {
	b := &FeishuBot{
		cfg:          cfg,
		clients:      clients,
		queueMgr:     queueMgr,
		syncerRef:    syncerRef,
		spamDetector: spamDetector,
		notifier:     n,
		adminIDs:     make(map[string]bool),
		stop:         make(chan struct{}),
		feishuCfg:    cfg.Feishu,
	}
	for _, id := range cfg.Feishu.AdminIDs {
		b.adminIDs[id] = true
	}
	return b
}

// Start starts the bot (sets up HTTP handlers if needed via external routing).
func (b *FeishuBot) Start() {
	slog.Info("starting feishu interactive bot")
}

// Stop stops the bot gracefully.
func (b *FeishuBot) Stop() {
	close(b.stop)
	slog.Info("feishu bot stopped")
}

// HandleEvent handles an incoming Feishu event (called from HTTP handler).
// Returns a response body or nil if no response needed.
func (b *FeishuBot) HandleEvent(ctx context.Context, body []byte) (interface{}, error) {
	var event struct {
		Schema string          `json:"schema"`
		Header struct {
			EventType string `json:"event_type"`
			Token     string `json:"token"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		slog.Error("feishu: failed to parse event", "error", err)
		return nil, err
	}

	switch event.Header.EventType {
	case "im.message.receive_v1":
		return b.handleMessageEvent(ctx, event.Event)
	case "url_verification":
		return b.handleURLVerification(event.Event)
	default:
		slog.Debug("feishu: unhandled event type", "type", event.Header.EventType)
	}

	return nil, nil
}

// handleURLVerification handles the URL verification challenge.
func (b *FeishuBot) handleURLVerification(raw json.RawMessage) (interface{}, error) {
	var challenge struct {
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return nil, err
	}

	return map[string]string{
		"challenge": challenge.Challenge,
	}, nil
}

// handleMessageEvent handles incoming messages (commands).
func (b *FeishuBot) handleMessageEvent(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var msg struct {
		Sender struct {
			SenderID struct {
				UserID string `json:"user_id"`
			} `json:"sender_id"`
		} `json:"sender"`
		Message struct {
			MessageID   string `json:"message_id"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			Content     string `json:"content"`
			MessageType string `json:"message_type"`
		} `json:"message"`
	}

	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message event: %w", err)
	}

	senderID := msg.Sender.SenderID.UserID
	chatID := msg.Message.ChatID
	contentStr := msg.Message.Content

	// Parse text content from Feishu message JSON
	text := b.parseMessageText(contentStr)
	if text == "" {
		return nil, nil
	}

	slog.Info("feishu bot: received message", "sender", senderID, "chat", chatID, "text", text)

	// Build reply message text
	reply := b.processCommand(senderID, text)

	if reply != "" {
		return map[string]interface{}{
			"msg_type": "text",
			"content": map[string]interface{}{
				"text": reply,
			},
		}, nil
	}

	return nil, nil
}

// parseMessageText extracts text content from Feishu message JSON.
// Feishu wraps message content in a JSON string like {"text":"hello"}.
func (b *FeishuBot) parseMessageText(contentStr string) string {
	if contentStr == "" {
		return ""
	}

	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(contentStr), &content); err != nil {
		// Try raw text
		return strings.TrimSpace(contentStr)
	}
	return strings.TrimSpace(content.Text)
}

// processCommand processes a text command and returns a reply string.
func (b *FeishuBot) processCommand(senderID, text string) string {
	if !b.isAdmin(senderID) {
		return "Access denied. Admin only."
	}

	lower := strings.ToLower(text)
	parts := strings.Fields(text)

	switch {
	case lower == "help" || lower == "/help":
		return b.helpText()

	case lower == "prs" || lower == "/prs":
		return b.listPRsText("")

	case strings.HasPrefix(lower, "prs ") || strings.HasPrefix(lower, "/prs "):
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		return b.listPRsText(groupName)

	case strings.HasPrefix(lower, "pr ") || strings.HasPrefix(lower, "/pr "):
		if len(parts) < 3 {
			return "Usage: pr <repo_group> <pr_number>"
		}
		return b.showPRText(parts[1], parts[2])

	case strings.HasPrefix(lower, "approve ") || strings.HasPrefix(lower, "/approve "):
		if len(parts) < 3 {
			return "Usage: approve <repo_group> <pr_id>"
		}
		return b.doApprove(senderID, parts[1], parts[2])

	case strings.HasPrefix(lower, "close ") || strings.HasPrefix(lower, "/close "):
		if len(parts) < 3 {
			return "Usage: close <repo_group> <pr_id>"
		}
		return b.doClose(senderID, parts[1], parts[2])

	case strings.HasPrefix(lower, "reopen ") || strings.HasPrefix(lower, "/reopen "):
		if len(parts) < 3 {
			return "Usage: reopen <repo_group> <pr_id>"
		}
		return b.doReopen(senderID, parts[1], parts[2])

	case strings.HasPrefix(lower, "spam ") || strings.HasPrefix(lower, "/spam "):
		if len(parts) < 3 {
			return "Usage: spam <repo_group> <pr_id>"
		}
		return b.doMarkSpam(senderID, parts[1], parts[2])

	case lower == "queue" || lower == "/queue":
		return b.showQueueText("")

	case strings.HasPrefix(lower, "queue ") || strings.HasPrefix(lower, "/queue "):
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		return b.showQueueText(groupName)

	case lower == "recheck" || lower == "/recheck":
		if b.queueMgr != nil {
			go b.queueMgr.CheckQueue()
			return "Queue recheck triggered."
		}
		return "Queue manager not initialized."

	case lower == "config" || lower == "/config":
		return b.showConfigText()

	case strings.HasPrefix(lower, "stalecheck") || strings.HasPrefix(lower, "/stalecheck") || strings.HasPrefix(lower, "stale-check") || strings.HasPrefix(lower, "/stale-check"):
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		return b.doStaleCheckText(groupName)

	case strings.HasPrefix(lower, "unstale ") || strings.HasPrefix(lower, "/unstale "):
		if len(parts) < 3 {
			return "Usage: unstale <repo_group> <pr_number>"
		}
		return b.doUnstale(senderID, parts[1], parts[2])

	case strings.HasPrefix(lower, "rebase ") || strings.HasPrefix(lower, "/rebase "):
		if len(parts) < 3 {
			return "Usage: rebase <repo_group> <pr_number>"
		}
		return b.doRebase(senderID, parts[1], parts[2])

	case lower == "stats" || lower == "/stats":
		return b.showStatsText()

	case lower == "version" || lower == "/version":
		return b.showVersionText()

	case strings.HasPrefix(lower, "cherry-pick ") || strings.HasPrefix(lower, "/cherry-pick "):
		if len(parts) < 4 {
			return "Usage: cherry-pick <repo_group> <pr_number> <target_branch>"
		}
		return b.doCherryPick(senderID, parts[1], parts[2], parts[3])

	default:
		return fmt.Sprintf("Unknown command: %s\nTry 'help' for available commands.", text)
	}
}

func (b *FeishuBot) helpText() string {
  return `Asika Feishu Bot Commands:
  help          - Show this help
  prs [group]   - List PRs
  pr <group> <num> - Show PR details
  approve <group> <id> - Approve PR
  close <group> <id>   - Close PR
  reopen <group> <id>  - Reopen PR
  spam <group> <id>    - Mark as spam
  queue [group] - Show merge queue
  recheck       - Trigger queue recheck
  config        - Show config summary
  stalecheck [group] - Check for stale PRs
  unstale <group> <id> - Remove stale label
  rebase <group> <num> - Rebase a PR
  cherry-pick <group> <num> <branch> - Cherry-pick a merged PR
  stats         - Show DORA metrics
  version       - Show version info`
}

func (b *FeishuBot) listPRsText(repoGroup string) string {
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
			emoji, pr.PRNumber, truncateStr(pr.Title, 35), pr.Author, pr.State))
	}
	return sb.String()
}

func (b *FeishuBot) showPRText(repoGroup, prID string) string {
	pr, _ := getPRRecord(repoGroup, prID)
	if pr == nil {
		return fmt.Sprintf("PR %s not found in %s", prID, repoGroup)
	}
	return fmt.Sprintf(
		"PR #%d - %s\n  Author: %s | State: %s\n  Platform: %s | Spam: %v\n  Labels: %s",
		pr.PRNumber, pr.Title, pr.Author, pr.State,
		pr.Platform, pr.SpamFlag, strings.Join(pr.Labels, ", "),
	)
}

func (b *FeishuBot) doApprove(senderID, repoGroup, prID string) string {
	pr, _ := getPRRecord(repoGroup, prID)
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
			"pr_number":  pr.PRNumber,
			"repo_group": pr.RepoGroup,
			"platform":   pr.Platform,
			"actor":      "feishu",
			"error":      err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}

	pr.IsApproved = true
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "approved",
		Actor:     senderID,
	})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number":     pr.PRNumber,
		"repo_group":    pr.RepoGroup,
		"platform":      pr.Platform,
		"actor":         "feishu",
		"added_to_queue": true,
	})

	if b.queueMgr != nil {
		if err := b.queueMgr.AddToQueue(pr); err != nil {
			slog.Warn("feishu bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
		} else {
			go b.queueMgr.CheckQueue()
		}
	}

	return fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber)
}

func (b *FeishuBot) doClose(senderID, repoGroup, prID string) string {
	pr, _ := getPRRecord(repoGroup, prID)
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
	if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number":  pr.PRNumber,
			"repo_group": pr.RepoGroup,
			"platform":   pr.Platform,
			"actor":      "feishu",
			"error":      err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}

	pr.State = "closed"
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)

	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number":  pr.PRNumber,
		"repo_group": pr.RepoGroup,
		"platform":   pr.Platform,
		"actor":      "feishu",
	})

	return fmt.Sprintf("PR #%d closed.", pr.PRNumber)
}

func (b *FeishuBot) doReopen(senderID, repoGroup, prID string) string {
	pr, _ := getPRRecord(repoGroup, prID)
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
			"pr_number":  pr.PRNumber,
			"repo_group": pr.RepoGroup,
			"platform":   pr.Platform,
			"actor":      "feishu",
			"error":      err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
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
		"actor":      "feishu",
	})

	return fmt.Sprintf("PR #%d reopened.", pr.PRNumber)
}

func (b *FeishuBot) doMarkSpam(senderID, repoGroup, prID string) string {
	pr, _ := getPRRecord(repoGroup, prID)
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
		"pr_number":  pr.PRNumber,
		"repo_group": pr.RepoGroup,
		"platform":   pr.Platform,
		"actor":      "feishu",
	})

	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group != nil {
		client := b.getClient(pr.Platform)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number":  pr.PRNumber,
					"repo_group": pr.RepoGroup,
					"platform":   pr.Platform,
					"actor":      "feishu",
					"error":      err.Error(),
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

func (b *FeishuBot) showQueueText(repoGroup string) string {
	if repoGroup == "" {
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
		if strings.HasPrefix(string(key), repoGroup+"#") {
			items = append(items, item)
		}
		return nil
	})

	if len(items) == 0 {
		return fmt.Sprintf("Queue empty for %s", repoGroup)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Queue - %s:\n", repoGroup))
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("  %s (%s)\n", item.PRID, item.Status))
	}
	return sb.String()
}

func (b *FeishuBot) showConfigText() string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	groups := config.GetRepoGroups(cfg)
	return fmt.Sprintf(
		"Asika Config:\n  Server: %s (%s)\n  DB: %s\n  Events: %s\n  Spam: %v\n  Repo Groups: %d\n  Notify Channels: %d",
		cfg.Server.Listen, cfg.Server.Mode, cfg.Database.Path,
		cfg.Events.Mode, cfg.Spam.Enabled, len(groups), len(cfg.Notify),
	)
}

func (b *FeishuBot) isAdmin(userID string) bool {
	if len(b.adminIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}

func (b *FeishuBot) getClient(platform string) platforms.PlatformClient {
	if b.clients == nil {
		return nil
	}
	return b.clients[platforms.PlatformType(platform)]
}

// getPRRecord finds a PR by repo group and ID or number.
func getPRRecord(repoGroup, idOrNumber string) (*models.PRRecord, error) {
	var found *models.PRRecord
	prNumber, _ := strconv.Atoi(idOrNumber)

	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup {
			if pr.ID == idOrNumber || (prNumber > 0 && pr.PRNumber == prNumber) {
				found = &pr
			}
		}
		return nil
	})

	if found == nil {
		return nil, fmt.Errorf("PR not found")
	}
	return found, nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func (b *FeishuBot) doStaleCheckText(repoGroup string) string {
	cfg := config.Current()
	if cfg == nil || !cfg.Stale.Enabled {
		return "Stale PR management is not enabled in config."
	}

	if repoGroup == "" {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			return "No repo groups configured."
		}
		repoGroup = groups[0].Name
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}

	var lines []string
	lines = append(lines, "Stale PR Check for "+repoGroup+":")

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
		prs, err := client.ListPRs(ctx, owner, repo, "open")
		cancel()
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: error listing PRs", pt))
			continue
		}
		for _, pr := range prs {
			if cfg.Stale.SkipDraftPRs && pr.IsDraft {
				continue
			}
			isExempt := false
			for _, exempt := range cfg.Stale.ExemptLabels {
				if hasLabelStr(pr.Labels, exempt, "") {
					isExempt = true
					break
				}
			}
			if isExempt {
				continue
			}
			days := int(time.Since(pr.UpdatedAt).Hours() / 24)
			hasStale := hasLabelStr(pr.Labels, cfg.Stale.StaleLabel, "stale")
			if hasStale && cfg.Stale.DaysUntilClose > 0 && days >= cfg.Stale.DaysUntilStale+cfg.Stale.DaysUntilClose {
				lines = append(lines, fmt.Sprintf("- [CLOSE] #%d %s", pr.PRNumber, truncateStr(pr.Title, 40)))
			} else if !hasStale && days >= cfg.Stale.DaysUntilStale {
				lines = append(lines, fmt.Sprintf("- [MARK] #%d %s", pr.PRNumber, truncateStr(pr.Title, 40)))
			}
		}
	}

	if len(lines) == 1 {
		return "No stale PRs found in " + repoGroup
	}
	return strings.Join(lines, "\n")
}

func (b *FeishuBot) doUnstale(senderID, repoGroup, prNumberStr string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}

	label := cfg.Stale.StaleLabel
	if label == "" {
		label = "stale"
	}

	var n int
	fmt.Sscanf(prNumberStr, "%d", &n)

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
		err := client.RemoveLabel(ctx, owner, repo, n, label)
		cancel()
		if err != nil {
			continue
		}
		removed = true
	}

	if !removed {
		return "Failed to remove stale label."
	}
	return "Stale label removed from PR #" + prNumberStr + " in " + repoGroup
}

func (b *FeishuBot) doRebase(senderID, repoGroup, prNumberStr string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}

	var prNumber int
	fmt.Sscanf(prNumberStr, "%d", &prNumber)
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

	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/rebase",
		cfg.Server.Listen, repoGroup, prNumber)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Auth.JWTSecret)

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

func (b *FeishuBot) showStatsText() string {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.cfg.Auth.JWTSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch stats: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Error parsing stats response"
	}

	var lines []string
	lines = append(lines, "DORA Metrics")
	lines = append(lines, "─────────────")

	if v, ok := result["deployment_frequency"]; ok {
		lines = append(lines, fmt.Sprintf("Deployments/Day: %.2f", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		lines = append(lines, fmt.Sprintf("Lead Time: %s", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		lines = append(lines, fmt.Sprintf("Failure Rate: %.1f%%", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		lines = append(lines, fmt.Sprintf("MTTR: %s", utils.FormatHours(utils.ToFloat64(v))))
	}

	lines = append(lines, "")
	lines = append(lines, "Overview")
	lines = append(lines, "─────────────")
	if v, ok := result["total_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Total PRs: %v", v))
	}
	if v, ok := result["open_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Open: %v", v))
	}
	if v, ok := result["merged_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Merged: %v", v))
	}
	if v, ok := result["queue_items"]; ok {
		lines = append(lines, fmt.Sprintf("Queue: %v", v))
	}

	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		lines = append(lines, "")
		lines = append(lines, "By Repo Group")
		lines = append(lines, "─────────────")
		for k, v := range byGroup {
			lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
		}
	}

	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		lines = append(lines, "")
		lines = append(lines, "By Platform")
		lines = append(lines, "─────────────")
		for k, v := range byPlat {
			lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
		}
	}

	return strings.Join(lines, "\n")
}



func (b *FeishuBot) showVersionText() string {
	return fmt.Sprintf("Asika\nVersion: %s", version.Version)
}

func (b *FeishuBot) doCherryPick(senderID, repoGroup, prNumberStr, targetBranch string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}

	var prNumber int
	fmt.Sscanf(prNumberStr, "%d", &prNumber)
	if prNumber == 0 {
		return "Invalid PR number: " + prNumberStr
	}

	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/cherry-pick",
		cfg.Server.Listen, repoGroup, prNumber)
	body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Auth.JWTSecret)

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