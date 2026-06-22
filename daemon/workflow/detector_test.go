package workflow

import (
	"os"
	"testing"
)

func TestDetectGitHub(t *testing.T) {
	os.Setenv("GITHUB_ACTIONS", "true")
	os.Setenv("GITHUB_REF", "refs/pull/123/merge")
	os.Setenv("GITHUB_REPOSITORY", "owner/repo")
	os.Setenv("GITHUB_TOKEN", "test-token")
	os.Setenv("GITHUB_BASE_REF", "main")
	os.Setenv("GITHUB_HEAD_REF", "feature-branch")
	defer func() {
		os.Unsetenv("GITHUB_ACTIONS")
		os.Unsetenv("GITHUB_REF")
		os.Unsetenv("GITHUB_REPOSITORY")
		os.Unsetenv("GITHUB_TOKEN")
		os.Unsetenv("GITHUB_BASE_REF")
		os.Unsetenv("GITHUB_HEAD_REF")
	}()

	info, err := DetectPlatform()
	if err != nil {
		t.Fatalf("detection failed: %v", err)
	}

	if info.Platform != "github" {
		t.Errorf("expected platform=github, got %s", info.Platform)
	}
	if info.PRNumber != 123 {
		t.Errorf("expected PRNumber=123, got %d", info.PRNumber)
	}
	if info.RepoOwner != "owner" {
		t.Errorf("expected RepoOwner=owner, got %s", info.RepoOwner)
	}
	if info.RepoName != "repo" {
		t.Errorf("expected RepoName=repo, got %s", info.RepoName)
	}
	if info.Token != "test-token" {
		t.Errorf("expected Token=test-token, got %s", info.Token)
	}
}

func TestDetectPlatformNotInCI(t *testing.T) {
	_, err := DetectPlatform()
	if err == nil {
		t.Error("expected error when not in CI environment")
	}
}
