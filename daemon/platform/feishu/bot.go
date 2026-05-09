package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
	operatorIDs   map[string]bool
	viewerIDs     map[string]bool
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
		operatorIDs:   make(map[string]bool),
		viewerIDs:     make(map[string]bool),
		stop:          make(chan struct{}),
		feishuCfg:     cfg.Feishu,
		internalToken: token,
	}
	for _, id := range cfg.Feishu.AdminIDs {
		b.adminIDs[id] = true
	}
	for _, id := range cfg.Feishu.OperatorIDs {
		b.operatorIDs[id] = true
	}
	for _, id := range cfg.Feishu.ViewerIDs {
		b.viewerIDs[id] = true
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
		// Auto-delete API key messages after 2 minutes
		if strings.Contains(reply, "ak_") {
			go b.scheduleDelete(msg.Message.MessageID, 2*time.Minute)
		}
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
	if len(b.adminIDs) == 0 && len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}

func (b *Bot) isOperator(userID string) bool {
	if b.isAdmin(userID) {
		return true
	}
	// If only adminIDs are configured (no operator/viewer IDs), nobody else is operator
	if len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return false
	}
	return b.operatorIDs[userID]
}

// getUserRole returns the role name for the user: "admin", "operator", or "viewer"
func (b *Bot) getUserRole(userID string) string {
	if b.isAdmin(userID) {
		return "admin"
	}
	if b.isOperator(userID) {
		return "operator"
	}
	return "viewer"
}

// scheduleDelete deletes a Feishu message after the given delay.
// Uses the Feishu recall API: DELETE /open-apis/im/v1/messages/:message_id
func (b *Bot) scheduleDelete(messageID string, delay time.Duration) {
	time.Sleep(delay)

	// Step 1: Get tenant_access_token
	tokenReq, _ := http.NewRequest("POST",
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", strings.NewReader(
			fmt.Sprintf(`{"app_id":"%s","app_secret":"%s"}`, b.feishuCfg.AppID, b.feishuCfg.AppSecret)))
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		slog.Warn("feishu: failed to get tenant_access_token", "error", err)
		return
	}
	defer tokenResp.Body.Close()

	var tokenResult struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenResult)
	if tokenResult.TenantAccessToken == "" {
		slog.Warn("feishu: empty tenant_access_token", "code", tokenResult.Code, "msg", tokenResult.Msg)
		return
	}

	// Step 2: Delete the message
	delURL := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s", messageID)
	delReq, _ := http.NewRequest("DELETE", delURL, nil)
	delReq.Header.Set("Authorization", "Bearer "+tokenResult.TenantAccessToken)
	delReq.Header.Set("Content-Type", "application/json")
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		slog.Warn("feishu: failed to delete message", "error", err)
		return
	}
	defer delResp.Body.Close()
	slog.Info("feishu: auto-deleted API key message", "message_id", messageID)
}
