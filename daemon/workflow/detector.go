package workflow

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type PlatformInfo struct {
	Platform   string
	PRNumber   int
	RepoOwner  string
	RepoName   string
	RepoFull   string
	Token      string
	BaseBranch string
	HeadBranch string
	BaseURL    string
}

func DetectPlatform() (*PlatformInfo, error) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return detectGitHub()
	}

	if os.Getenv("GITLAB_CI") == "true" {
		return detectGitLab()
	}

	if os.Getenv("GITEA_ACTIONS") == "true" {
		return detectGitea()
	}

	if os.Getenv("FORGEJO_ACTIONS") == "true" {
		return detectForgejo()
	}

	if os.Getenv("BITBUCKET_PIPELINE_UUID") != "" {
		return detectBitbucket()
	}

	if os.Getenv("GERRIT_CHANGE_NUMBER") != "" {
		return detectGerrit()
	}

	return nil, fmt.Errorf("unsupported CI platform or not running in CI environment")
}

func detectGitHub() (*PlatformInfo, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not found")
	}

	ref := os.Getenv("GITHUB_REF")
	if !strings.HasPrefix(ref, "refs/pull/") {
		return nil, fmt.Errorf("GITHUB_REF does not contain PR number: %s", ref)
	}

	parts := strings.Split(ref, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid GITHUB_REF format: %s", ref)
	}

	prNum, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse PR number from GITHUB_REF: %w", err)
	}

	repo := os.Getenv("GITHUB_REPOSITORY")
	repoParts := strings.Split(repo, "/")
	if len(repoParts) != 2 {
		return nil, fmt.Errorf("invalid GITHUB_REPOSITORY format: %s", repo)
	}

	baseURL := os.Getenv("GITHUB_API_URL")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	return &PlatformInfo{
		Platform:   "github",
		PRNumber:   prNum,
		RepoOwner:  repoParts[0],
		RepoName:   repoParts[1],
		RepoFull:   repo,
		Token:      token,
		BaseBranch: os.Getenv("GITHUB_BASE_REF"),
		HeadBranch: os.Getenv("GITHUB_HEAD_REF"),
		BaseURL:    baseURL,
	}, nil
}

func detectGitLab() (*PlatformInfo, error) {
	token := os.Getenv("CI_JOB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("CI_JOB_TOKEN not found")
	}

	prNumStr := os.Getenv("CI_MERGE_REQUEST_IID")
	if prNumStr == "" {
		return nil, fmt.Errorf("CI_MERGE_REQUEST_IID not found")
	}

	prNum, err := strconv.Atoi(prNumStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CI_MERGE_REQUEST_IID: %w", err)
	}

	projectPath := os.Getenv("CI_PROJECT_PATH")
	if projectPath == "" {
		return nil, fmt.Errorf("CI_PROJECT_PATH not found")
	}

	baseURL := os.Getenv("CI_API_V4_URL")
	if baseURL == "" {
		baseURL = "https://gitlab.com/api/v4"
	}

	parts := strings.Split(projectPath, "/")
	owner := ""
	name := projectPath
	if len(parts) >= 2 {
		owner = parts[0]
		name = parts[len(parts)-1]
	}

	return &PlatformInfo{
		Platform:   "gitlab",
		PRNumber:   prNum,
		RepoOwner:  owner,
		RepoName:   name,
		RepoFull:   projectPath,
		Token:      token,
		BaseBranch: os.Getenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME"),
		HeadBranch: os.Getenv("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME"),
		BaseURL:    baseURL,
	}, nil
}

func detectGitea() (*PlatformInfo, error) {
	token := os.Getenv("GITEA_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITEA_TOKEN not found")
	}

	ref := os.Getenv("GITEA_REF")
	if !strings.HasPrefix(ref, "refs/pull/") {
		return nil, fmt.Errorf("GITEA_REF does not contain PR number: %s", ref)
	}

	parts := strings.Split(ref, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid GITEA_REF format: %s", ref)
	}

	prNum, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse PR number from GITEA_REF: %w", err)
	}

	repo := os.Getenv("GITEA_REPOSITORY")
	repoParts := strings.Split(repo, "/")
	owner, name := "", repo
	if len(repoParts) == 2 {
		owner, name = repoParts[0], repoParts[1]
	}

	baseURL := os.Getenv("GITEA_API_URL")
	if baseURL == "" {
		baseURL = "https://gitea.com/api/v1"
	}

	return &PlatformInfo{
		Platform:   "gitea",
		PRNumber:   prNum,
		RepoOwner:  owner,
		RepoName:   name,
		RepoFull:   repo,
		Token:      token,
		BaseBranch: os.Getenv("GITEA_BASE_REF"),
		HeadBranch: os.Getenv("GITEA_HEAD_REF"),
		BaseURL:    baseURL,
	}, nil
}

func detectForgejo() (*PlatformInfo, error) {
	token := os.Getenv("FORGEJO_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("FORGEJO_TOKEN not found")
	}

	ref := os.Getenv("FORGEJO_REF")
	if !strings.HasPrefix(ref, "refs/pull/") {
		return nil, fmt.Errorf("FORGEJO_REF does not contain PR number: %s", ref)
	}

	parts := strings.Split(ref, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid FORGEJO_REF format: %s", ref)
	}

	prNum, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse PR number from FORGEJO_REF: %w", err)
	}

	repo := os.Getenv("FORGEJO_REPOSITORY")
	repoParts := strings.Split(repo, "/")
	owner, name := "", repo
	if len(repoParts) == 2 {
		owner, name = repoParts[0], repoParts[1]
	}

	baseURL := os.Getenv("FORGEJO_API_URL")
	if baseURL == "" {
		baseURL = "https://codeberg.org/api/v1"
	}

	return &PlatformInfo{
		Platform:   "forgejo",
		PRNumber:   prNum,
		RepoOwner:  owner,
		RepoName:   name,
		RepoFull:   repo,
		Token:      token,
		BaseBranch: os.Getenv("FORGEJO_BASE_REF"),
		HeadBranch: os.Getenv("FORGEJO_HEAD_REF"),
		BaseURL:    baseURL,
	}, nil
}

func detectBitbucket() (*PlatformInfo, error) {
	token := os.Getenv("ASIKA_TOKEN")
	if token == "" {
		token = os.Getenv("BITBUCKET_ACCESS_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("ASIKA_TOKEN or BITBUCKET_ACCESS_TOKEN not found (must be set in workflow config)")
	}

	prNumStr := os.Getenv("BITBUCKET_PR_ID")
	if prNumStr == "" {
		return nil, fmt.Errorf("BITBUCKET_PR_ID not found")
	}

	prNum, err := strconv.Atoi(prNumStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse BITBUCKET_PR_ID: %w", err)
	}

	repo := os.Getenv("BITBUCKET_REPO_FULL_NAME")
	if repo == "" {
		return nil, fmt.Errorf("BITBUCKET_REPO_FULL_NAME not found")
	}

	repoParts := strings.Split(repo, "/")
	owner, name := "", repo
	if len(repoParts) == 2 {
		owner, name = repoParts[0], repoParts[1]
	}

	return &PlatformInfo{
		Platform:   "bitbucket",
		PRNumber:   prNum,
		RepoOwner:  owner,
		RepoName:   name,
		RepoFull:   repo,
		Token:      token,
		BaseBranch: os.Getenv("BITBUCKET_PR_DESTINATION_BRANCH"),
		HeadBranch: os.Getenv("BITBUCKET_BRANCH"),
		BaseURL:    "https://api.bitbucket.org/2.0",
	}, nil
}

func detectGerrit() (*PlatformInfo, error) {
	token := os.Getenv("ASIKA_TOKEN")
	if token == "" {
		token = os.Getenv("GERRIT_HTTP_PASSWORD")
	}
	if token == "" {
		return nil, fmt.Errorf("ASIKA_TOKEN or GERRIT_HTTP_PASSWORD not found (must be set in workflow config)")
	}

	changeNumStr := os.Getenv("GERRIT_CHANGE_NUMBER")
	if changeNumStr == "" {
		return nil, fmt.Errorf("GERRIT_CHANGE_NUMBER not found")
	}

	changeNum, err := strconv.Atoi(changeNumStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GERRIT_CHANGE_NUMBER: %w", err)
	}

	project := os.Getenv("GERRIT_PROJECT")
	if project == "" {
		return nil, fmt.Errorf("GERRIT_PROJECT not found")
	}

	host := os.Getenv("GERRIT_HOST")
	if host == "" {
		host = "gerrit.example.com"
	}

	return &PlatformInfo{
		Platform:  "gerrit",
		PRNumber:  changeNum,
		RepoOwner: "",
		RepoName:  project,
		RepoFull:  project,
		Token:     token,
		BaseURL:   fmt.Sprintf("https://%s", host),
	}, nil
}
