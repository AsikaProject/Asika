package slack

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

// Bot wraps the Slack bot with Asika management functionality.
type Bot struct {
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

// NewBot creates a new Slack bot.
func NewBot(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	slackNotifier *notifier.SlackBotNotifier,
	adminIDs []string,
) *Bot {
	b := &Bot{
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
func (b *Bot) SetSocketClient(sc *socketmode.Client) {
	b.socketClient = sc
}

// SetClients sets the platform clients map.
func (b *Bot) SetClients(clients map[platforms.PlatformType]platforms.PlatformClient) {
	b.clients = clients
}

// SetQueueManager sets the queue manager.
func (b *Bot) SetQueueManager(qm *queue.Manager) {
	b.queueMgr = qm
}

// SetSyncer sets the syncer reference.
func (b *Bot) SetSyncer(s *syncer.Syncer) {
	b.syncerRef = s
}

// SetSpamDetector sets the spam detector.
func (b *Bot) SetSpamDetector(sd *syncer.SpamDetector) {
	b.spamDetector = sd
}

// Start starts the Slack bot in Socket Mode.
func (b *Bot) Start() {
	if b.socketClient == nil {
		slog.Warn("slack bot: no socket client, skipping start")
		return
	}
	slog.Info("starting slack bot in socket mode")
	go b.socketClient.Run()
}

// Stop stops the Slack bot gracefully.
func (b *Bot) Stop() {
	close(b.stop)
	slog.Info("slack bot stopped")
}

// HandleEvent handles incoming Socket Mode events (exported for use by core).
func (b *Bot) HandleEvent(evt socketmode.Event, client *socketmode.Client) {
	b.handleEvent(evt, client)
}

func (b *Bot) handleEvent(evt socketmode.Event, client *socketmode.Client) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		client.Ack(*evt.Request)
		raw, ok := evt.Data.([]byte)
		if !ok {
			return
		}
		var msgEv slack.MessageEvent
		if err := json.Unmarshal(raw, &msgEv); err == nil && msgEv.Type == "message" {
			b.handleMessage(&msgEv, client)
			return
		}
		var wrapper struct {
			Type  string          `json:"type"`
			Event json.RawMessage `json:"event"`
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

func (b *Bot) handleMessage(ev *slack.MessageEvent, client *socketmode.Client) {
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
	case "queue_clear":
		b.handleClearQueue(ev, client, parts)
	case "queue_remove":
		b.handleRemoveFromQueue(ev, client, parts)
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

func (b *Bot) isAdmin(userID string) bool {
	if len(b.adminIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}

func (b *Bot) getClientForPlatform(platform string) platforms.PlatformClient {
	if b.clients == nil {
		return nil
	}
	return b.clients[platforms.PlatformType(platform)]
}

func (b *Bot) postMessage(client *socketmode.Client, channelID, text string) {
	client.PostMessage(channelID, slack.MsgOptionText(text, false))
}
