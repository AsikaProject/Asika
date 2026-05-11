package webhook

import (
	"testing"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func TestWebhookHealthUpdate(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	err := db.PutWebhookHealth("test-group", "github", time.Now())
	if err != nil {
		t.Fatalf("PutWebhookHealth failed: %v", err)
	}

	ts, err := db.GetWebhookHealth("test-group", "github")
	if err != nil {
		t.Fatalf("GetWebhookHealth failed: %v", err)
	}
	if ts.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}

	_, err = db.GetWebhookHealth("nonexistent", "github")
	if err != nil {
		t.Fatalf("GetWebhookHealth for nonexistent should not error: %v", err)
	}
}

func TestWebhookHealthList(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	now := time.Now()
	db.PutWebhookHealth("group1", "github", now)
	db.PutWebhookHealth("group1", "gitlab", now.Add(-time.Hour))
	db.PutWebhookHealth("group2", "github", now.Add(-2*time.Hour))

	health, err := db.ListWebhookHealth()
	if err != nil {
		t.Fatalf("ListWebhookHealth failed: %v", err)
	}
	if len(health) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(health))
	}
}

func TestWebhookHealthOverwrites(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()

	db.PutWebhookHealth("test-group", "github", oldTime)
	db.PutWebhookHealth("test-group", "github", newTime)

	ts, err := db.GetWebhookHealth("test-group", "github")
	if err != nil {
		t.Fatalf("GetWebhookHealth failed: %v", err)
	}

	diff := ts.Sub(newTime)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expected timestamp close to newTime, got %v (diff %v)", ts, diff)
	}
}

func TestCollectPlatforms(t *testing.T) {
	rg := models.RepoGroupConfig{
		GitHub: "org/repo",
		GitLab: "org/repo",
	}
	platforms := collectPlatforms(rg)
	if len(platforms) != 2 {
		t.Fatalf("expected 2 platforms, got %d", len(platforms))
	}
}

func TestCollectPlatforms_Empty(t *testing.T) {
	rg := models.RepoGroupConfig{}
	platforms := collectPlatforms(rg)
	if len(platforms) != 0 {
		t.Fatalf("expected 0 platforms, got %d", len(platforms))
	}
}

func TestParseHealthThreshold(t *testing.T) {
	cfg := &models.Config{
		Events: models.EventsConfig{
			HealthCheckThreshold: "10m",
			PollingInterval:      "5m",
		},
	}
	_ = config.Current()
	threshold := parseHealthThreshold(cfg.Events.HealthCheckThreshold, cfg.Events.PollingInterval)
	if threshold != 10*time.Minute {
		t.Errorf("expected 10m, got %v", threshold)
	}
}

func TestRoundDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
	}
	for _, tt := range tests {
		got := roundDuration(tt.input)
		if got != tt.expected {
			t.Errorf("roundDuration(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
