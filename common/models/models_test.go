package models

import (
	"testing"
	"time"
)

func TestUser(t *testing.T) {
	u := User{
		Username:     "admin",
		PasswordHash: "hash",
		Role:         "admin",
		CreatedAt:    time.Now(),
	}

	if u.Username != "admin" {
		t.Errorf("Username = %q, want admin", u.Username)
	}
	if u.Role != "admin" {
		t.Errorf("Role = %q, want admin", u.Role)
	}
}

func TestRepoGroup(t *testing.T) {
	rg := RepoGroup{
		Name:           "main",
		Mode:           "multi",
		GitHub:         "org/repo",
		DefaultBranch:  "main",
		MirrorPlatform: "github",
	}

	if rg.Name != "main" {
		t.Errorf("Name = %q, want main", rg.Name)
	}
	if rg.Mode != "multi" {
		t.Errorf("Mode = %q, want multi", rg.Mode)
	}
}

func TestPRRecord(t *testing.T) {
	pr := PRRecord{
		ID:             "uuid-123",
		RepoGroup:      "main",
		Platform:       "github",
		PRNumber:       42,
		Title:          "Add feature",
		Author:         "user1",
		State:          "open",
		Labels:         []string{"enhancement"},
		MergeCommitSHA: "abc123",
		SpamFlag:       false,
		IsDraft:        false,
	}

	if pr.ID != "uuid-123" {
		t.Errorf("ID = %q, want uuid-123", pr.ID)
	}
	if pr.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", pr.PRNumber)
	}
	if pr.State != "open" {
		t.Errorf("State = %q, want open", pr.State)
	}
}

func TestPREvent(t *testing.T) {
	ev := PREvent{
		Timestamp: time.Now(),
		Action:    "opened",
		Actor:     "user1",
		Detail:    "PR opened",
	}

	if ev.Action != "opened" {
		t.Errorf("Action = %q, want opened", ev.Action)
	}
}

func TestQueueItem(t *testing.T) {
	item := QueueItem{
		PRID:      "pr-123",
		RepoGroup: "main",
		Status:    "waiting",
		AddedAt:   time.Now(),
		Criteria: MergeCriteria{
			RequiredApprovals: 2,
			ApprovedBy:        []string{"user1", "user2"},
			CIStatus:          "success",
		},
	}

	if item.Status != "waiting" {
		t.Errorf("Status = %q, want waiting", item.Status)
	}
	if item.Criteria.RequiredApprovals != 2 {
		t.Errorf("RequiredApprovals = %d, want 2", item.Criteria.RequiredApprovals)
	}
}

func TestMergeQueueConfig(t *testing.T) {
	mq := MergeQueueConfig{
		RequiredApprovals: 1,
		CICheckRequired:   true,
		CoreContributors:  []string{"admin"},
		CIProvider:        "github_actions",
	}

	if mq.RequiredApprovals != 1 {
		t.Errorf("RequiredApprovals = %d, want 1", mq.RequiredApprovals)
	}
	if !mq.CICheckRequired {
		t.Error("CICheckRequired should be true")
	}
}

func TestLabelRule(t *testing.T) {
	rule := LabelRule{
		Pattern:     "*.go",
		Label:       "go-code",
		Color:       "blue",
		Description: "Go source files",
	}

	if rule.Pattern != "*.go" {
		t.Errorf("Pattern = %q, want *.go", rule.Pattern)
	}
	if rule.Label != "go-code" {
		t.Errorf("Label = %q, want go-code", rule.Label)
	}
}

func TestSpamConfig(t *testing.T) {
	spam := SpamConfig{
		Enabled:          true,
		TimeWindow:       "5m",
		Threshold:        5,
		TriggerOnAuthor:  true,
		TriggerOnTitleKw: []string{"spam"},
	}

	if !spam.Enabled {
		t.Error("Enabled should be true")
	}
	if spam.Threshold != 5 {
		t.Errorf("Threshold = %d, want 5", spam.Threshold)
	}
}

func TestConfig(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Listen:          ":8080",
			Mode:            "release",
			EnableWebUpdate: true,
		},
		Database: DatabaseConfig{
			Path: "./test.db",
		},
		Auth: AuthConfig{
			JWTSecret:   "secret",
			TokenExpiry: "24h",
		},
	}

	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Database.Path != "./test.db" {
		t.Errorf("Database.Path = %q, want ./test.db", cfg.Database.Path)
	}
}

func TestNotifyConfig(t *testing.T) {
	notify := NotifyConfig{
		Type: "smtp",
		Config: map[string]interface{}{
			"host": "smtp.example.com",
			"port": 587,
		},
	}

	if notify.Type != "smtp" {
		t.Errorf("Type = %q, want smtp", notify.Type)
	}
}

func TestStaleConfig(t *testing.T) {
	stale := StaleConfig{
		Enabled:          true,
		CheckInterval:    "24h",
		DaysUntilStale:   30,
		DaysUntilClose:   7,
		StaleLabel:       "stale",
		ExemptLabels:     []string{"pinned"},
		NotifyOnStale:    true,
		RemoveOnActivity: true,
	}

	if !stale.Enabled {
		t.Error("Enabled should be true")
	}
	if stale.DaysUntilStale != 30 {
		t.Errorf("DaysUntilStale = %d, want 30", stale.DaysUntilStale)
	}
}

func TestSyncRecord(t *testing.T) {
	record := SyncRecord{
		ID:             "sync-123",
		PRID:           "pr-456",
		RepoGroup:      "main",
		SourcePlatform: "github",
		TargetPlatform: "gitlab",
		Status:         "success",
	}

	if record.ID != "sync-123" {
		t.Errorf("ID = %q, want sync-123", record.ID)
	}
	if record.Status != "success" {
		t.Errorf("Status = %q, want success", record.Status)
	}
}
