package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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
	stopOnce      sync.Once
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
	feishuNotifier *notifier.FeishuNotifier,
	adminIDs []string,
	operatorIDs []string,
	viewerIDs []string,
) *Bot {
	token, _ := auth.GenerateInternalToken()
	b := &Bot{
		cfg:           cfg,
		clients:       clients,
		queueMgr:      queueMgr,
		syncerRef:     syncerRef,
		spamDetector:  spamDetector,
		notifier:      feishuNotifier,
		adminIDs:      make(map[string]bool),
		operatorIDs:   make(map[string]bool),
		viewerIDs:     make(map[string]bool),
		stop:          make(chan struct{}),
		feishuCfg:     cfg.Feishu,
		internalToken: token,
	}
	for _, id := range adminIDs {
		b.adminIDs[id] = true
	}
	for _, id := range operatorIDs {
		b.operatorIDs[id] = true
	}
	for _, id := range viewerIDs {
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
	b.stopOnce.Do(func() {
		close(b.stop)
	})
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
	userID := msg.Sender.SenderID.UserID
	contentStr := msg.Message.Content
	text := b.parseMessageText(contentStr)
	if text == "" {
		return nil, nil
	}
	slog.Info("feishu bot: received message", "sender", userID, "text", text)
	reply := b.processCommand(userID, text)
	if reply != "" {
		// If reply contains API key, send via DM instead
		if strings.Contains(reply, "ak_") {
			b.sendDM(userID, reply)
			return map[string]interface{}{
				"msg_type": "text",
				"content":  map[string]interface{}{"text": "🔑 API key created! Check your DMs."},
			}, nil
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
		slog.Warn("feishu: no admin/operator/viewer IDs configured, rejecting all users")
		return false
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

var feishuHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (b *Bot) getTenantAccessToken() (string, error) {
	body, _ := json.Marshal(map[string]string{
		"app_id":     b.feishuCfg.AppID,
		"app_secret": b.feishuCfg.AppSecret,
	})
	tokenReq, _ := http.NewRequest("POST",
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		strings.NewReader(string(body)))
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := feishuHTTPClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to request tenant_access_token: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(tokenResp.Body, 1024))
		return "", fmt.Errorf("tenant_access_token request failed: HTTP %d: %s", tokenResp.StatusCode, string(body))
	}

	var tokenResult struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, 4096)).Decode(&tokenResult); err != nil {
		return "", fmt.Errorf("failed to decode tenant_access_token response: %w", err)
	}
	if tokenResult.Code != 0 {
		return "", fmt.Errorf("tenant_access_token error: code=%d msg=%s", tokenResult.Code, tokenResult.Msg)
	}
	if tokenResult.TenantAccessToken == "" {
		return "", fmt.Errorf("empty tenant_access_token, code: %d", tokenResult.Code)
	}
	return tokenResult.TenantAccessToken, nil
}

// sendDM sends a direct message to a Feishu user and schedules deletion.
func (b *Bot) sendDM(receiverID string, text string) {
	token, err := b.getTenantAccessToken()
	if err != nil {
		slog.Warn("feishu: failed to get tenant_access_token for DM", "error", err)
		return
	}

	sendURL := "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id"
	sendBody, _ := json.Marshal(map[string]interface{}{
		"receive_id": receiverID,
		"msg_type":   "text",
		"content":    map[string]interface{}{"text": text},
	})
	sendReq, _ := http.NewRequest("POST", sendURL, strings.NewReader(string(sendBody)))
	sendReq.Header.Set("Authorization", "Bearer "+token)
	sendReq.Header.Set("Content-Type", "application/json")
	sendResp, err := feishuHTTPClient.Do(sendReq)
	if err != nil {
		slog.Warn("feishu: failed to send DM", "error", err)
		return
	}
	defer sendResp.Body.Close()

	var sendResult struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	json.NewDecoder(sendResp.Body).Decode(&sendResult)
	if sendResult.Data.MessageID == "" {
		slog.Warn("feishu: DM sent but no message_id", "code", sendResult.Code)
		return
	}

	go func() {
		time.Sleep(2 * time.Minute)
		delURL := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s", sendResult.Data.MessageID)
		delReq, _ := http.NewRequest("DELETE", delURL, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		delReq.Header.Set("Content-Type", "application/json")
		resp, err := feishuHTTPClient.Do(delReq)
		if err != nil {
			slog.Warn("feishu: failed to auto-delete DM", "message_id", sendResult.Data.MessageID, "error", err)
			return
		}
		resp.Body.Close()
		slog.Info("feishu: auto-deleted DM API key message", "message_id", sendResult.Data.MessageID)
	}()
}

// scheduleDelete deletes a Feishu message after the given delay.
// Uses the Feishu recall API: DELETE /open-apis/im/v1/messages/:message_id
func (b *Bot) scheduleDelete(messageID string, delay time.Duration) {
	time.Sleep(delay)

	token, err := b.getTenantAccessToken()
	if err != nil {
		slog.Warn("feishu: failed to get tenant_access_token", "error", err)
		return
	}

	delURL := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s", messageID)
	delReq, _ := http.NewRequest("DELETE", delURL, nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	delReq.Header.Set("Content-Type", "application/json")
	delResp, err := feishuHTTPClient.Do(delReq)
	if err != nil {
		slog.Warn("feishu: failed to delete message", "error", err)
		return
	}
	defer delResp.Body.Close()
	slog.Info("feishu: auto-deleted API key message", "message_id", messageID)
}
