package platforms

import (
	"context"
	"testing"

	"asika/common/config"
	"asika/common/models"
	"asika/testutil"
)

func TestCheckMergeMethods_NilConfig(t *testing.T) {
	err := CheckMergeMethods(nil, nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestCheckMergeMethods_NoRepos(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{},
	}
	err := CheckMergeMethods(cfg, nil)
	if err != nil {
		t.Errorf("expected no error for empty repo groups, got %v", err)
	}
}

func TestCheckMergeMethods_MissingClient(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:   "test-repo",
				Mode:   "multi",
				GitHub: "owner/repo",
			},
		},
	}

	mock := testutil.NewMockPlatformClient()
	clients := map[PlatformType]PlatformClient{
		PlatformGitHub: mock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCheckMergeMethods_SingleMergeMethod(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "test-repo",
				Mode:          "multi",
				GitHub:        "owner/repo",
				DefaultBranch: "main",
			},
		},
	}

	mock := testutil.NewMockPlatformClient()
	mock.MergeMethods = []string{"merge"}
	mock.DefaultMethod = "merge"
	clients := map[PlatformType]PlatformClient{
		PlatformGitHub: mock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Errorf("expected no error for single merge method, got %v", err)
	}
}

func TestCheckMergeMethods_MultipleWithDefault(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "test-repo",
				Mode:          "multi",
				GitHub:        "owner/repo",
				DefaultBranch: "main",
			},
		},
	}

	mock := testutil.NewMockPlatformClient()
	mock.MergeMethods = []string{"merge", "squash", "rebase"}
	mock.DefaultMethod = "squash"
	clients := map[PlatformType]PlatformClient{
		PlatformGitHub: mock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Errorf("expected no error when default is known, got %v", err)
	}
}

func TestCheckMergeMethods_MultipleWithoutDefault(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "test-repo",
				Mode:          "multi",
				GitHub:        "owner/repo",
				DefaultBranch: "main",
			},
		},
	}

	mock := testutil.NewMockPlatformClient()
	mock.MergeMethods = []string{"merge", "squash", "rebase"}
	mock.DefaultMethod = ""
	clients := map[PlatformType]PlatformClient{
		PlatformGitHub: mock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Logf("Correctly detected fatal: %v", err)
	} else {
		t.Error("expected error for multiple merge methods without default")
	}
}

func TestCheckPlatformMergeMethod_InvalidRepoFormat(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "test-repo",
				Mode:          "multi",
				GitHub:        "invalid-format-no-slash",
				DefaultBranch: "main",
			},
		},
	}

	mock := testutil.NewMockPlatformClient()
	clients := map[PlatformType]PlatformClient{
		PlatformGitHub: mock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Logf("Correctly detected invalid format: %v", err)
	} else {
		t.Error("expected error for invalid repo format")
	}
}

func TestCheckMergeMethods_MultiplePlatforms(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "multi-platform-repo",
				Mode:          "multi",
				GitHub:        "owner/github-repo",
				GitLab:        "group/gitlab-repo",
				Gitea:         "user/gitea-repo",
				DefaultBranch: "main",
			},
		},
	}

	ghMock := testutil.NewMockPlatformClient()
	ghMock.MergeMethods = []string{"merge"}
	ghMock.DefaultMethod = "merge"

	glMock := testutil.NewMockPlatformClient()
	glMock.MergeMethods = []string{"merge"}
	glMock.DefaultMethod = "merge"

	gtMock := testutil.NewMockPlatformClient()
	gtMock.MergeMethods = []string{"merge", "squash"}
	gtMock.DefaultMethod = "squash"

	clients := map[PlatformType]PlatformClient{
		PlatformGitHub: ghMock,
		PlatformGitLab: glMock,
		PlatformGitea:  gtMock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Errorf("expected no error when all platforms resolve, got %v", err)
	}
}

func TestExitOnCheckFailed(t *testing.T) {
	t.Run("nil error should not exit", func(t *testing.T) {
		// Just verify it doesn't panic
		ExitOnCheckFailed(nil)
	})
}

func TestRepoGroupsNotConfigured(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{},
	}

	repos := config.GetRepoGroups(cfg)
	if len(repos) != 0 {
		t.Errorf("expected 0 repo groups, got %d", len(repos))
	}
}

func TestGetDefaultMergeMethod(t *testing.T) {
	mock := testutil.NewMockPlatformClient()
	mock.MergeMethods = []string{"merge", "squash", "rebase"}
	mock.DefaultMethod = "rebase"

	ctx := context.Background()
	method, err := mock.GetDefaultMergeMethod(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "rebase" {
		t.Errorf("GetDefaultMergeMethod() = %q, want %q", method, "rebase")
	}
}

func TestHasMultipleMergeMethods(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		want    bool
	}{
		{"single method", []string{"merge"}, false},
		{"multiple methods", []string{"merge", "squash"}, true},
		{"three methods", []string{"merge", "squash", "rebase"}, true},
		{"no methods", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockPlatformClient()
			mock.MergeMethods = tt.methods

			ctx := context.Background()
			has, err := mock.HasMultipleMergeMethods(ctx, "owner", "repo")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if has != tt.want {
				t.Errorf("HasMultipleMergeMethods() = %v, want %v", has, tt.want)
			}
		})
	}
}

func TestCheckMergeMethods_RepoWithGitLabOnly(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "gitlab-only",
				Mode:           "single",
				MirrorPlatform: "gitlab",
				GitLab:         "group/project",
				DefaultBranch:  "main",
			},
		},
	}

	glMock := testutil.NewMockPlatformClient()
	glMock.MergeMethods = []string{"merge"}
	clients := map[PlatformType]PlatformClient{
		PlatformGitLab: glMock,
	}

	err := CheckMergeMethods(cfg, clients)
	if err != nil {
		t.Errorf("expected no error for single gitlab repo, got %v", err)
	}
}
