package syncer

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func setupSyncerTest(t *testing.T) (*Syncer, *testutil.MockPlatformClient, func()) {
	t.Helper()

	testutil.NewTestDB(t)

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

func TestSetNotifyFunc(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	var notified bool
	s.SetNotifyFunc(func(title, body string) {
		notified = true
	})

	pr := &models.PRRecord{
		ID:        "notify-test",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
	}
	s.notifySyncFailure(pr, "gitlab", "test failure")
	if !notified {
		t.Error("expected notify function to be called")
	}
}

func TestSetNotifyFunc_Nil(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	s.SetNotifyFunc(nil)

	pr := &models.PRRecord{
		ID:        "notify-nil-test",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
	}
	s.notifySyncFailure(pr, "gitlab", "test failure")
}

func TestGetTargetPlatforms_AllPlatforms(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	group := &models.RepoGroup{
		Name:    "all-platforms",
		Mode:    "multi",
		GitHub:  "org/repo-gh",
		GitLab:  "group/repo-gl",
		Gitea:   "user/repo-gt",
		Forgejo: "org/repo-fj",
		Codeberg: "org/repo-cb",
		Bitbucket: "org/repo-bb",
		Gerrit:  "project",
	}

	targets := s.getTargetPlatforms(group, "github")
	if len(targets) != 6 {
		t.Errorf("expected 6 targets (excluding github), got %d", len(targets))
	}

	names := make(map[string]bool)
	for _, t := range targets {
		names[t.name] = true
	}

	expected := []string{"gitlab", "gitea", "forgejo", "codeberg", "bitbucket", "gerrit"}
	for _, e := range expected {
		if !names[e] {
			t.Errorf("expected target %q not found in results", e)
		}
	}

	if names["github"] {
		t.Error("source platform should not be in targets")
	}
}

func TestGetTargetPlatforms_ExcludesSource(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	group := &models.RepoGroup{
		Name:   "test",
		Mode:   "multi",
		GitHub: "org/repo-gh",
		GitLab: "group/repo-gl",
	}

	targets := s.getTargetPlatforms(group, "gitlab")
	if len(targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(targets))
	}
	if len(targets) > 0 && targets[0].name != "github" {
		t.Errorf("expected github target, got %s", targets[0].name)
	}
}

func TestGetTargetPlatforms_SkipsEmptyRepos(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	group := &models.RepoGroup{
		Name:   "test",
		Mode:   "multi",
		GitHub: "org/repo-gh",
	}

	targets := s.getTargetPlatforms(group, "github")
	if len(targets) != 0 {
		t.Errorf("expected 0 targets (all others empty), got %d", len(targets))
	}
}

func TestIsConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"conflict in message", fmt.Errorf("merge conflict in file.go"), true},
		{"cherry-pick conflict", fmt.Errorf("cherry-pick conflict"), true},
		{"non-fast-forward", fmt.Errorf("non-fast-forward update"), true},
		{"rejected", fmt.Errorf("push rejected"), true},
		{"failed to push", fmt.Errorf("failed to push some refs"), true},
		{"unrelated error", fmt.Errorf("connection refused"), false},
		{"not found", fmt.Errorf("404 not found"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConflictError(tt.err)
			if got != tt.want {
				t.Errorf("isConflictError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"timeout", fmt.Errorf("i/o timeout"), true},
		{"connection refused", fmt.Errorf("connection refused"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"rate limit", fmt.Errorf("API rate limit exceeded"), true},
		{"temporary failure", fmt.Errorf("temporary failure in name resolution"), true},
		{"eof", fmt.Errorf("unexpected EOF"), true},
		{"no such host", fmt.Errorf("dial tcp: lookup foo: no such host"), true},
		{"unrelated error", fmt.Errorf("permission denied"), false},
		{"not found", fmt.Errorf("404 not found"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientError(tt.err)
			if got != tt.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSyncOnMerge_AllPlatformsInTargetList(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "multi-all",
				Mode:          "multi",
				GitHub:        "org/repo-gh",
				GitLab:        "group/repo-gl",
				Gitea:         "user/repo-gt",
				Forgejo:       "org/repo-fj",
				Codeberg:      "org/repo-cb",
				Bitbucket:     "org/repo-bb",
				Gerrit:        "project",
				DefaultBranch: "main",
			},
		},
	}
	config.Store(cfg)

	s := NewSyncer(cfg, nil)
	group := config.GetRepoGroupByName(cfg, "multi-all")

	targets := s.getTargetPlatforms(group, "github")
	if len(targets) != 6 {
		t.Errorf("expected 6 targets, got %d", len(targets))
	}

	found := make(map[string]bool)
	for _, tgt := range targets {
		found[tgt.name] = true
	}
	for _, name := range []string{"gitlab", "gitea", "forgejo", "codeberg", "bitbucket", "gerrit"} {
		if !found[name] {
			t.Errorf("missing target platform: %s", name)
		}
	}
}

func TestNotifySyncFailure_MessageFormat(t *testing.T) {
	s, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	var gotTitle, gotBody string
	s.SetNotifyFunc(func(title, body string) {
		gotTitle = title
		gotBody = body
	})

	pr := &models.PRRecord{
		ID:        "format-test",
		RepoGroup: "mygroup",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Fix critical bug",
	}
	s.notifySyncFailure(pr, "gitlab,gitea", "push conflict")

	if gotTitle != "⚠️ Sync Failed: mygroup#42" {
		t.Errorf("unexpected title: %s", gotTitle)
	}

	expectedBodyParts := []string{"Fix critical bug", "github", "gitlab,gitea", "push conflict"}
	for _, part := range expectedBodyParts {
		if !strings.Contains(gotBody, part) {
			t.Errorf("body missing %q: %s", part, gotBody)
		}
	}
}


