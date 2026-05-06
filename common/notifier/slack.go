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

// SlackNotifier sends notifications via Slack Incoming Webhook
type SlackNotifier struct {
	webhookURL string
	channel    string
	username   string
	iconEmoji  string
	client     *http.Client
}

// NewSlackNotifier creates a new Slack notifier from config.
// Supported config keys:
//   - webhook_url (required): Slack incoming webhook URL
//   - channel (optional): override the default channel (e.g. "#deployments")
//   - username (optional): bot display name (default: "Asika")
//   - icon_emoji (optional): bot icon emoji (default: ":robot_face:")
func NewSlackNotifier(config map[string]interface{}) *SlackNotifier {
	webhookURL, _ := config["webhook_url"].(string)
	if webhookURL == "" {
		slog.Warn("slack notifier: no webhook_url configured")
		return nil
	}

	channel, _ := config["channel"].(string)
	username, _ := config["username"].(string)
	if username == "" {
		username = "Asika"
	}
	iconEmoji, _ := config["icon_emoji"].(string)
	if iconEmoji == "" {
		iconEmoji = ":robot_face:"
	}

	return &SlackNotifier{
		webhookURL: webhookURL,
		channel:    channel,
		username:   username,
		iconEmoji:  iconEmoji,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Type returns the type of notifier
func (n *SlackNotifier) Type() string {
	return "slack"
}

// Send sends a notification via Slack Incoming Webhook.
// The message is formatted as a simple text payload: "*Title*\n\nBody"
func (n *SlackNotifier) Send(ctx context.Context, title, body string) error {
	if n.client == nil {
		return fmt.Errorf("slack: http client not initialized")
	}

	payload := map[string]interface{}{
		"text":       fmt.Sprintf("*%s*\n\n%s", title, body),
		"username":   n.username,
		"icon_emoji": n.iconEmoji,
	}
	if n.channel != "" {
		payload["channel"] = n.channel
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("slack: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: failed to send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: unexpected status %d", resp.StatusCode)
	}

	slog.Info("slack notification sent", "channel", n.channel)
	return nil
}
