package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"asika/common/auth"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

// Bot wraps the Feishu/Lark bot with Asika management functionality.
type Bot struct {
	cfg           *models.Config
	clients       map[platforms.PlatformType]platforms.PlatformClient
	queueMgr      *queue.Manager
	syncerRef     *syncer.Syncer
	spamDetector  *syncer.SpamDetector
	notifier      *notifier.FeishuNotifier
	adminIDs      map[string]bool
	stop          chan struct{}
	feishuCfg     models.FeishuConfig
	internalToken string
}

// NewBot creates a new Feishu bot.
func NewBot(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	n *notifier.FeishuNotifier,
) *Bot {
	token, _ := auth.GenerateInternalToken()
	b := &Bot{
		cfg:           cfg,
		clients:       clients,
		queueMgr:      queueMgr,
		syncerRef:     syncerRef,
		spamDetector:  spamDetector,
		notifier:      n,
		adminIDs:      make(map[string]bool),
		stop:          make(chan struct{}),
		feishuCfg:     cfg.Feishu,
		internalToken: token,
	}
	for _, id := range cfg.Feishu.AdminIDs {
		b.adminIDs[id] = true
	}
	return b
}

// Start starts the bot.
func (b *Bot) Start() {
	slog.Info("starting feishu interactive bot")
}

// Stop stops the bot gracefully.
func (b *Bot) Stop() {
	close(b.stop)
	slog.Info("feishu bot stopped")
}

// HandleEvent handles an incoming Feishu event (called from HTTP handler).
func (b *Bot) HandleEvent(ctx context.Context, body []byte) (interface{}, error) {
	var event struct {
		Schema string `json:"schema"`
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

func (b *Bot) handleURLVerification(raw json.RawMessage) (interface{}, error) {
	var challenge struct {
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return nil, err
	}
	return map[string]string{"challenge": challenge.Challenge}, nil
}

func (b *Bot) handleMessageEvent(ctx context.Context, raw json.RawMessage) (interface{}, error) {
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
	contentStr := msg.Message.Content
	text := b.parseMessageText(contentStr)
	if text == "" {
		return nil, nil
	}
	slog.Info("feishu bot: received message", "sender", senderID, "text", text)
	reply := b.processCommand(senderID, text)
	if reply != "" {
		return map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]interface{}{"text": reply},
		}, nil
	}
	return nil, nil
}

func (b *Bot) parseMessageText(contentStr string) string {
	if contentStr == "" {
		return ""
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(contentStr), &content); err != nil {
		return strings.TrimSpace(contentStr)
	}
	return strings.TrimSpace(content.Text)
}

func (b *Bot) getClient(platform string) platforms.PlatformClient {
	if b.clients == nil {
		return nil
	}
	return b.clients[platforms.PlatformType(platform)]
}

func (b *Bot) isAdmin(userID string) bool {
	if len(b.adminIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}
