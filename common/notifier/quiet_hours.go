package notifier

import (
	"log/slog"
	"time"

	"asika/common/models"
)

// IsQuietHours checks if the current time falls within the configured quiet hours.
func IsQuietHours(cfg *models.Config) bool {
	qh := cfg.QuietHours
	if !qh.Enabled || qh.StartTime == "" || qh.EndTime == "" {
		return false
	}

	loc := time.Local
	if qh.Timezone != "" {
		if l, err := time.LoadLocation(qh.Timezone); err == nil {
			loc = l
		} else {
			slog.Warn("invalid quiet_hours timezone, using local", "timezone", qh.Timezone, "error", err)
		}
	}

	now := time.Now().In(loc)
	currentMinutes := now.Hour()*60 + now.Minute()

	startMinutes := parseTimeMinutes(qh.StartTime)
	endMinutes := parseTimeMinutes(qh.EndTime)

	if startMinutes < endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

// IsBypassEvent checks if an event type should bypass quiet hours.
func IsBypassEvent(cfg *models.Config, eventType string) bool {
	for _, b := range cfg.QuietHours.BypassForUrgent {
		if b == eventType {
			return true
		}
	}
	return false
}

// GetEscalationNotifiers filters notifiers based on escalation role during quiet hours.
// Returns true if notification should be sent (either not quiet hours, or escalation match).
func ShouldNotifyDuringQuietHours(cfg *models.Config, notifierType string, isUrgent bool) bool {
	if !IsQuietHours(cfg) {
		return true
	}
	if isUrgent || IsBypassEvent(cfg, "") {
		return true
	}
	role := cfg.QuietHours.EscalationRole
	if role == "" {
		return false
	}
	switch role {
	case "admin":
		return notifierType == "smtp" || notifierType == "telegram" || notifierType == "discord"
	case "operator":
		return notifierType == "smtp" || notifierType == "telegram" || notifierType == "discord" || notifierType == "slack" || notifierType == "feishu"
	default:
		return false
	}
}

func parseTimeMinutes(s string) int {
	if t, err := time.Parse("15:04", s); err == nil {
		return t.Hour()*60 + t.Minute()
	}
	if t, err := time.Parse("15:04:05", s); err == nil {
		return t.Hour()*60 + t.Minute()
	}
	return 0
}

// QuietHoursDefault returns the default quiet hours config.
func QuietHoursDefault() models.QuietHoursConfig {
	return models.QuietHoursConfig{
		Enabled:        false,
		StartTime:      "22:00",
		EndTime:        "08:00",
		Timezone:       "",
		EscalationRole: "admin",
		BypassForUrgent: []string{"spam_detected", "sync_failed"},
	}
}
