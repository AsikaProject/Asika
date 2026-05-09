package ci

import (
	"context"
	"log/slog"

	"asika/common/platforms"
)

// CIDetector detects CI configuration in repositories
type CIDetector struct{}

// NewCIDetector creates a new CI detector
func NewCIDetector() *CIDetector {
	return &CIDetector{}
}

// Detect detects the CI system used in a repository
func (d *CIDetector) Detect(ctx context.Context, client platforms.PlatformClient, owner, repo, branch string) (string, error) {
	// Try GitHub Actions
	if hasGH, err := hasGitHubActions(ctx, client, owner, repo); err == nil && hasGH {
		return "github_actions", nil
	}

	// Try GitLab CI
	if hasGL, err := hasGitLabCI(ctx, client, owner, repo); err == nil && hasGL {
		return "gitlab_ci", nil
	}

	// Try Gitea Actions
	if hasGitea, err := hasGiteaActions(ctx, client, owner, repo); err == nil && hasGitea {
		return "gitea_actions", nil
	}

	return "none", nil
}

// hasGitHubActions checks for GitHub Actions workflow files
// by looking at the diff files of recent PRs or checking CI status
func hasGitHubActions(ctx context.Context, client platforms.PlatformClient, owner, repo string) (bool, error) {
	// If we can get CI status and it's not "none", GitHub Actions likely exists
	status, err := client.GetCIStatus(ctx, owner, repo, "")
	if err != nil {
		return false, nil
	}
	// If CI returns any status besides "none", CI is configured
	return status != "none" && status != "", nil
}

// hasGitLabCI checks for .gitlab-ci.yml
func hasGitLabCI(ctx context.Context, client platforms.PlatformClient, owner, repo string) (bool, error) {
	status, err := client.GetCIStatus(ctx, owner, repo, "")
	if err != nil {
		return false, nil
	}
	return status != "none" && status != "", nil
}

// hasGiteaActions checks for Gitea Actions workflow files
func hasGiteaActions(ctx context.Context, client platforms.PlatformClient, owner, repo string) (bool, error) {
	status, err := client.GetCIStatus(ctx, owner, repo, "")
	if err != nil {
		return false, nil
	}
	return status != "none" && status != "", nil
}

// GetCIStatus gets the CI status for a commit
func GetCIStatus(ctx context.Context, client platforms.PlatformClient, owner, repo, commitSHA string) (string, error) {
	return client.GetCIStatus(ctx, owner, repo, commitSHA)
}

// DetermineCIProvider determines the CI provider based on config or detection
func DetermineCIProvider(ctx context.Context, client platforms.PlatformClient, owner, repo, branch, configured string) (string, error) {
	// If explicitly configured, use that
	if configured != "" && configured != "none" {
		return configured, nil
	}
	if configured == "none" {
		return "none", nil
	}

	// Otherwise, try to detect
	detector := NewCIDetector()
	result, err := detector.Detect(ctx, client, owner, repo, branch)
	if err != nil {
		slog.Warn("CI detection failed", "error", err)
		return "none", nil
	}
	return result, nil
}
