package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	webhookRequestTimeout  = 10 * time.Second
	webhookSignatureHeader = "X-Asika-Signature"
)

// WebhookNotifier sends notifications to a custom URL via HTTP POST.
type WebhookNotifier struct {
	url    string
	secret string
	client *http.Client
}

// NewWebhookNotifier creates a new outgoing webhook notifier from config.
// Supported config keys:
//   - url (required): the target HTTP/HTTPS URL
//   - secret (optional): HMAC-SHA256 secret for signing the payload
func NewWebhookNotifier(config map[string]interface{}) *WebhookNotifier {
	url, _ := config["url"].(string)
	if url == "" {
		slog.Warn("webhook notifier: no url configured")
		return nil
	}
	secret, _ := config["secret"].(string)
	return &WebhookNotifier{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: webhookRequestTimeout},
	}
}

func (n *WebhookNotifier) Type() string {
	return "webhook"
}

func (n *WebhookNotifier) Send(ctx context.Context, title, body string) error {
	payload := map[string]interface{}{
		"title":     title,
		"body":      body,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if n.secret != "" {
		sign := hmac.New(sha256.New, []byte(n.secret))
		sign.Write(data)
		req.Header.Set(webhookSignatureHeader, "sha256="+hex.EncodeToString(sign.Sum(nil)))
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}

	slog.Info("webhook sent", "url", n.url, "status", resp.StatusCode)
	return nil
}
