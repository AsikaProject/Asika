package models

// NotificationPreferences defines per-user notification settings.
type NotificationPreferences struct {
	Username           string             `json:"username"`
	Enabled            bool               `json:"enabled"`                           // master switch
	EnabledNotifiers   []string           `json:"enabled_notifiers"`                 // which notifier types to use
	EventPrefs         map[string]bool    `json:"event_prefs"`                       // event_type -> enabled
	DigestMode         string             `json:"digest_mode"`                       // "realtime" | "hourly" | "daily"
	QuietHoursOverride *QuietHoursConfig  `json:"quiet_hours_override,omitempty"`    // per-user quiet hours
}

// QuietHoursConfig defines notification quiet hours and escalation rules.
type QuietHoursConfig struct {
	Enabled         bool     `toml:"enabled" json:"enabled"`
	StartTime       string   `toml:"start_time" json:"start_time"`       // e.g. "22:00"
	EndTime         string   `toml:"end_time" json:"end_time"`           // e.g. "08:00"
	Timezone        string   `toml:"timezone" json:"timezone"`           // e.g. "Asia/Shanghai"; empty = local
	EscalationRole  string   `toml:"escalation_role" json:"escalation_role"` // role to notify during quiet hours: "admin"|"operator"
	BypassForUrgent []string `toml:"bypass_for_urgent" json:"bypass_for_urgent"` // event types that bypass quiet hours
}

// CloseReasonsConfig holds the predefined close reasons.
// Each reason automatically maps to a label with the same name.
type CloseReasonsConfig struct {
	Reasons []string `json:"reasons" toml:"reasons"`
}
