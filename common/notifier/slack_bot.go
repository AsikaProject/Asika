package notifier

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"
)

// SlackBotNotifier sends notifications via Slack Bot API (chat.postMessage).
type SlackBotNotifier struct {
	client    *slack.Client
	channelID string
}

// NewSlackBotNotifier creates a new Slack bot notifier from config.
// Supported config keys:
//   - token (required): Bot User OAuth Token (xoxb-...)
//   - channel_id (optional): default channel to post to (e.g. "#general" or "C12345")
func NewSlackBotNotifier(config map[string]interface{}) *SlackBotNotifier {
	token, _ := config["token"].(string)
	if token == "" {
		slog.Warn("slack_bot notifier: no token configured")
		return nil
	}

	channelID, _ := config["channel_id"].(string)

	return &SlackBotNotifier{
		client:    slack.New(token),
		channelID: channelID,
	}
}

// Type returns the type of notifier
func (n *SlackBotNotifier) Type() string {
	return "slack_bot"
}

// Send sends a notification via Slack Bot API.
func (n *SlackBotNotifier) Send(ctx context.Context, title, body string) error {
	if n.client == nil {
		return fmt.Errorf("slack_bot: client not initialized")
	}

	text := fmt.Sprintf("*%s*\n\n%s", title, body)

	if n.channelID != "" {
		_, _, err := n.client.PostMessageContext(ctx, n.channelID, slack.MsgOptionText(text, false))
		if err != nil {
			return fmt.Errorf("slack_bot: failed to send to channel %s: %w", n.channelID, err)
		}
		slog.Info("slack_bot notification sent", "channel", n.channelID)
		return nil
	}

	return fmt.Errorf("slack_bot: no channel_id configured")
}
