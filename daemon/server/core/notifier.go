package core

import (
	"context"
	"log/slog"
	"time"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/handlers"
)

var (
	globalNotifiers  []notifier.Notifier
	failureTracker   *notifier.FailureTracker
)

// InitNotifiers creates and wires all configured notifiers with platform clients.
func InitNotifiers(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) {
	notifiers := make([]notifier.Notifier, 0, len(cfg.Notify))
	for _, nc := range cfg.Notify {
		n := createNotifierFromConfig(nc)
		if n != nil {
			notifiers = append(notifiers, n)
		}
	}
	notifier.WirePlatformNotifiers(notifiers, clients)
	globalNotifiers = notifiers

	failureTracker = notifier.NewFailureTracker(func(notifierType string, failures int, lastErr string) {
		title, body := notifier.AlertMessage(notifierType, failures, lastErr)
		sendAlert(title, body, notifierType)
	})

	handlers.SetNotifyFunc(SendNotificationSync)
	slog.Info("notifiers initialized", "count", len(globalNotifiers))
}

// SendNotification sends a notification through all configured notifiers.
func SendNotification(ctx context.Context, title, body string) {
	for _, n := range globalNotifiers {
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := n.Send(sendCtx, title, body)
		cancel()
		if err != nil {
			failureTracker.RecordFailure(n.Type(), err)
		} else {
			failureTracker.RecordSuccess(n.Type())
		}
	}
}

// SendNotificationSync sends notifications synchronously with a timeout.
func SendNotificationSync(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	SendNotification(ctx, title, body)
}

// sendAlert sends a fault alert through all notifiers except the failed one.
func sendAlert(title, body string, excludeType string) {
	for _, n := range globalNotifiers {
		if n.Type() == excludeType {
			continue
		}
		sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := n.Send(sendCtx, title, body); err != nil {
			slog.Warn("alert notification send failed", "type", n.Type(), "error", err)
		}
		cancel()
	}
}

// GetNotifierFailureStatus returns the current failure status of all notifiers.
func GetNotifierFailureStatus() map[string]notifier.FailureStatus {
	if failureTracker == nil {
		return nil
	}
	return failureTracker.Status()
}

func createNotifierFromConfig(nc models.NotifyConfig) notifier.Notifier {
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
	case "slack":
		if n := notifier.NewSlackNotifier(nc.Config); n != nil {
			return n
		}
	case "webhook":
		if n := notifier.NewWebhookNotifier(nc.Config); n != nil {
			return n
		}
	}
	return nil
}
