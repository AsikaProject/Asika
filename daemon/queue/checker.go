package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pr, err := getPRFromDB(item.RepoGroup, item.PRID)
	if err != nil {
		return false, err
	}

	group := config.GetRepoGroupByName(c.cfg, pr.RepoGroup)
	if group == nil {
		return false, fmt.Errorf("repo group not found: %s", pr.RepoGroup)
	}

	mq := group.MergeQueue

	if pr.HasConflict && mq.Expression == "" {
		slog.Info("PR has merge conflicts, attempting auto-rebase", "pr_id", pr.ID, "title", pr.Title)
		if c.tryAutoRebase(ctx, pr, group) {
			pr.HasConflict = false
		} else {
			slog.Info("PR has merge conflicts, skipping", "pr_id", pr.ID, "title", pr.Title)
			return false, nil
		}
	}

	approvalStatus, err := c.fetchApprovals(ctx, pr, group)
	if err != nil {
		return false, err
	}

	approvals := approvalStatus.Approvers
	coreApproved := 0
	coreSet := make(map[string]bool, len(mq.CoreContributors))
	for _, cc := range mq.CoreContributors {
		coreSet[cc] = true
	}
	for _, a := range approvals {
		if coreSet[a] {
			coreApproved++
		}
	}

	ciStatus := "none"
	if mq.CICheckRequired && group.CIProvider != "none" && group.CIProvider != "" {
		passed, status, err := c.checkCI(ctx, pr, group)
		if err != nil {
			return false, &TransientError{Err: err}
		}
		ciStatus = status
		if !passed && mq.Expression == "" {
			item.Criteria = models.MergeCriteria{
				RequiredApprovals: mq.RequiredApprovals,
				ApprovedBy:        approvals,
				CIStatus:          ciStatus,
			}
			return false, nil
		}
	}

	labelSet := make(map[string]bool)
	for _, l := range pr.Labels {
		labelSet[l] = true
	}

	coreContribMap := make(map[string]bool)
	for _, cc := range mq.CoreContributors {
		coreContribMap[cc] = true
	}

	ageHours := 0.0
	if !pr.CreatedAt.IsZero() {
		ageHours = time.Since(pr.CreatedAt).Hours()
	}

	evalCtx := EvalContext{
		Approvals:        len(approvals),
		Required:         mq.RequiredApprovals,
		CIStatus:         ciStatus,
		HasConflict:      pr.HasConflict,
		IsDraft:          pr.IsDraft,
		CoreApproved:     coreApproved,
		Author:           pr.Author,
		CoreContributors: coreContribMap,
		AgeHours:         ageHours,
		Labels:           labelSet,
	}

	if mq.Expression != "" {
		result, err := Eval(mq.Expression, evalCtx)
		if err != nil {
			slog.Error("merge expression evaluation failed", "error", err, "expression", mq.Expression, "pr_id", pr.ID)
			return false, fmt.Errorf("merge expression error: %w", err)
		}
		item.Criteria = models.MergeCriteria{
			RequiredApprovals: mq.RequiredApprovals,
			ApprovedBy:        approvals,
			CIStatus:          ciStatus,
		}
		return result, nil
	}

	if len(approvals) < mq.RequiredApprovals {
		item.Criteria = models.MergeCriteria{
			RequiredApprovals: mq.RequiredApprovals,
			ApprovedBy:        approvals,
			CIStatus:          ciStatus,
		}
		return false, nil
	}

	if ciStatus != "none" && ciStatus != "success" {
		item.Criteria = models.MergeCriteria{
			RequiredApprovals: mq.RequiredApprovals,
			ApprovedBy:        approvals,
			CIStatus:          ciStatus,
		}
		return false, nil
	}

	item.Criteria = models.MergeCriteria{
		RequiredApprovals: mq.RequiredApprovals,
		ApprovedBy:        approvals,
		CIStatus:          "success",
	}
	return true, nil
}

func (c *Checker) fetchApprovals(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) (*models.ApprovalStatus, error) {
	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return nil, fmt.Errorf("no client for platform: %s", pr.Platform)
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("cannot resolve repo for platform %s in group %s", pr.Platform, group.Name)
	}
	var status *models.ApprovalStatus
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		status, err = client.GetApprovals(ctx, owner, repo, pr.PRNumber)
		if err == nil {
			break
		}
		if !isTransientError(err) {
			return nil, err
		}
		if attempt < 2 {
			slog.Warn("transient error fetching approvals, retrying", "pr_id", pr.ID, "attempt", attempt+1, "error", err)
		}
	}
	if err != nil {
		return nil, &TransientError{Err: err}
	}
	return status, nil
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

var errStop = fmt.Errorf("stop")

func getPRFromDB(repoGroup, prID string) (*models.PRRecord, error) {
	// Try index lookup first (fast path for production data)
	data, err := db.GetPRByIndex(prID, "", 0)
	if err == nil && data != nil {
		var pr models.PRRecord
		if json.Unmarshal(data, &pr) == nil {
			return &pr, nil
		}
	}
	// Fallback: direct key lookup by repoGroup#prID
	key := fmt.Sprintf("%s#%s", repoGroup, prID)
	data, err = db.Get(db.BucketPRs, key)
	if err == nil && data != nil {
		var pr models.PRRecord
		if json.Unmarshal(data, &pr) == nil {
			return &pr, nil
		}
	}
	// Last resort: scan PRs by repoGroup prefix (bounded scan, not full table)
	var found *models.PRRecord
	prefix := repoGroup + "#"
	_ = db.BucketForEachPrefix(db.BucketPRs, prefix, func(k, v []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(v, &pr) != nil {
			return nil
		}
		if pr.ID == prID {
			found = &pr
			return errStop
		}
		return nil
	})
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("PR not found: %s", prID)
}

// tryAutoRebase attempts to rebase a conflicted PR.
// Returns true if the rebase succeeded and the conflict was resolved.
func (c *Checker) tryAutoRebase(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) bool {
	platform := pr.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
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

	cloneURL := config.GetCloneURL(platform, owner, repo)
	token := config.GetToken(c.cfg, platform)
	clonePath := c.cfg.Git.RepoClonePath

	rebaseErr := gitutil.Rebase("", cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
	if rebaseErr != nil {
		slog.Warn("auto-rebase: rebase failed", "error", rebaseErr, "pr_id", pr.ID)
		return false
	}

	slog.Info("auto-rebase: succeeded", "pr_id", pr.ID, "head_branch", branchInfo.HeadBranch, "base_branch", branchInfo.BaseBranch)
	return true
}
