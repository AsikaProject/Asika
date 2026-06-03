package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"sync"
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

// IsReadyToMerge checks if a PR currently satisfies all merge conditions
// (approvals, CI, conflicts) without requiring a QueueItem. Used by the
// consumer to decide whether to enqueue a PR immediately after approval.
func (c *Checker) IsReadyToMerge(pr *models.PRRecord) (bool, error) {
	if pr == nil || pr.State != "open" || pr.IsDraft {
		return false, nil
	}

	group := config.GetRepoGroupByName(c.cfg, pr.RepoGroup)
	if group == nil {
		return false, nil
	}

	mq := group.MergeQueue

	if pr.HasConflict {
		slog.Info("PR has merge conflicts, hard gate blocking", "pr_id", pr.ID, "title", pr.Title)
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	approvalStatus, err := c.fetchApprovals(ctx, pr, group)
	if err != nil {
		return false, err
	}
	approvals, err := c.filterWritePermission(ctx, pr, group, approvalStatus.Approvers)
	if err != nil {
		return false, err
	}

	if len(approvals) < mq.RequiredApprovals {
		slog.Info("PR does not meet approval requirement, skipping enqueue",
			"pr_id", pr.ID, "approvals", len(approvals), "required", mq.RequiredApprovals)
		return false, nil
	}

	// Check path-specific approval rules
	if len(group.ApprovalRules) > 0 {
		pathApproved, err := c.checkPathApprovals(ctx, pr, group, approvals)
		if err != nil {
			return false, err
		}
		if !pathApproved {
			slog.Info("PR does not meet path approval requirements, skipping enqueue", "pr_id", pr.ID)
			return false, nil
		}
	}

	// Check PR size limits
	if group.PRSizeLimits.Enabled {
		sizeOK, err := c.checkPRSize(ctx, pr, group)
		if err != nil {
			return false, err
		}
		if !sizeOK {
			slog.Info("PR exceeds size limits, skipping enqueue", "pr_id", pr.ID)
			return false, nil
		}
	}

	// Check PR template enforcement
	if group.PRTemplate.Enabled {
		templateOK, err := c.checkPRTemplate(ctx, pr, group)
		if err != nil {
			return false, err
		}
		if !templateOK {
			slog.Info("PR does not meet template requirements, skipping enqueue", "pr_id", pr.ID)
			return false, nil
		}
	}

	if mq.CICheckRequired && group.CIProvider != "none" && group.CIProvider != "" {
		passed, status, err := c.checkCI(ctx, pr, group)
		if err != nil {
			return false, err
		}
		if !passed {
			slog.Info("PR CI not passed, skipping enqueue",
				"pr_id", pr.ID, "ci_status", status)
			return false, nil
		}
	}

	// Security check: block enqueue if security alerts exist
	secure, err := c.checkSecurity(ctx, pr, group)
	if err != nil {
		return false, err
	}
	if !secure {
		slog.Info("PR blocked by security alerts, skipping enqueue", "pr_id", pr.ID)
		return false, nil
	}

	if mq.Expression != "" {
		labelSet := make(map[string]bool)
		for _, l := range pr.Labels {
			labelSet[l] = true
		}
		coreContribMap := make(map[string]bool)
		for _, cc := range mq.CoreContributors {
			coreContribMap[cc] = true
		}
		coreApproved := 0
		for _, a := range approvals {
			if coreContribMap[a] {
				coreApproved++
			}
		}
		ageHours := 0.0
		if !pr.CreatedAt.IsZero() {
			ageHours = time.Since(pr.CreatedAt).Hours()
		}
		evalCtx := EvalContext{
			Approvals:        len(approvals),
			Required:         mq.RequiredApprovals,
			CIStatus:         "success",
			HasConflict:      pr.HasConflict,
			IsDraft:          pr.IsDraft,
			CoreApproved:     coreApproved,
			Author:           pr.Author,
			CoreContributors: coreContribMap,
			AgeHours:         ageHours,
			Labels:           labelSet,
		}
		result, err := Eval(mq.Expression, evalCtx)
		if err != nil {
			slog.Error("merge expression evaluation failed", "error", err, "expression", mq.Expression, "pr_id", pr.ID)
			return false, nil
		}
		if !result {
			slog.Info("PR does not match merge expression, skipping enqueue", "pr_id", pr.ID)
			return false, nil
		}
	}

	return true, nil
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

	if pr.HasConflict {
		slog.Info("PR has merge conflicts, attempting auto-rebase", "pr_id", pr.ID, "title", pr.Title)
		if c.tryAutoRebase(ctx, pr, group) {
			pr.HasConflict = false
		} else {
			slog.Info("PR has merge conflicts, hard gate blocking", "pr_id", pr.ID, "title", pr.Title)
			return false, nil
		}
	}

	approvalStatus, err := c.fetchApprovals(ctx, pr, group)
	if err != nil {
		return false, err
	}

	approvals, err := c.filterWritePermission(ctx, pr, group, approvalStatus.Approvers)
	if err != nil {
		return false, err
	}
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

	// Security check (skip if overridden)
	if item.SecurityOverride == "" {
		secure, err := c.checkSecurity(ctx, pr, group)
		if err != nil {
			return false, &TransientError{Err: err}
		}
		if !secure {
			item.SecurityBlocked = true
			item.FailureReason = fmt.Sprintf("Blocked by security alerts (%d)", len(pr.SecurityAlerts))
			return false, nil
		}
	}
	item.SecurityBlocked = false

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

	if len(group.ApprovalRules) > 0 {
		pathOK, err := c.checkPathApprovals(ctx, pr, group, approvals)
		if err != nil {
			return false, err
		}
		if !pathOK {
			item.Criteria = models.MergeCriteria{
				RequiredApprovals: mq.RequiredApprovals,
				ApprovedBy:        approvals,
				CIStatus:          ciStatus,
			}
			return false, nil
		}
	}

	if group.PRSizeLimits.Enabled {
		sizeOK, err := c.checkPRSize(ctx, pr, group)
		if err != nil {
			return false, err
		}
		if !sizeOK {
			item.Criteria = models.MergeCriteria{
				RequiredApprovals: mq.RequiredApprovals,
				ApprovedBy:        approvals,
				CIStatus:          ciStatus,
			}
			return false, nil
		}
	}

	if group.PRTemplate.Enabled {
		tplOK, err := c.checkPRTemplate(ctx, pr, group)
		if err != nil {
			return false, err
		}
		if !tplOK {
			item.Criteria = models.MergeCriteria{
				RequiredApprovals: mq.RequiredApprovals,
				ApprovedBy:        approvals,
				CIStatus:          ciStatus,
			}
			return false, nil
		}
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

func (c *Checker) filterWritePermission(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup, approvers []string) ([]string, error) {
	if len(approvers) == 0 {
		return approvers, nil
	}
	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return approvers, nil
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return approvers, nil
	}
	var filtered []string
	for _, username := range approvers {
		hasWrite, err := client.HasWritePermission(ctx, owner, repo, username)
		if err != nil {
			slog.Warn("failed to check write permission, skipping approver", "username", username, "error", err)
			continue
		}
		if hasWrite {
			filtered = append(filtered, username)
		} else {
			slog.Info("approver filtered: no write permission", "username", username, "pr_id", pr.ID)
		}
	}
	return filtered, nil
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
	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err == nil && data != nil {
		var pr models.PRRecord
		if json.Unmarshal(data, &pr) == nil {
			if pr.RepoGroup != "" && pr.RepoGroup != repoGroup {
				return nil, fmt.Errorf("pr repo_group mismatch: expected %s, got %s", repoGroup, pr.RepoGroup)
			}
			return &pr, nil
		}
	}
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

// checkPathApprovals checks file-path-based approval rules.
// For each rule whose pattern matches changed files, at least one approver must have approved.
func (c *Checker) checkPathApprovals(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup, approvals []string) (bool, error) {
	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return false, fmt.Errorf("no client for platform: %s", pr.Platform)
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return false, fmt.Errorf("cannot resolve repo for platform %s", pr.Platform)
	}

	files, err := client.GetDiffFiles(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		return false, fmt.Errorf("failed to get diff files: %w", err)
	}

	approvalSet := make(map[string]bool, len(approvals))
	for _, a := range approvals {
		approvalSet[a] = true
	}

	for _, rule := range group.ApprovalRules {
		matched := false
		for _, f := range files {
			if matchSinglePattern(rule.Pattern, f) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		minCount := rule.MinCount
		if minCount <= 0 {
			minCount = 1
		}
		matchedApprovers := 0
		for _, approver := range rule.Approvers {
			if approvalSet[approver] {
				matchedApprovers++
			}
		}
		if matchedApprovers < minCount {
			reason := rule.Reason
			if reason == "" {
				reason = fmt.Sprintf("path rule %q requires %d approver(s) from %v, got %d",
					rule.Pattern, minCount, rule.Approvers, matchedApprovers)
			}
			slog.Info("path approval not met", "pr_id", pr.ID, "rule", rule.Pattern, "reason", reason)
			return false, nil
		}
	}
	return true, nil
}

// checkPRSize checks PR size limits and optionally blocks merge.
func (c *Checker) checkPRSize(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) (bool, error) {
	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return false, fmt.Errorf("no client for platform: %s", pr.Platform)
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return false, fmt.Errorf("cannot resolve repo for platform %s", pr.Platform)
	}

	diffFiles, err := client.GetPRDiff(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		return false, fmt.Errorf("failed to get PR diff: %w", err)
	}

	totalFiles := len(diffFiles)
	totalLines := 0
	for _, df := range diffFiles {
		totalLines += df.Additions + df.Deletions
	}

	if len(group.PRSizeLimits.ExemptLabels) > 0 {
		for _, exempt := range group.PRSizeLimits.ExemptLabels {
			for _, label := range pr.Labels {
				if label == exempt {
					slog.Info("PR size check skipped: exempt label", "pr_id", pr.ID, "label", exempt)
					return true, nil
				}
			}
		}
	}

	cfg := group.PRSizeLimits
	blocked := false
	if cfg.BlockMerge {
		if cfg.LinesChangedBlock > 0 && totalLines > cfg.LinesChangedBlock {
			blocked = true
		}
		if cfg.FilesChangedBlock > 0 && totalFiles > cfg.FilesChangedBlock {
			blocked = true
		}
	}

	if blocked {
		slog.Info("PR exceeds size limits, blocking merge",
			"pr_id", pr.ID, "lines", totalLines, "files", totalFiles,
			"lines_limit", cfg.LinesChangedBlock, "files_limit", cfg.FilesChangedBlock)

		if cfg.BigPRLabel != "" {
			if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, cfg.BigPRLabel, "ededed"); err != nil {
				slog.Warn("failed to add big PR label", "error", err, "pr_id", pr.ID)
			}
		}
		return false, nil
	}

	warned := false
	if cfg.LinesChangedWarn > 0 && totalLines > cfg.LinesChangedWarn {
		warned = true
	}
	if cfg.FilesChangedWarn > 0 && totalFiles > cfg.FilesChangedWarn {
		warned = true
	}

	if warned && cfg.WarnPRLabel != "" {
		slog.Info("PR exceeds size warning threshold, adding label",
			"pr_id", pr.ID, "lines", totalLines, "files", totalFiles)
		if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, cfg.WarnPRLabel, "ffcc00"); err != nil {
			slog.Warn("failed to add warn PR label", "error", err, "pr_id", pr.ID)
		}
	}

	return true, nil
}

// checkPRTemplate validates PR description against template enforcement rules.
func (c *Checker) checkPRTemplate(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) (bool, error) {
	cfg := group.PRTemplate

	if len(cfg.ExemptLabels) > 0 {
		for _, exempt := range cfg.ExemptLabels {
			for _, label := range pr.Labels {
				if label == exempt {
					slog.Info("PR template check skipped: exempt label", "pr_id", pr.ID, "label", exempt)
					return true, nil
				}
			}
		}
	}

	body := pr.Body
	if body == "" {
		client := c.clients[platforms.PlatformType(pr.Platform)]
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if owner != "" && repo != "" {
				if fetched, err := client.GetPRBody(ctx, owner, repo, pr.PRNumber); err == nil {
					body = fetched
				}
			}
		}
	}

	if cfg.RequireBody && (body == "" || len(strings.TrimSpace(body)) < 10) {
		slog.Info("PR template enforcement: body required but missing", "pr_id", pr.ID)
		if cfg.BlockMerge {
			return false, nil
		}
	}

	if cfg.RequireChecklist {
		complete, total, unchecked := validateChecklist(body)
		if total > 0 && !complete {
			slog.Info("PR template enforcement: checklist incomplete",
				"pr_id", pr.ID, "total", total, "unchecked", unchecked)
			if cfg.IncompleteLabel != "" {
				client := c.clients[platforms.PlatformType(pr.Platform)]
				if client != nil {
					owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
					if owner != "" && repo != "" {
						if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, cfg.IncompleteLabel, "ff0000"); err != nil {
							slog.Warn("failed to add incomplete label", "error", err, "pr_id", pr.ID)
						}
					}
				}
			}
			if cfg.BlockMerge {
				return false, nil
			}
		}
	}

	return true, nil
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

// checkSecurity checks if the PR has security alerts and blocks merging if configured.
func (c *Checker) checkSecurity(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) (bool, error) {
	mq := group.MergeQueue
	if !mq.SecurityScan.Enabled || !mq.SecurityScan.BlockOnAlerts {
		return true, nil
	}

	platform := pr.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}

	client := c.clients[platforms.PlatformType(platform)]
	if client == nil {
		return true, nil
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return true, nil
	}

	alerts, err := client.HasSecurityAlerts(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		slog.Warn("security check failed", "error", err, "pr_id", pr.ID)
		return true, nil
	}

	if len(alerts) > 0 {
		pr.SecurityBlocked = true
		pr.SecurityAlerts = alerts
		slog.Warn("PR blocked by security alerts", "pr_id", pr.ID, "count", len(alerts))
		return false, nil
	}

	pr.SecurityBlocked = false
	pr.SecurityAlerts = nil
	return true, nil
}

var checklistPattern = regexp.MustCompile(`(?m)^\s*[-*]\s+\[([ x])\]`)

func validateChecklist(body string) (complete bool, total int, unchecked int) {
	matches := checklistPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return true, 0, 0
	}
	total = len(matches)
	for _, m := range matches {
		if m[1] != "x" && m[1] != "X" {
			unchecked++
		}
	}
	return unchecked == 0, total, unchecked
}

var singlePatternCache sync.RWMutex
var compiledSinglePatterns = make(map[string]*regexp.Regexp)

func matchSinglePattern(pattern, file string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		matched, _ := path.Match(pattern, file)
		if matched {
			return true
		}
	}
	singlePatternCache.RLock()
	re, ok := compiledSinglePatterns[pattern]
	singlePatternCache.RUnlock()
	if !ok {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return false
		}
		singlePatternCache.Lock()
		if len(compiledSinglePatterns) > 1000 {
			for k := range compiledSinglePatterns {
				delete(compiledSinglePatterns, k)
				break
			}
		}
		compiledSinglePatterns[pattern] = re
		singlePatternCache.Unlock()
	}
	return re.MatchString(file)
}
