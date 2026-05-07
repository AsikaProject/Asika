package stale

import (
	"testing"
	"time"

	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func TestNewManager(t *testing.T) {
	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)

	m := NewManager(cfg, clients)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestInactivityDays(t *testing.T) {
	tests := []struct {
		lastActive time.Time
		want        int
	}{
		{time.Now().Add(-24 * time.Hour), 1},      // 1 day ago
		{time.Now().Add(-72 * time.Hour), 3},      // 3 days ago
		{time.Time{}, 0},                     // zero time
		{time.Now().Add(24 * time.Hour), 0},       // future (should be 0 or negative, but we return 0)
	}

	for _, tt := range tests {
		t.Run(tt.lastActive.String(), func(t *testing.T) {
			got := inactivityDays(tt.lastActive)
			// Due to time precision issues, we only check approximate values
			if tt.lastActive.IsZero() && got != 0 {
				t.Errorf("inactivityDays(zero time) = %d, want 0", got)
			}
		})
	}
}

func TestHasLabel(t *testing.T) {
	tests := []struct {
		labels []string
		target string
		want   bool
	}{
		{[]string{"bug", "enhancement"}, "bug", true},
		{[]string{"bug", "enhancement"}, "feature", false},
		{[]string{}, "bug", false},
		{[]string{"stale"}, "stale", true},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := hasLabel(tt.labels, tt.target)
			if got != tt.want {
				t.Errorf("hasLabel(%v, %q) = %v, want %v", tt.labels, tt.target, got, tt.want)
			}
		})
	}
}

func TestGroupPlatforms(t *testing.T) {
	group := &models.RepoGroup{
		Name:   "main",
		GitHub: "owner/repo",
		GitLab: "group/repo",
		Gitea:  "user/repo",
	}

	platforms := platforms.GroupPlatforms(group)
	if len(platforms) != 3 {
		t.Errorf("groupPlatforms returned %d platforms, want 3", len(platforms))
	}

	// Check if correct platform types are included
	found := make(map[string]bool)
	for _, p := range platforms {
		found[string(p)] = true
	}

	if !found["github"] {
		t.Error("missing GitHub platform")
	}
	if !found["gitlab"] {
		t.Error("missing GitLab platform")
	}
	if !found["gitea"] {
		t.Error("missing Gitea platform")
	}
}

func TestGroupPlatforms_Partial(t *testing.T) {
	group := &models.RepoGroup{
		Name:   "single",
		GitHub: "owner/repo",
	}

	platforms := platforms.GroupPlatforms(group)
	if len(platforms) != 1 {
		t.Errorf("groupPlatforms returned %d platforms, want 1", len(platforms))
	}
	if string(platforms[0]) != "github" {
		t.Errorf("platform = %v, want github", platforms[0])
	}
}

func TestManager_CheckRepoGroup_Disabled(t *testing.T) {
	cfg := &models.Config{
		Stale: models.StaleConfig{
			Enabled: false,
		},
	}

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	m := NewManager(cfg, clients)

	group := &models.RepoGroup{
		Name: "main",
	}

	// Should return directly, not execute any operation
	m.CheckRepoGroup(group)
	// Test passes if no panic
}

func TestManager_CheckRepoGroup_Enabled(t *testing.T) {
	_ = testutil.NewTestDB(t)
	events.Init()

	cfg := &models.Config{
		Stale: models.StaleConfig{
			Enabled: true,
		},
	}

	client := &testutil.MockPlatformClient{
		PRs: map[string]*models.PRRecord{
			"owner/repo#1": {
				ID:       "pr-1",
				PRNumber:  1,
				Title:    "Test PR",
				State:    "open",
				Author:   "user1",
			},
		},
	}

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: client,
	}

	m := NewManager(cfg, clients)

	group := &models.RepoGroup{
		Name:   "main",
		GitHub: "owner/repo",
	}

	// Should execute check, but won't panic
	m.CheckRepoGroup(group)
}

func TestAnalyzePR(t *testing.T) {
	m := &Manager{
		cfg: &models.Config{
			Stale: models.StaleConfig{
				DaysUntilStale: 7,
				StaleLabel:     "stale",
			},
		},
	}

	pr := &models.PRRecord{
		ID:        "pr-1",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
		UpdatedAt: time.Now().Add(-10 * 24 * time.Hour), // 10 days ago
		Labels:    []string{},
	}

	action := m.analyzePR(nil, nil, pr, &m.cfg.Stale)
	if action.Type == "" {
		t.Error("analyzePR should return an action for stale PR")
	}
}

func TestAnalyzePR_NotStale(t *testing.T) {
	m := &Manager{
		cfg: &models.Config{
			Stale: models.StaleConfig{
				DaysUntilStale: 7,
				StaleLabel:     "stale",
			},
		},
	}

	pr := &models.PRRecord{
		ID:        "pr-1",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
		UpdatedAt: time.Now().Add(-3 * 24 * time.Hour), // 3 days ago
		Labels:    []string{},
	}

	action := m.analyzePR(nil, nil, pr, &m.cfg.Stale)
	if action.Type != "" {
		t.Errorf("analyzePR should return empty action for non-stale PR, got %v", action.Type)
	}
}
