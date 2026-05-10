package notifier

import (
	"testing"
	"time"

	"asika/common/models"
)

func TestIsQuietHours_Disabled(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{Enabled: false},
	}
	if IsQuietHours(cfg) {
		t.Fatal("expected false when disabled")
	}
}

func TestIsQuietHours_WithinWindow(t *testing.T) {
	now := time.Now()
	start := now.Add(-time.Hour).Format("15:04")
	end := now.Add(time.Hour).Format("15:04")

	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:   true,
			StartTime: start,
			EndTime:   end,
			Timezone:  "",
		},
	}
	if !IsQuietHours(cfg) {
		t.Fatal("expected true when within quiet hours")
	}
}

func TestIsQuietHours_OutsideWindow(t *testing.T) {
	now := time.Now()
	start := now.Add(2 * time.Hour).Format("15:04")
	end := now.Add(3 * time.Hour).Format("15:04")

	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:   true,
			StartTime: start,
			EndTime:   end,
			Timezone:  "",
		},
	}
	if IsQuietHours(cfg) {
		t.Fatal("expected false when outside quiet hours")
	}
}

func TestIsQuietHours_Overnight(t *testing.T) {
	now := time.Now()
	nightStart := now.Add(-2 * time.Hour).Format("15:04")
	nightEnd := now.Add(2 * time.Hour).Format("15:04")

	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:   true,
			StartTime: nightStart,
			EndTime:   nightEnd,
			Timezone:  "",
		},
	}
	if !IsQuietHours(cfg) {
		t.Fatal("expected true for overnight window")
	}
}

func TestIsQuietHours_InvalidTimezone(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:   true,
			StartTime: "00:00",
			EndTime:   "23:59",
			Timezone:  "Invalid/Timezone",
		},
	}
	result := IsQuietHours(cfg)
	_ = result
}

func TestIsBypassEvent(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:         true,
			BypassForUrgent: []string{"spam_detected", "sync_failed"},
		},
	}
	if !IsBypassEvent(cfg, "spam_detected") {
		t.Fatal("expected spam_detected to bypass")
	}
	if IsBypassEvent(cfg, "pr_opened") {
		t.Fatal("expected pr_opened to not bypass")
	}
}

func TestShouldNotifyDuringQuietHours_NotQuiet(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{Enabled: false},
	}
	if !ShouldNotifyDuringQuietHours(cfg, "telegram", false) {
		t.Fatal("expected true when not in quiet hours")
	}
}

func TestShouldNotifyDuringQuietHours_Urgent(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:         true,
			StartTime:       "00:00",
			EndTime:         "23:59",
			BypassForUrgent: []string{"spam_detected"},
		},
	}
	if !ShouldNotifyDuringQuietHours(cfg, "smtp", true) {
		t.Fatal("expected true for urgent events during quiet hours")
	}
}

func TestShouldNotifyDuringQuietHours_EscalationAdmin(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:        true,
			StartTime:      "00:00",
			EndTime:        "23:59",
			EscalationRole: "admin",
		},
	}
	if !ShouldNotifyDuringQuietHours(cfg, "telegram", false) {
		t.Fatal("expected true for telegram with admin escalation")
	}
	if !ShouldNotifyDuringQuietHours(cfg, "smtp", false) {
		t.Fatal("expected true for smtp with admin escalation")
	}
}

func TestShouldNotifyDuringQuietHours_EscalationOperator(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:        true,
			StartTime:      "00:00",
			EndTime:        "23:59",
			EscalationRole: "operator",
		},
	}
	if !ShouldNotifyDuringQuietHours(cfg, "slack", false) {
		t.Fatal("expected true for slack with operator escalation")
	}
}

func TestShouldNotifyDuringQuietHours_Suppressed(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:        true,
			StartTime:      "00:00",
			EndTime:        "23:59",
			EscalationRole: "admin",
		},
	}
	if ShouldNotifyDuringQuietHours(cfg, "slack_bot", false) {
		t.Fatal("expected false for slack_bot suppressed during quiet hours")
	}
}

func TestShouldNotifyDuringQuietHours_NoEscalation(t *testing.T) {
	cfg := &models.Config{
		QuietHours: models.QuietHoursConfig{
			Enabled:        true,
			StartTime:      "00:00",
			EndTime:        "23:59",
			EscalationRole: "",
		},
	}
	if ShouldNotifyDuringQuietHours(cfg, "telegram", false) {
		t.Fatal("expected false when no escalation role")
	}
}

func TestParseTimeMinutes(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"08:00", 480},
		{"22:30", 1350},
		{"00:00", 0},
		{"12:00", 720},
	}
	for _, tt := range tests {
		got := parseTimeMinutes(tt.input)
		if got != tt.expected {
			t.Errorf("parseTimeMinutes(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestQuietHoursDefault(t *testing.T) {
	d := QuietHoursDefault()
	if d.Enabled {
		t.Fatal("expected disabled by default")
	}
	if d.StartTime != "22:00" {
		t.Errorf("expected start 22:00, got %s", d.StartTime)
	}
	if d.EndTime != "08:00" {
		t.Errorf("expected end 08:00, got %s", d.EndTime)
	}
	if d.EscalationRole != "admin" {
		t.Errorf("expected admin escalation, got %s", d.EscalationRole)
	}
	if len(d.BypassForUrgent) != 2 {
		t.Errorf("expected 2 bypass events, got %d", len(d.BypassForUrgent))
	}
}
