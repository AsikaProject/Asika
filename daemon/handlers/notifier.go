package handlers

import (
	"context"
	"log/slog"
	"time"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
)

// notifyFunc is an optional external notification sender (set by core).
var notifyFunc func(title, body string)

// SetNotifyFunc sets the external notification function.
func SetNotifyFunc(fn func(title, body string)) {
	notifyFunc = fn
}

var globalNotifiers []notifier.Notifier

// InitNotifiers initializes the notification senders for handlers.
func InitNotifiers(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) {
	notifiers := make([]notifier.Notifier, 0, len(cfg.Notify))
	for _, nc := range cfg.Notify {
		n := createNotifierFromNotifyConfig(nc)
		if n != nil {
			notifiers = append(notifiers, n)
		}
	}
	notifier.WirePlatformNotifiers(notifiers, clients)
	globalNotifiers = notifiers
}

func sendNotifications(title, body string) {
	if notifyFunc != nil {
		notifyFunc(title, body)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, n := range globalNotifiers {
		if err := n.Send(ctx, title, body); err != nil {
			slog.Warn("notification send failed", "type", n.Type(), "error", err)
		}
	}
}

func createNotifierFromNotifyConfig(nc models.NotifyConfig) notifier.Notifier {
	switch nc.Type {
	case "smtp":
		if n := notifier.NewSMTPNotifier(nc.Config); n != nil {
			return n
		}
	case "wecom":
		if n := notifier.NewWeComNotifier(nc.Config); n != nil {
			return n
		}
	case "github_at":
		if n := notifier.NewGitHubAtNotifier(nc.Config); n != nil {
			return n
		}
	case "gitlab_at":
		if n := notifier.NewGitLabAtNotifier(nc.Config); n != nil {
			return n
		}
	case "gitea_at":
		if n := notifier.NewGiteaAtNotifier(nc.Config); n != nil {
			return n
		}
	case "telegram":
		if n := notifier.NewTelegramNotifier(nc.Config); n != nil {
			return n
		}
	case "feishu":
		if n := notifier.NewFeishuNotifier(nc.Config); n != nil {
			return n
		}
	case "discord":
		if n := notifier.NewDiscordNotifier(nc.Config); n != nil {
			return n
		}
	case "dingtalk":
		if n := notifier.NewDingTalkNotifier(nc.Config); n != nil {
			return n
		}
	case "msteams":
		if n := notifier.NewMSTeamsNotifier(nc.Config); n != nil {
			return n
		}
	case "slack_bot":
		if n := notifier.NewSlackBotNotifier(nc.Config); n != nil {
			return n
		}
	case "webhook":
		if n := notifier.NewWebhookNotifier(nc.Config); n != nil {
			return n
		}
	}
	return nil
}
