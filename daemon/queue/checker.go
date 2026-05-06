package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"asika/common/config"
	"asika/common/db"
	"asika/common/gitutil"
	"asika/common/models"
	"asika/common/platforms"
)

// Checker checks if a queue item is ready to merge
type Checker struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
}

// NewChecker creates a new checker
func NewChecker(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Checker {
	return &Checker{
		cfg:     cfg,
		clients: clients,
	}
}

// TransientError indicates a temporary error that should be retried
type TransientError struct {
	Err error
}

func (e *TransientError) Error() string {
	return fmt.Sprintf("transient error: %v", e.Err)
}

func (e *TransientError) Unwrap() error {
	return e.Err
}

// ShouldMerge checks if a queue item should be merged
func (c *Checker) ShouldMerge(item *models.QueueItem) (bool, error) {
	ctx := context.Background()

	pr, err := getPRFromDB(item.RepoGroup, item.PRID)
	if err != nil {
		return false, err
	}

	// Get repo group config - falls back to "default" if name not found
	group := config.GetRepoGroupByName(c.cfg, pr.RepoGroup)
	if group == nil {
		return false, fmt.Errorf("repo group not found: %s", pr.RepoGroup)
	}

	// Check for merge conflicts - attempt auto-rebase if configured
	if pr.HasConflict {
		slog.Info("PR has merge conflicts, attempting auto-rebase", "pr_id", pr.ID, "title", pr.Title)
		if c.tryAutoRebase(ctx, pr, group) {
			pr.HasConflict = false
		} else {
			slog.Info("PR has merge conflicts, skipping", "pr_id", pr.ID, "title", pr.Title)
			return false, nil
		}
	}

	// Check approvals with retry for transient errors
	approved, approvals, err := c.checkApprovals(ctx, pr, group)
	if err != nil {
		return false, err
	}
	if !approved {
		return false, nil
	}

	// Check CI (skip if ci_check_required is false or ci_provider is "none"/empty)
	if group.MergeQueue.CICheckRequired && group.CIProvider != "none" && group.CIProvider != "" {
		ciPassed, ciStatus, err := c.checkCI(ctx, pr, group)
		if err != nil {
			return false, &TransientError{Err: err}
		}
		if !ciPassed {
			// Update criteria snapshot
			item.Criteria = models.MergeCriteria{
				RequiredApprovals: group.MergeQueue.RequiredApprovals,
				ApprovedBy:        approvals,
				CIStatus:          ciStatus,
			}
			return false, nil
		}
	}

	// Update criteria snapshot
	item.Criteria = models.MergeCriteria{
		RequiredApprovals: group.MergeQueue.RequiredApprovals,
		ApprovedBy:        approvals,
		CIStatus:          "success",
	}

	return true, nil
}

// checkApprovals checks if the PR has enough approvals from core contributors
func (c *Checker) checkApprovals(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) (bool, []string, error) {
	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return false, nil, fmt.Errorf("no client for platform: %s", pr.Platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return false, nil, fmt.Errorf("cannot resolve repo for platform %s in group %s", pr.Platform, group.Name)
	}

	// Retry transient network errors
	var approvals []string
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		approvals, err = client.GetApprovals(ctx, owner, repo, pr.PRNumber)
		if err == nil {
			break
		}
		if !isTransientError(err) {
			return false, nil, err
		}
		if attempt < 2 {
			slog.Warn("transient error fetching approvals, retrying", "pr_id", pr.ID, "attempt", attempt+1, "error", err)
		}
	}
	if err != nil {
		return false, nil, &TransientError{Err: err}
	}

	// Check if core contributors approved
	// If core_contributors list is empty, allow any approval
	coreApproved := make([]string, 0)
	isCoreListEmpty := len(group.MergeQueue.CoreContributors) == 0
	seen := make(map[string]bool)
	for _, approver := range approvals {
		if seen[approver] {
			continue
		}
		if isCoreListEmpty || contains(group.MergeQueue.CoreContributors, approver) {
			coreApproved = append(coreApproved, approver)
			seen[approver] = true
		}
	}

	return len(coreApproved) >= group.MergeQueue.RequiredApprovals, coreApproved, nil
}

// checkCI checks if CI passed
func (c *Checker) checkCI(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) (bool, string, error) {
	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return false, "none", fmt.Errorf("no client for platform: %s", pr.Platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return false, "none", fmt.Errorf("cannot resolve repo for platform %s", pr.Platform)
	}

	// Get the latest commit SHA from the PR
	commits, err := client.GetPRCommits(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		return false, "none", &TransientError{Err: err}
	}
	if len(commits) == 0 {
		return true, "none", nil
	}

	lastCommit := commits[len(commits)-1]
	status, err := client.GetCIStatus(ctx, owner, repo, lastCommit)
	if err != nil {
		return false, "none", &TransientError{Err: err}
	}

	return status == "success", status, nil
}

// isTransientError checks if an error is likely temporary
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected EOF") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "EOF")
}

func getPRFromDB(repoGroup, prID string) (*models.PRRecord, error) {
	var pr *models.PRRecord
	err := db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var record models.PRRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return err
		}
		if record.ID == prID {
			pr = &record
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, fmt.Errorf("PR not found: %s", prID)
	}
	return pr, nil
}

func contains(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}

// tryAutoRebase attempts to rebase a conflicted PR.
// Returns true if the rebase succeeded and the conflict was resolved.
func (c *Checker) tryAutoRebase(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) bool {
	platform := pr.Platform
	if platform == "" {
		if group.Mode == "single" && group.MirrorPlatform != "" {
			platform = group.MirrorPlatform
		} else if group.GitHub != "" {
			platform = "github"
		} else if group.GitLab != "" {
			platform = "gitlab"
		} else if group.Gitea != "" {
			platform = "gitea"
		}
	}

	client := c.clients[platforms.PlatformType(platform)]
	if client == nil {
		slog.Warn("auto-rebase: no platform client", "platform", platform, "pr_id", pr.ID)
		return false
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		slog.Warn("auto-rebase: cannot resolve repo", "platform", platform, "pr_id", pr.ID)
		return false
	}

	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		slog.Warn("auto-rebase: failed to get branch info", "error", err, "pr_id", pr.ID)
		return false
	}

	if !branchInfo.MaintainerCanModify {
		slog.Info("auto-rebase: maintainer cannot modify PR, skipping", "pr_id", pr.ID)
		return false
	}

	cloneURL := buildCloneURL(group, platform, owner, repo)
	token := getPlatformToken(c.cfg, platform)
	clonePath := c.cfg.Git.RepoClonePath

	rebaseErr := gitutil.Rebase("", cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
	if rebaseErr != nil {
		slog.Warn("auto-rebase: rebase failed", "error", rebaseErr, "pr_id", pr.ID)
		return false
	}

	slog.Info("auto-rebase: succeeded", "pr_id", pr.ID, "head_branch", branchInfo.HeadBranch, "base_branch", branchInfo.BaseBranch)
	return true
}

func buildCloneURL(group *models.RepoGroup, platform, owner, repo string) string {
	var baseURL string
	switch platform {
	case "github":
		baseURL = "https://github.com"
	case "gitlab":
		cfg := config.Current()
		if cfg != nil && cfg.GitLabBaseURL != "" {
			baseURL = strings.TrimSuffix(cfg.GitLabBaseURL, "/")
		} else {
			baseURL = "https://gitlab.com"
		}
	case "gitea":
		cfg := config.Current()
		if cfg != nil && cfg.GiteaBaseURL != "" {
			baseURL = strings.TrimSuffix(cfg.GiteaBaseURL, "/")
		} else {
			return ""
		}
	}
	return fmt.Sprintf("%s/%s/%s", baseURL, owner, repo)
}

func getPlatformToken(cfg *models.Config, platform string) string {
	if cfg == nil {
		return ""
	}
	switch platform {
	case "github":
		return cfg.Tokens.GitHub
	case "gitlab":
		return cfg.Tokens.GitLab
	case "gitea":
		return cfg.Tokens.Gitea
	}
	return ""
}