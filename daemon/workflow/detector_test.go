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

func TestDetectGitLab(t *testing.T) {
	os.Setenv("GITLAB_CI", "true")
	os.Setenv("CI_MERGE_REQUEST_IID", "456")
	os.Setenv("CI_PROJECT_PATH", "group/project")
	os.Setenv("CI_JOB_TOKEN", "gitlab-token")
	os.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", "main")
	os.Setenv("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME", "feature")
	os.Setenv("CI_API_V4_URL", "https://gitlab.com/api/v4")
	defer func() {
		os.Unsetenv("GITLAB_CI")
		os.Unsetenv("CI_MERGE_REQUEST_IID")
		os.Unsetenv("CI_PROJECT_PATH")
		os.Unsetenv("CI_JOB_TOKEN")
		os.Unsetenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME")
		os.Unsetenv("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME")
		os.Unsetenv("CI_API_V4_URL")
	}()

	info, err := DetectPlatform()
	if err != nil {
		t.Fatalf("detection failed: %v", err)
	}

	if info.Platform != "gitlab" {
		t.Errorf("expected platform=gitlab, got %s", info.Platform)
	}
	if info.PRNumber != 456 {
		t.Errorf("expected PRNumber=456, got %d", info.PRNumber)
	}
}

func TestDetectGitea(t *testing.T) {
	os.Setenv("GITEA_ACTIONS", "true")
	os.Setenv("GITEA_REF", "refs/pull/789/head")
	os.Setenv("GITEA_REPOSITORY", "org/repo")
	os.Setenv("GITEA_TOKEN", "gitea-token")
	defer func() {
		os.Unsetenv("GITEA_ACTIONS")
		os.Unsetenv("GITEA_REF")
		os.Unsetenv("GITEA_REPOSITORY")
		os.Unsetenv("GITEA_TOKEN")
	}()

	info, err := DetectPlatform()
	if err != nil {
		t.Fatalf("detection failed: %v", err)
	}

	if info.Platform != "gitea" || info.PRNumber != 789 {
		t.Errorf("unexpected detection result: %+v", info)
	}
}

func TestDetectBitbucket(t *testing.T) {
	os.Setenv("BITBUCKET_PIPELINE_UUID", "test-uuid")
	os.Setenv("BITBUCKET_PR_ID", "999")
	os.Setenv("BITBUCKET_REPO_FULL_NAME", "workspace/repo")
	os.Setenv("ASIKA_TOKEN", "bitbucket-token")
	defer func() {
		os.Unsetenv("BITBUCKET_PIPELINE_UUID")
		os.Unsetenv("BITBUCKET_PR_ID")
		os.Unsetenv("BITBUCKET_REPO_FULL_NAME")
		os.Unsetenv("ASIKA_TOKEN")
	}()

	info, err := DetectPlatform()
	if err != nil {
		t.Fatalf("detection failed: %v", err)
	}

	if info.Platform != "bitbucket" || info.PRNumber != 999 {
		t.Errorf("unexpected detection result: %+v", info)
	}
}

func TestDetectGerrit(t *testing.T) {
	os.Setenv("GERRIT_CHANGE_NUMBER", "12345")
	os.Setenv("GERRIT_PROJECT", "test/project")
	os.Setenv("ASIKA_TOKEN", "gerrit-token")
	os.Setenv("GERRIT_HOST", "gerrit.example.com")
	defer func() {
		os.Unsetenv("GERRIT_CHANGE_NUMBER")
		os.Unsetenv("GERRIT_PROJECT")
		os.Unsetenv("ASIKA_TOKEN")
		os.Unsetenv("GERRIT_HOST")
	}()

	info, err := DetectPlatform()
	if err != nil {
		t.Fatalf("detection failed: %v", err)
	}

	if info.Platform != "gerrit" || info.PRNumber != 12345 {
		t.Errorf("unexpected detection result: %+v", info)
	}
}
