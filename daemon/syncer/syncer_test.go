package syncer

import (
	"encoding/json"
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func setupSyncerTest(t *testing.T) (*Syncer, *testutil.MockPlatformClient, func()) {
	t.Helper()

	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "test-group",
				Mode:          "multi",
				GitHub:        "org/repo",
				GitLab:        "group/repo",
				Gitea:         "user/repo",
				DefaultBranch: "main",
			},
			{
				Name:           "single-group",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/single-repo",
				DefaultBranch:  "main",
			},
		},
	}
	config.Store(cfg)

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	s := NewSyncer(cfg, clients)

	cleanup := func() {
		db.Close()
	}
	return s, mock, cleanup
}

func TestNewSyncer(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	if s == nil {
		t.Fatal("NewSyncer returned nil")
	}
	if s.cfg == nil {
		t.Error("cfg should not be nil")
	}
	if s.clients == nil {
		t.Error("clients should not be nil")
	}
}

func TestGetTokenForPlatform(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	s.cfg.Tokens.GitHub = "github-token-123"
	s.cfg.Tokens.GitLab = "gitlab-token-456"
	s.cfg.Tokens.Gitea = "gitea-token-789"

	tests := []struct {
		platform string
		want     string
	}{
		{"github", "github-token-123"},
		{"gitlab", "gitlab-token-456"},
		{"gitea", "gitea-token-789"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := config.GetToken(s.cfg, tt.platform)
			if got != tt.want {
				t.Errorf("GetToken(%q) = %q, want %q", tt.platform, got, tt.want)
			}
		})
	}
}

func TestGetRepoURL(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	tests := []struct {
		platform string
		repo     string
		want     string
	}{
		{"github", "org/repo", "https://github.com/org/repo.git"},
		{"gitlab", "group/repo", "https://gitlab.com/group/repo.git"},
		{"gitea", "user/repo", "https://gitea.example.com/user/repo.git"},
		{"unknown", "org/repo", ""},
		{"github", "invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.platform+"/"+tt.repo, func(t *testing.T) {
			got := s.getRepoURL(tt.platform, tt.repo)
			if got != tt.want {
				t.Errorf("getRepoURL(%q, %q) = %q, want %q", tt.platform, tt.repo, got, tt.want)
			}
		})
	}
}

func TestGetRepoURL_CustomBaseURLs(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	s.cfg.GitLabBaseURL = "https://gitlab.example.com/"
	s.cfg.GiteaBaseURL = "https://gitea.internal.org"

	tests := []struct {
		platform string
		repo     string
		want     string
	}{
		{"gitlab", "group/repo", "https://gitlab.example.com/group/repo.git"},
		{"gitea", "user/repo", "https://gitea.internal.org/user/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := s.getRepoURL(tt.platform, tt.repo)
			if got != tt.want {
				t.Errorf("getRepoURL(%q, %q) = %q, want %q", tt.platform, tt.repo, got, tt.want)
			}
		})
	}
}

func TestSyncOnMerge_SingleMode(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:        "pr-single-1",
		RepoGroup: "single-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Single mode PR",
		State:     "merged",
	}

	// Single mode should skip sync
	err := s.SyncOnMerge(nil, pr)
	if err != nil {
		t.Errorf("SyncOnMerge for single mode should not error, got: %v", err)
	}
}

func TestSyncOnMerge_GroupNotFound(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:        "pr-nogroup",
		RepoGroup: "nonexistent-group",
		Platform:  "github",
		PRNumber:  1,
		State:     "merged",
	}

	err := s.SyncOnMerge(nil, pr)
	if err == nil {
		t.Error("SyncOnMerge should return error for nonexistent group")
	}
}

func TestSyncBranchDeletion_SingleMode(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	// Should not panic for single mode
	s.SyncBranchDeletion("single-group", "github", "feature-branch")
}

func TestSyncBranchDeletion_GroupNotFound(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	// Should not panic for nonexistent group
	s.SyncBranchDeletion("nonexistent", "github", "feature-branch")
}

func TestGetOrCreateLock(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	mu1 := s.getOrCreateLock("group-a")
	mu2 := s.getOrCreateLock("group-a")
	mu3 := s.getOrCreateLock("group-b")

	if mu1 == nil || mu2 == nil || mu3 == nil {
		t.Fatal("getOrCreateLock should never return nil")
	}

	// Same group should return same lock
	if mu1 != mu2 {
		t.Error("same group should return same lock")
	}

	// Different groups should return different locks
	if mu1 == mu3 {
		t.Error("different groups should return different locks")
	}
}

func TestRecordSync(t *testing.T) {
	_, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:             "pr-record-test",
		RepoGroup:      "test-group",
		Platform:       "github",
		PRNumber:       42,
		MergeCommitSHA: "abc123def",
	}

	s := &Syncer{}
	s.recordSync(pr, "main", "gitlab", "success", "")

	// Verify sync record was stored
	var found bool
	db.ForEach(db.BucketSyncHistory, func(key, value []byte) error {
		var record models.SyncRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return err
		}
		if record.PRID == "pr-record-test" {
			found = true
			if record.Status != "success" {
				t.Errorf("expected status=success, got %q", record.Status)
			}
			if record.TargetPlatform != "gitlab" {
				t.Errorf("expected target=gitlab, got %q", record.TargetPlatform)
			}
			if record.SourcePlatform != "github" {
				t.Errorf("expected source=github, got %q", record.SourcePlatform)
			}
			if record.CommitSHA != "abc123def" {
				t.Errorf("expected commit=abc123def, got %q", record.CommitSHA)
			}
		}
		return nil
	})

	if !found {
		t.Error("sync record not found in database")
	}
}

func TestRecordSync_Failed(t *testing.T) {
	_, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:             "pr-fail-test",
		RepoGroup:      "test-group",
		Platform:       "github",
		PRNumber:       99,
		MergeCommitSHA: "deadbeef",
	}

	s := &Syncer{}
	s.recordSync(pr, "main", "gitea", "failed", "push rejected")

	var found bool
	db.ForEach(db.BucketSyncHistory, func(key, value []byte) error {
		var record models.SyncRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return err
		}
		if record.PRID == "pr-fail-test" {
			found = true
			if record.Status != "failed" {
				t.Errorf("expected status=failed, got %q", record.Status)
			}
			if record.ErrorMessage != "push rejected" {
				t.Errorf("expected error='push rejected', got %q", record.ErrorMessage)
			}
		}
		return nil
	})

	if !found {
		t.Error("failed sync record not found in database")
	}
}
