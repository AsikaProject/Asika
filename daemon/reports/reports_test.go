package reports

import (
	"strings"
	"testing"

	"asika/common/models"
)

func TestCronSchedule(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hourly", "@hourly"},
		{"daily", "@daily"},
		{"weekly", "@weekly"},
		{"monthly", "@monthly"},
		{"0 * * * *", "0 * * * *"},
		{"@every 1h", "@every 1h"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cronSchedule(tt.input)
			if got != tt.expected {
				t.Errorf("cronSchedule(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsValidCron(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"hourly", true},
		{"daily", true},
		{"weekly", true},
		{"monthly", true},
		{"0 * * * *", true},
		{"@every 1h", true},
		{"invalid garbage", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidCron(tt.input)
			if got != tt.valid {
				t.Errorf("isValidCron(%q) = %v, want %v", tt.input, got, tt.valid)
			}
		})
	}
}

func TestNewScheduler_DefaultCron(t *testing.T) {
	cfg := ScheduleConfig{Enabled: true, Cron: ""}
	s := NewScheduler(cfg)
	if s.cfg.Cron != "weekly" {
		t.Errorf("default cron = %q, want 'weekly'", s.cfg.Cron)
	}
}

func TestScheduler_StartDisabled(t *testing.T) {
	cfg := ScheduleConfig{Enabled: false, Cron: "weekly"}
	s := NewScheduler(cfg)
	s.Start()
	if s.cron != nil {
		t.Error("expected nil cron when disabled")
	}
}

func TestScheduler_StartInvalidCronFallsBack(t *testing.T) {
	cfg := ScheduleConfig{Enabled: true, Cron: "not-a-valid-schedule"}
	s := NewScheduler(cfg)
	s.Start()
	if s.cron == nil {
		t.Fatal("expected cron to be created with fallback to weekly")
	}
	s.Stop()
}

func TestScheduler_StartValidCron(t *testing.T) {
	cfg := ScheduleConfig{Enabled: true, Cron: "@every 1m"}
	s := NewScheduler(cfg)
	s.Start()
	if s.cron == nil {
		t.Fatal("expected non-nil cron for valid expression")
	}
	s.Stop()
}

func TestFormatReport_Full(t *testing.T) {
	stats := map[string]interface{}{
		"period_days":          float64(7),
		"deployment_frequency": float64(2.5),
		"lead_time_hours":      float64(48.5),
		"change_failure_rate":  float64(0.15),
		"mttr_hours":           float64(4.0),
		"total_prs":            float64(100),
		"merged_prs":           float64(80),
		"open_prs":             float64(20),
		"queue_items":          float64(5),
		"failed_queue_items":   float64(2),
		"prs_by_repo_group":    map[string]interface{}{"frontend": float64(60), "backend": float64(40)},
	}

	report := formatReport(stats)

	if !strings.Contains(report, "📊 Asika DORA Report (last 7 days)") {
		t.Errorf("missing header: %s", report)
	}
	if !strings.Contains(report, "Deployments/Day: 2.5") {
		t.Errorf("missing deployment frequency: %s", report)
	}
	if !strings.Contains(report, "Lead Time: 48.5 hours") {
		t.Errorf("missing lead time: %s", report)
	}
	if !strings.Contains(report, "Failure Rate: 15.0%") {
		t.Errorf("missing failure rate: %s", report)
	}
	if !strings.Contains(report, "MTTR: 4.0 hours") {
		t.Errorf("missing MTTR: %s", report)
	}
	if !strings.Contains(report, "Total PRs: 100") {
		t.Errorf("missing total PRs: %s", report)
	}
	if !strings.Contains(report, "Open: 20") {
		t.Errorf("missing open PRs: %s", report)
	}
	if !strings.Contains(report, "Merged: 80") {
		t.Errorf("missing merged PRs: %s", report)
	}
	if !strings.Contains(report, "Queue Items: 5") {
		t.Errorf("missing queue items: %s", report)
	}
	if !strings.Contains(report, "Failed Queue: 2") {
		t.Errorf("missing failed queue: %s", report)
	}
	if !strings.Contains(report, "frontend") {
		t.Errorf("missing repo group: %s", report)
	}
}

func TestFormatReport_Empty(t *testing.T) {
	report := formatReport(map[string]interface{}{})

	if !strings.Contains(report, "Asika DORA Report") {
		t.Errorf("missing header: %s", report)
	}
	if !strings.Contains(report, "Total PRs: 0") {
		t.Errorf("expected zero defaults: %s", report)
	}
}

func TestFormatReport_NoRepoGroups(t *testing.T) {
	stats := map[string]interface{}{
		"period_days": float64(30),
		"total_prs":   float64(50),
	}

	report := formatReport(stats)

	if strings.Contains(report, "By Repo Group") {
		t.Errorf("should not have repo group section when empty: %s", report)
	}
}

func TestFormatReport_ZeroFailureRate(t *testing.T) {
	stats := map[string]interface{}{
		"change_failure_rate": float64(0),
	}

	report := formatReport(stats)

	if !strings.Contains(report, "Failure Rate: 0.0%") {
		t.Errorf("expected 0%% failure rate: %s", report)
	}
}

func TestCreateNotifier(t *testing.T) {
	tests := []struct {
		notifierType string
		config       map[string]interface{}
		expectNil    bool
	}{
		{"smtp", map[string]interface{}{"host": "smtp.test"}, false},
		{"telegram", map[string]interface{}{"token": "test"}, false},
		{"discord", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"slack", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"slack_bot", map[string]interface{}{"token": "xoxb-test"}, false},
		{"feishu", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"webhook", map[string]interface{}{"url": "http://test"}, false},
		{"msteams", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"dingtalk", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"wecom", map[string]interface{}{"webhook_url": "http://test"}, false},
		{"unknown", map[string]interface{}{}, true},
		{"", map[string]interface{}{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.notifierType, func(t *testing.T) {
			nc := models.NotifyConfig{Type: tt.notifierType, Config: tt.config}
			n := createNotifier(nc)
			if tt.expectNil && n != nil {
				t.Errorf("expected nil for type %q, got %v", tt.notifierType, n)
			}
			if !tt.expectNil && n == nil {
				t.Errorf("expected non-nil for type %q", tt.notifierType)
			}
		})
	}
}
