package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
)

func getDigestModeUsers(notifierType, eventType string) []string {
	prefs, err := cachedNotificationPrefs()
	if err != nil || len(prefs) == 0 {
		return nil
	}
	var users []string
	for _, p := range prefs {
		if !p.Enabled {
			continue
		}
		if p.DigestMode == "" || p.DigestMode == "realtime" {
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
		users = append(users, p.Username)
	}
	return users
}

func FlushDigests(username, mode string) {
	digests, err := db.ListNotificationDigests()
	if err != nil {
		slog.Error("failed to list digests", "error", err)
		return
	}
	entries, ok := digests[username]
	if !ok || len(entries) == 0 {
		return
	}
	prefsData, err := db.GetNotificationPrefs(username)
	if err != nil {
		slog.Warn("failed to get prefs for digest flush", "username", username, "error", err)
		return
	}
	var prefs models.NotificationPreferences
	if err := json.Unmarshal(prefsData, &prefs); err != nil {
		slog.Warn("failed to unmarshal prefs for digest flush", "username", username, "error", err)
		return
	}
	notifiers := getNotifiersForUser(&prefs)
	if len(notifiers) == 0 {
		return
	}
	summary := fmt.Sprintf("📋 Notification Digest (%d items)\n\n", len(entries))
	for _, entry := range entries {
		summary += fmt.Sprintf("• %s\n", entry.Title)
	}
	for _, n := range notifiers {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := n.Send(ctx, summary, ""); err != nil {
			slog.Warn("digest notification send failed", "notifier", n.Type(), "username", username, "error", err)
		}
		cancel()
	}
	if err := db.DeleteNotificationDigests(username); err != nil {
		slog.Error("failed to delete flushed digests", "username", username, "error", err)
	}
	slog.Info("digests flushed", "username", username, "count", len(entries))
}

func getNotifiersForUser(prefs *models.NotificationPreferences) []notifier.Notifier {
	globalNotifiersMu.RLock()
	defer globalNotifiersMu.RUnlock()
	if len(prefs.EnabledNotifiers) == 0 {
		result := make([]notifier.Notifier, len(globalNotifiers))
		copy(result, globalNotifiers)
		return result
	}
	var result []notifier.Notifier
	for _, n := range globalNotifiers {
		for _, en := range prefs.EnabledNotifiers {
			if n.Type() == en {
				result = append(result, n)
				break
			}
		}
	}
	return result
}

func StartDigestWorker() {
	go func() {
		hourlyTicker := time.NewTicker(1 * time.Hour)
		dailyTicker := time.NewTicker(24 * time.Hour)
		defer hourlyTicker.Stop()
		defer dailyTicker.Stop()
		for {
			select {
			case <-hourlyTicker.C:
				flushDigestsByMode("hourly")
			case <-dailyTicker.C:
				flushDigestsByMode("daily")
			}
		}
	}()
	slog.Info("digest worker started")
}

func flushDigestsByMode(mode string) {
	prefs, err := db.ListNotificationPrefs(nil)
	if err != nil {
		slog.Error("failed to list prefs for digest flush", "error", err)
		return
	}
	for _, p := range prefs {
		if p.DigestMode == mode {
			FlushDigests(p.Username, mode)
		}
	}
}
