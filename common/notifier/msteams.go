package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	msteamsRequestTimeout = 10 * time.Second
)

// MSTeamsNotifier sends notifications via Microsoft Teams Incoming Webhook.
// Uses the legacy MessageCard format for broad compatibility.
type MSTeamsNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewMSTeamsNotifier creates a new MS Teams notifier from config.
// Supported config keys:
//   - webhook_url (required): Teams Incoming Webhook URL
func NewMSTeamsNotifier(config map[string]interface{}) *MSTeamsNotifier {
	webhookURL, _ := config["webhook_url"].(string)
	if webhookURL == "" {
		slog.Warn("msteams notifier: no webhook_url configured")
		return nil
	}

	return &MSTeamsNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: msteamsRequestTimeout,
		},
	}
}

// Type returns the type of notifier
func (n *MSTeamsNotifier) Type() string {
	return "msteams"
}

// Send sends a notification via Teams Incoming Webhook using MessageCard format.
func (n *MSTeamsNotifier) Send(ctx context.Context, title, body string) error {
	if n.client == nil {
		return fmt.Errorf("msteams: http client not initialized")
	}

	payload := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"title":      title,
		"text":       body,
		"themeColor": "0076D7",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("msteams: failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("msteams: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("msteams: failed to send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("msteams: unexpected status %d", resp.StatusCode)
	}

	slog.Info("msteams notification sent")
	return nil
}
