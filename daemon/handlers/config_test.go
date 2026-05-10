package handlers

import (
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func TestDryRun_ValidPatch(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server: models.ServerConfig{Listen: ":8080", Mode: "release"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth: models.AuthConfig{JWTSecret: "test-secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default", Mode: "multi", GitHub: "org/repo"},
		},
		Spam: models.SpamConfig{
			Enabled: true, TimeWindow: "10m", Threshold: 3,
			TriggerOnAuthor: true,
		},
		Events: models.EventsConfig{
			Mode: "webhook", PollingInterval: "30s",
			HealthCheckInterval: "2m", HealthCheckThreshold: "5m",
		},
		QuietHours: models.QuietHoursConfig{
			Enabled: false, StartTime: "22:00", EndTime: "08:00",
			EscalationRole: "admin",
		},
		MergeQueue: models.MergeQueueConfig{
			RequiredApprovals: 1, CICheckRequired: true,
		},
		WorkerPool: models.WorkerPoolConfig{
			MinWorkers: 2, MaxWorkers: 8,
		},
	}
	config.Store(cfg)

	patch := `
[merge_queue]
required_approvals = 2
`
	merged, err := config.DryRun(patch)
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}
	if merged.MergeQueue.RequiredApprovals != 2 {
		t.Errorf("expected required_approvals=2, got %d", merged.MergeQueue.RequiredApprovals)
	}
	if merged.Server.Listen != ":8080" {
		t.Errorf("expected listen=:8080, got %s", merged.Server.Listen)
	}
}

func TestDryRun_InvalidTOML(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server: models.ServerConfig{Listen: ":8080", Mode: "release"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth: models.AuthConfig{JWTSecret: "test-secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default", Mode: "multi", GitHub: "org/repo"},
		},
		Spam: models.SpamConfig{
			Enabled: true, TimeWindow: "10m", Threshold: 3,
			TriggerOnAuthor: true,
		},
		Events: models.EventsConfig{
			Mode: "webhook", PollingInterval: "30s",
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1, CICheckRequired: true},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
	}
	config.Store(cfg)

	_, err := config.DryRun("invalid {{{ toml")
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestDryRun_NoConfig(t *testing.T) {
	config.Store(nil)
	_, err := config.DryRun("[merge_queue]\nrequired_approvals = 2\n")
	if err == nil {
		t.Fatal("expected error when no config loaded")
	}
}

func TestCurrentCfgVersion(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	v := config.CurrentCfgVersion()
	if v < 0 {
		t.Errorf("expected non-negative version, got %d", v)
	}
}
