package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/handlers"
)

const dedupWindow = 5 * time.Minute

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
	handlers.SetResetPrefsCacheFunc(resetNotifierPrefsCache)
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
		if !isNotifierEnabledForAnyUser(nt, eventType) {
			slog.Info("notification disabled by all users", "notifier", nt, "event", eventType)
			continue
		}
		if eventType != "" && prID != "" {
			entry, buffered := appendToDedupBuffer(eventType, prID, nt, title, body)
			if buffered {
				slog.Info("notification buffered for digest", "notifier", nt, "event", eventType, "pr", prID)
				continue
			}
			if entry != nil {
				sendDigest(ctx, nt, n, entry)
				continue
			}
		}
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := n.Send(sendCtx, title, body)
		cancel()
		if err != nil {
			failureTracker.RecordFailure(nt, err)
		} else {
			failureTracker.RecordSuccess(nt)
		}
	}
}

var (
	notifierPrefsCache     []models.NotificationPreferences
	notifierPrefsCacheTime time.Time
	notifierPrefsCacheMu   sync.RWMutex
	notifierPrefsCacheTTL  = 30 * time.Second
)

func resetNotifierPrefsCache() {
	notifierPrefsCacheMu.Lock()
	notifierPrefsCache = nil
	notifierPrefsCacheTime = time.Time{}
	notifierPrefsCacheMu.Unlock()
}

func cachedNotificationPrefs() ([]models.NotificationPreferences, error) {
	notifierPrefsCacheMu.RLock()
	if time.Since(notifierPrefsCacheTime) < notifierPrefsCacheTTL && notifierPrefsCache != nil {
		prefs := notifierPrefsCache
		notifierPrefsCacheMu.RUnlock()
		return prefs, nil
	}
	notifierPrefsCacheMu.RUnlock()

	prefs, err := db.ListNotificationPrefs(nil)
	if err != nil {
		return nil, err
	}

	notifierPrefsCacheMu.Lock()
	notifierPrefsCache = prefs
	notifierPrefsCacheTime = time.Now()
	notifierPrefsCacheMu.Unlock()
	return prefs, nil
}

// ResetNotifierPrefsCache resets the notification preferences cache (for tests).
func ResetNotifierPrefsCache() {
	notifierPrefsCacheMu.Lock()
	notifierPrefsCache = nil
	notifierPrefsCacheTime = time.Time{}
	notifierPrefsCacheMu.Unlock()
}

func isNotifierEnabledForAnyUser(notifierType, eventType string) bool {
	prefs, err := cachedNotificationPrefs()
	if err != nil {
		return true
	}
	if len(prefs) == 0 {
		return true
	}
	for _, p := range prefs {
		if !p.Enabled {
			continue
		}
		if len(p.EnabledNotifiers) > 0 {
			found := false
			for _, en := range p.EnabledNotifiers {
				if en == notifierType {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if eventType != "" && p.EventPrefs != nil {
			if enabled, exists := p.EventPrefs[eventType]; exists && !enabled {
				continue
			}
		}
		return true
	}
	return false
}

type dedupEntry struct {
	PRID       string    `json:"pr_id"`
	Notifier   string    `json:"notifier"`
	Events     []string  `json:"events"`
	Titles     []string  `json:"titles"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Dispatched bool      `json:"dispatched"`
}

var (
	dedupMu     sync.Mutex
	dedupTimers = make(map[string]*time.Timer)
)

func dedupBufferKey(prID, notifierType string) string {
	return fmt.Sprintf("%s:%s", prID, notifierType)
}

func appendToDedupBuffer(eventType, prID, notifierType, title, body string) (*dedupEntry, bool) {
	key := dedupBufferKey(prID, notifierType)
	now := time.Now()

	dedupMu.Lock()
	defer dedupMu.Unlock()

	data, err := db.GetNotificationDedup(key)
	if err == nil && data != nil {
		var entry dedupEntry
		if json.Unmarshal(data, &entry) == nil && !entry.Dispatched {
			if now.Sub(entry.FirstSeen) < dedupWindow {
				entry.Events = append(entry.Events, eventType)
				entry.Titles = append(entry.Titles, title)
				entry.LastSeen = now
				db.PutNotificationDedup(key, mustMarshal(entry))
				if len(entry.Events) == 1 {
					scheduleDigestDispatch(key, entry)
				}
				return nil, true
			}
		}
	}

	entry := dedupEntry{
		PRID:      prID,
		Notifier:  notifierType,
		Events:    []string{eventType},
		Titles:    []string{title},
		FirstSeen: now,
		LastSeen:  now,
	}
	db.PutNotificationDedup(key, mustMarshal(entry))
	scheduleDigestDispatch(key, entry)
	return &entry, false
}

func scheduleDigestDispatch(key string, entry dedupEntry) {
	if t, exists := dedupTimers[key]; exists {
		t.Stop()
	}
	dedupTimers[key] = time.AfterFunc(dedupWindow, func() {
		dedupMu.Lock()
		defer dedupMu.Unlock()

		data, err := db.GetNotificationDedup(key)
		if err != nil || data == nil {
			return
		}
		var e dedupEntry
		if json.Unmarshal(data, &e) != nil || e.Dispatched {
			return
		}
		e.Dispatched = true
		db.PutNotificationDedup(key, mustMarshal(e))

		if n := findNotifier(e.Notifier); n != nil {
			sendDigest(context.Background(), e.Notifier, n, &e)
		}
		delete(dedupTimers, key)
	})
}

func sendDigest(ctx context.Context, notifierType string, n notifier.Notifier, entry *dedupEntry) {
	if len(entry.Events) <= 1 {
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		err := n.Send(sendCtx, entry.Titles[0], "")
		if err != nil {
			failureTracker.RecordFailure(notifierType, err)
		} else {
			failureTracker.RecordSuccess(notifierType)
		}
		return
	}

	summary := fmt.Sprintf("📋 PR %s: %d events\n", entry.PRID, len(entry.Events))
	eventCount := make(map[string]int)
	for _, ev := range entry.Events {
		eventCount[ev]++
	}
	for ev, count := range eventCount {
		if count > 1 {
			summary += fmt.Sprintf("  • %s ×%d\n", ev, count)
		} else {
			summary += fmt.Sprintf("  • %s\n", ev)
		}
	}

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := n.Send(sendCtx, summary, "")
	if err != nil {
		failureTracker.RecordFailure(notifierType, err)
	} else {
		failureTracker.RecordSuccess(notifierType)
	}
	recordBatchDedup(entry.PRID, notifierType, entry.Events)
}

func findNotifier(notifierType string) notifier.Notifier {
	for _, n := range globalNotifiers {
		if n.Type() == notifierType {
			return n
		}
	}
	return nil
}

func recordBatchDedup(prID, notifierType string, events []string) {
	now := time.Now()
	data, _ := json.Marshal(now)
	for _, ev := range events {
		key := fmt.Sprintf("%s:%s:%s", ev, prID, notifierType)
		db.PutNotificationDedup(key, data)
	}
}

func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("failed to marshal dedup entry, writing minimal fallback", "error", err)
		fallback := fmt.Sprintf(`{"_id":"%s"}`, uuid.New().String())
		return []byte(fallback)
	}
	return data
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
