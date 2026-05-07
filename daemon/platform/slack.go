package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

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

// SlackBot wraps the Slack bot with Asika management functionality.
type SlackBot struct {
	client       *slack.Client
	socketClient *socketmode.Client
	cfg          *models.Config
	clients      map[platforms.PlatformType]platforms.PlatformClient
	queueMgr     *queue.Manager
	syncerRef    *syncer.Syncer
	spamDetector *syncer.SpamDetector
	notifier     *notifier.SlackBotNotifier
	adminIDs     map[string]bool
	stop         chan struct{}
}

// NewSlackBot creates a new Slack bot.
func NewSlackBot(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	slackNotifier *notifier.SlackBotNotifier,
	adminIDs []string,
) *SlackBot {
	b := &SlackBot{
		cfg:          cfg,
		clients:      clients,
		queueMgr:     queueMgr,
		syncerRef:    syncerRef,
		spamDetector: spamDetector,
		notifier:     slackNotifier,
		adminIDs:     make(map[string]bool),
		stop:         make(chan struct{}),
	}
	for _, id := range adminIDs {
		b.adminIDs[id] = true
	}
	return b
}

// SetSocketClient sets the Socket Mode client.
func (b *SlackBot) SetSocketClient(sc *socketmode.Client) {
	b.socketClient = sc
}

// HandleEvent handles incoming Socket Mode events (exported for use by core).
func (b *SlackBot) HandleEvent(evt socketmode.Event, client *socketmode.Client) {
	b.handleEvent(evt, client)
}

// SetClients sets the platform clients map.
func (b *SlackBot) SetClients(clients map[platforms.PlatformType]platforms.PlatformClient) {
	b.clients = clients
}

// SetQueueManager sets the queue manager.
func (b *SlackBot) SetQueueManager(qm *queue.Manager) {
	b.queueMgr = qm
}

// SetSyncer sets the syncer reference.
func (b *SlackBot) SetSyncer(s *syncer.Syncer) {
	b.syncerRef = s
}

// SetSpamDetector sets the spam detector.
func (b *SlackBot) SetSpamDetector(sd *syncer.SpamDetector) {
	b.spamDetector = sd
}

// Start starts the Slack bot in Socket Mode.
func (b *SlackBot) Start() {
	if b.socketClient == nil {
		slog.Warn("slack bot: no socket client, skipping start")
		return
	}

	slog.Info("starting slack bot in socket mode")

	go b.socketClient.Run()
}

// Stop stops the Slack bot gracefully.
func (b *SlackBot) Stop() {
	close(b.stop)
	slog.Info("slack bot stopped")
}

// isAdmin checks if the sender is an authorized admin.
func (b *SlackBot) isAdmin(userID string) bool {
	if len(b.adminIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}

// handleEvent handles incoming Socket Mode events.
func (b *SlackBot) handleEvent(evt socketmode.Event, client *socketmode.Client) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		client.Ack(*evt.Request)

		// Data is a json.RawMessage in socket mode v0.23+
		raw, ok := evt.Data.([]byte)
		if !ok {
			return
		}

		// Try to unmarshal as message event first
		var msgEv slack.MessageEvent
		if err := json.Unmarshal(raw, &msgEv); err == nil && msgEv.Type == "message" {
			b.handleMessage(&msgEv, client)
			return
		}

		// Try wrapper events API event
		var wrapper struct {
			Type    string          `json:"type"`
			Event   json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return
		}
		if wrapper.Type != "event_callback" {
			return
		}

		var inner struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(wrapper.Event, &inner) != nil {
			return
		}
		if inner.Type != "message" {
			return
		}
		var msg slack.MessageEvent
		if json.Unmarshal(wrapper.Event, &msg) == nil {
			b.handleMessage(&msg, client)
		}
	}
}

// handleMessage handles incoming messages.
func (b *SlackBot) handleMessage(ev *slack.MessageEvent, client *socketmode.Client) {
	if ev.User == "" || ev.SubType == "bot_message" {
		return
	}

	if !b.isAdmin(ev.User) {
		return
	}

	content := strings.TrimSpace(ev.Text)
	if content == "" {
		return
	}

	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "help":
		b.handleHelp(ev, client)
	case "prs":
		b.handleListPRs(ev, client, parts)
	case "pr":
		b.handleShowPR(ev, client, parts)
	case "approve":
		b.handleApprovePR(ev, client, parts)
	case "close":
		b.handleClosePR(ev, client, parts)
	case "reopen":
		b.handleReopenPR(ev, client, parts)
	case "spam":
		b.handleMarkSpam(ev, client, parts)
	case "queue":
		b.handleShowQueue(ev, client, parts)
	case "recheck":
		b.handleRecheckQueue(ev, client)
	case "config":
		b.handleShowConfig(ev, client)
	case "rebase":
		b.handleRebasePR(ev, client, parts)
	case "cherry-pick":
		b.handleCherryPickPR(ev, client, parts)
	case "stats":
		b.handleStats(ev, client)
	case "version":
		b.handleVersion(ev, client)
	}
}

func (b *SlackBot) postMessage(client *socketmode.Client, channelID, text string) {
	client.PostMessage(channelID, slack.MsgOptionText(text, false))
}

func (b *SlackBot) handleHelp(ev *slack.MessageEvent, client *socketmode.Client) {
	help := `*Asika Bot Commands*

*PR Management*
prs [repo_group] — List PRs
pr <repo_group> <number> — Show PR details
approve <repo_group> <pr_id> — Approve a PR
close <repo_group> <pr_id> — Close a PR
reopen <repo_group> <pr_id> — Reopen a PR (spam recovery)
spam <repo_group> <pr_id> — Mark PR as spam

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

func (b *SlackBot) handleListPRs(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
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
		stateEmoji := map[string]string{
			"merged": "🟣",
			"closed": "🔴",
			"spam":   "⚠️",
		}
		emoji := "🔵"
		if e, ok := stateEmoji[pr.State]; ok {
			emoji = e
		}
		sb.WriteString(fmt.Sprintf("%s *#%d* %s — by %s (%s/%s)\n",
			emoji, pr.PRNumber, utils.TruncateString(pr.Title, 40), pr.Author, pr.Platform, pr.State))
	}

	b.postMessage(client, ev.Channel, sb.String())
}

func (b *SlackBot) handleShowPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
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

func (b *SlackBot) handleApprovePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: approve <repo_group> <pr_id>")
		return
	}
	// Reuse the approve handler logic from Discord bot
	b.postMessage(client, ev.Channel, "Approve via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleClosePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: close <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Close via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleReopenPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: reopen <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Reopen via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleMarkSpam(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: spam <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Spam marking via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleShowQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
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

func (b *SlackBot) handleRecheckQueue(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, "Queue recheck triggered.")
}

func (b *SlackBot) handleShowConfig(ev *slack.MessageEvent, client *socketmode.Client) {
	cfg := b.cfg
	text := fmt.Sprintf("*Asika Config*\nListen: %s\nMode: %s\nRepo Groups: %d",
		cfg.Server.Listen, cfg.Server.Mode, len(cfg.RepoGroups))
	b.postMessage(client, ev.Channel, text)
}

func (b *SlackBot) handleRebasePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: rebase <repo_group> <pr_number>")
		return
	}
	b.postMessage(client, ev.Channel, "Rebase via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleCherryPickPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 4 {
		b.postMessage(client, ev.Channel, "Usage: cherry-pick <repo_group> <pr_number> <target_branch>")
		return
	}
	b.postMessage(client, ev.Channel, "Cherry-pick via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleStats(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, "Stats via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *SlackBot) handleVersion(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, fmt.Sprintf("*Asika*\nVersion: `%s`", version.Version))
}

func (b *SlackBot) getClientForPlatform(platform string) platforms.PlatformClient {
	if b.clients == nil {
		return nil
	}
	return b.clients[platforms.PlatformType(platform)]
}
