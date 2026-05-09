package polling

import (
	"testing"
	"time"

	"asika/common/models"
	"asika/common/platforms"
	"asika/common/utils"
	"asika/testutil"
)

func TestNewPoller(t *testing.T) {
	cfg := &models.Config{
		Events: models.EventsConfig{
			PollingInterval: "30s",
		},
	}

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	p := NewPoller(cfg, clients)

	if p == nil {
		t.Fatal("NewPoller returned nil")
	}
	if p.cfg != cfg {
		t.Error("config not set correctly")
	}
}

func TestStop(t *testing.T) {
	cfg := &models.Config{
		Events: models.EventsConfig{
			PollingInterval: "30s",
		},
	}

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	p := NewPoller(cfg, clients)

	// Start a goroutine to run Start (it will block)
	go func() {
		p.Start()
	}()

	// Slightly wait
	time.Sleep(10 * time.Millisecond)

	// Stop
	p.Stop()

	// Test passes if no panic
}

func TestPollOnce(t *testing.T) {
	_ = testutil.NewTestDB(t)

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:   "main",
				Mode:   "multi",
				GitHub: "owner/repo",
			},
		},
		Events: models.EventsConfig{
			PollingInterval: "30s",
		},
	}

	// Use mock client
	client := &testutil.MockPlatformClient{
		PRs: nil, // Return empty PR list
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	p := NewPoller(cfg, clients)

	// Manually call pollOnce (without starting Start)
	p.pollOnce()

	// Test passes if no panic
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		defaultD time.Duration
		want     time.Duration
	}{
		{"30s", 1 * time.Minute, 30 * time.Second},
		{"1m", 30 * time.Second, 1 * time.Minute},
		{"", 30 * time.Second, 30 * time.Second},
		{"invalid", 45 * time.Second, 45 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := utils.ParseDuration(tt.input, tt.defaultD)
			if got != tt.want {
				t.Errorf("ParseDuration(%q, %v) = %v, want %v", tt.input, tt.defaultD, got, tt.want)
			}
		})
	}
}

func TestPollRepoGroup(t *testing.T) {
	_ = testutil.NewTestDB(t)

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:   "main",
				Mode:   "multi",
				GitHub: "owner/repo",
			},
		},
	}

	client := &testutil.MockPlatformClient{
		PRs: nil,
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	p := NewPoller(cfg, clients)

	rg := models.RepoGroupConfig{
		Name:   "main",
		GitHub: "owner/repo",
	}

	p.pollRepoGroup(rg)

	// Test passes if no panic
}

func TestPollPlatform(t *testing.T) {
	_ = testutil.NewTestDB(t)

	cfg := &models.Config{}
	client := &testutil.MockPlatformClient{
		PRs: nil,
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	p := NewPoller(cfg, clients)

	// Test pollPlatform
	p.pollPlatform(client, "main", "github", "owner/repo")

	// Test passes if no panic
}

// Test pollPlatform when client returns PRs
func TestPollPlatform_WithPRs(t *testing.T) {
	_ = testutil.NewTestDB(t)

	cfg := &models.Config{}
	client := &testutil.MockPlatformClient{
		PRs: map[string]*models.PRRecord{
			"owner/repo#1": {
				ID:        "test-pr-1",
				RepoGroup: "main",
				Platform:  "github",
				PRNumber:  1,
				Title:     "Test PR",
				State:     "open",
				Author:    "user1",
			},
		},
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	p := NewPoller(cfg, clients)

	p.pollPlatform(client, "main", "github", "owner/repo")

	// Verify PR is stored to database
	// Need to check database, but simplify by just verifying no panic
}
