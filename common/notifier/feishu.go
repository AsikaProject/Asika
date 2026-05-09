package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// FeishuNotifier sends notifications via Feishu/Lark
type FeishuNotifier struct {
	webhookURL    string
	appID         string
	appSecret     string
	chatIDs       []string
	client        *lark.Client
	receiveIDType string
}

// NewFeishuNotifier creates a new Feishu notifier.
// Supports two modes:
//  1. Webhook mode: config["webhook_url"] is set (simpler, no app credentials)
//  2. App mode: config["app_id"] + config["app_secret"] are set (full SDK)
func NewFeishuNotifier(config map[string]interface{}) *FeishuNotifier {
	n := &FeishuNotifier{}

	webhookURL, _ := config["webhook_url"].(string)
	appID, _ := config["app_id"].(string)
	appSecret, _ := config["app_secret"].(string)
	receiveIDType, _ := config["receive_id_type"].(string)
	if receiveIDType == "" {
		receiveIDType = "chat_id" // default
	}

	n.webhookURL = webhookURL
	n.receiveIDType = receiveIDType

	// Parse chat IDs (same as Telegram's "to" config)
	if toList, ok := config["to"].([]interface{}); ok {
		for _, t := range toList {
			if s, ok := t.(string); ok && s != "" {
				n.chatIDs = append(n.chatIDs, s)
			}
		}
	}

	if appID != "" && appSecret != "" {
		n.appID = appID
		n.appSecret = appSecret
		n.client = lark.NewClient(appID, appSecret)
	}

	return n
}

// Type returns the type of notifier
func (n *FeishuNotifier) Type() string {
	return "feishu"
}

// WebhookURL returns the configured webhook URL.
func (n *FeishuNotifier) WebhookURL() string {
	return n.webhookURL
}

// Send sends a notification via Feishu webhook or IM API.
func (n *FeishuNotifier) Send(ctx context.Context, title, body string) error {
	// App mode: use SDK to send to multiple recipients
	if n.client != nil && len(n.chatIDs) > 0 {
		return n.sendViaApp(ctx, title, body)
	}

	// Webhook mode: send to single webhook URL
	if n.webhookURL != "" {
		return n.sendViaWebhook(ctx, title, body)
	}

	return fmt.Errorf("feishu: no webhook_url or app credentials configured")
}

// sendViaWebhook sends via simple webhook (like WeCom)
func (n *FeishuNotifier) sendViaWebhook(ctx context.Context, title, body string) error {
	content := formatFeishuCard(title, body)

	payload := map[string]interface{}{
		"msg_type": "interactive",
		"card":     content,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: marshal payload failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", n.webhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		slog.Error("feishu webhook failed", "status", resp.StatusCode, "body", string(bodyBytes))
		return fmt.Errorf("feishu webhook returned status %d", resp.StatusCode)
	}

	slog.Info("feishu notification sent via webhook")
	return nil
}

// sendViaApp sends via Feishu IM API (requires app credentials)
func (n *FeishuNotifier) sendViaApp(ctx context.Context, title, body string) error {
	if n.client == nil {
		return fmt.Errorf("feishu: client not initialized")
	}

	// Build message content
	content := fmt.Sprintf(`{"text":"%s\n\n%s"}`, escapeFeishu(title), escapeFeishu(body))

	// Determine receive ID type
	receiveIDType := larkim.ReceiveIdTypeChatId // default
	switch n.receiveIDType {
	case "open_id":
		receiveIDType = larkim.ReceiveIdTypeOpenId
	case "user_id":
		receiveIDType = larkim.ReceiveIdTypeUserId
	case "email":
		receiveIDType = larkim.ReceiveIdTypeEmail
	case "chat_id":
		receiveIDType = larkim.ReceiveIdTypeChatId
	}

	var lastErr error
	targets := n.chatIDs
	if len(targets) == 0 {
		return fmt.Errorf("feishu: no chat IDs configured for app mode")
	}

	for _, receiveID := range targets {
		resp, err := n.client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(receiveIDType).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				MsgType(larkim.MsgTypeText).
				ReceiveId(receiveID).
				Content(content).
				Build()).
			Build())

		if err != nil {
			slog.Error("feishu app: failed to send", "target", receiveID, "error", err)
			lastErr = err
			continue
		}
		if !resp.Success() {
			slog.Error("feishu app: send failed", "target", receiveID, "code", resp.Code, "msg", resp.Msg)
			lastErr = fmt.Errorf("send failed: %s", resp.Msg)
			continue
		}
		slog.Info("feishu notification sent via app", "target", receiveID)
	}

	if lastErr != nil {
		return fmt.Errorf("feishu app send failed for some recipients: %w", lastErr)
	}
	return nil
}

// formatFeishuCard builds a simple Feishu card message.
func formatFeishuCard(title, body string) map[string]interface{} {
	return map[string]interface{}{
		"header": map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": title,
			},
			"template": "blue",
		},
		"elements": []map[string]interface{}{
			{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "lark_md",
					"content": body,
				},
			},
		},
	}
}

// feishuCardPayload is used for constructing interactive card messages.
// Exported for use by the bot package.
func FeishuCardPayload(title, body string) map[string]interface{} {
	return formatFeishuCard(title, body)
}

func escapeFeishu(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
