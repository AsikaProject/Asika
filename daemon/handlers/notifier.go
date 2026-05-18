package handlers

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/handlers/webhook"
)

// notifyFunc is an optional external notification sender (set by core).
var notifyFunc func(title, body string)

// notifyFuncMu protects notifyFunc from concurrent access.
var notifyFuncMu sync.RWMutex

// notifyUrgentFunc is an optional external urgent notification sender (bypasses quiet hours).
var notifyUrgentFunc func(title, body string)

// resetPrefsCacheFunc resets the notifier preferences cache (set by core).
var resetPrefsCacheFunc func()

// SetResetPrefsCacheFunc registers the cache reset callback from core.
func SetResetPrefsCacheFunc(fn func()) {
	resetPrefsCacheFunc = fn
}

func resetNotifierPrefsCache() {
	if resetPrefsCacheFunc != nil {
		resetPrefsCacheFunc()
	}
}

// SetNotifyFunc sets the external notification function.
func SetNotifyFunc(fn func(title, body string)) {
	notifyFuncMu.Lock()
	notifyFunc = fn
	notifyFuncMu.Unlock()
}

// SetNotifyUrgentFunc sets the external urgent notification function.
func SetNotifyUrgentFunc(fn func(title, body string)) {
	notifyFuncMu.Lock()
	notifyUrgentFunc = fn
	notifyFuncMu.Unlock()
}

var globalNotifiers []notifier.Notifier
var globalNotifiersMu sync.RWMutex

// InitNotifiers initializes the notification senders for handlers.
func InitNotifiers(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) {
	webhook.SetNotifyFunc(sendNotifications)
	notifiers := make([]notifier.Notifier, 0, len(cfg.Notify))
	for _, nc := range cfg.Notify {
		n := createNotifierFromNotifyConfig(nc)
		if n != nil {
			notifiers = append(notifiers, n)
		}
	}
	notifier.WirePlatformNotifiers(notifiers, clients)
	globalNotifiersMu.Lock()
	globalNotifiers = notifiers
	globalNotifiersMu.Unlock()
}

// SendNotifications sends a notification to all configured notifiers.
func SendNotifications(title, body string) {
	sendNotifications(title, body)
}

// SendNotificationSync sends a notification synchronously (used by webhook retry worker).
func SendNotificationSync(title, body string) {
	sendNotifications(title, body)
}

func sendNotifications(title, body string) {
	notifyFuncMu.RLock()
	fn := notifyFunc
	notifyFuncMu.RUnlock()
	if fn != nil {
		fn(title, body)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	globalNotifiersMu.RLock()
	notifiers := make([]notifier.Notifier, len(globalNotifiers))
	copy(notifiers, globalNotifiers)
	globalNotifiersMu.RUnlock()
	for _, n := range notifiers {
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
