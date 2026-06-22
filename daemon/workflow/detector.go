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
	return nil, fmt.Errorf("GitLab detection not yet implemented")
}

func detectGitea() (*PlatformInfo, error) {
	return nil, fmt.Errorf("Gitea detection not yet implemented")
}

func detectForgejo() (*PlatformInfo, error) {
	return nil, fmt.Errorf("Forgejo detection not yet implemented")
}

func detectBitbucket() (*PlatformInfo, error) {
	return nil, fmt.Errorf("Bitbucket detection not yet implemented")
}

func detectGerrit() (*PlatformInfo, error) {
	return nil, fmt.Errorf("Gerrit detection not yet implemented")
}
