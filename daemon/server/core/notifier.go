package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/handlers"
)

var (
	globalNotifiers []notifier.Notifier
	failureTracker  *notifier.FailureTracker
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
	sendNotificationInternal(ctx, title, body, "", "", "")
}

// SendNotificationWithContext sends a notification with event context for dedup and preferences.
func SendNotificationWithContext(ctx context.Context, title, body, eventType, prID, notifierType string) {
	sendNotificationInternal(ctx, title, body, eventType, prID, notifierType)
}

func sendNotificationInternal(ctx context.Context, title, body, eventType, prID, notifierType string) {
	cfg := config.Current()
	quiet := cfg != nil && notifier.IsQuietHours(cfg)

	for _, n := range globalNotifiers {
		if quiet && !notifier.ShouldNotifyDuringQuietHours(cfg, n.Type(), false) {
			slog.Info("notification suppressed by quiet hours", "notifier", n.Type())
			continue
		}
		nt := n.Type()
		if eventType != "" && prID != "" && isDeduped(eventType, prID, nt) {
			slog.Info("notification deduplicated", "notifier", nt, "event", eventType, "pr", prID)
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := n.Send(sendCtx, title, body)
		cancel()
		if err != nil {
			failureTracker.RecordFailure(nt, err)
		} else {
			failureTracker.RecordSuccess(nt)
			if eventType != "" && prID != "" {
				recordDedup(eventType, prID, nt)
			}
		}
	}
}

func isDeduped(eventType, prID, notifierType string) bool {
	key := fmt.Sprintf("%s:%s:%s", eventType, prID, notifierType)
	data, err := db.GetNotificationDedup(key)
	if err != nil || data == nil {
		return false
	}
	var ts time.Time
	if err := json.Unmarshal(data, &ts); err != nil {
		return false
	}
	if time.Since(ts) > 5*time.Minute {
		db.DeleteNotificationDedup(key)
		return false
	}
	return true
}

func recordDedup(eventType, prID, notifierType string) {
	key := fmt.Sprintf("%s:%s:%s", eventType, prID, notifierType)
	ts := time.Now()
	data, _ := json.Marshal(ts)
	db.PutNotificationDedup(key, data)
}

// SendNotificationUrgent sends a notification bypassing quiet hours.
func SendNotificationUrgent(ctx context.Context, title, body string) {
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

// SendNotificationUrgentSync sends urgent notifications bypassing quiet hours.
func SendNotificationUrgentSync(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	SendNotificationUrgent(ctx, title, body)
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
