package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
)

// findTargetPR searches for the corresponding PR on the target platform by matching
// head branch + base branch. Returns nil if no matching open PR is found.
func (s *Syncer) findTargetPR(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup, targetPlatform string) (*models.PRRecord, error) {
	if pr.BranchInfo == nil {
		return nil, nil
	}
	client, ok := s.clients[platforms.PlatformType(targetPlatform)]
	if !ok {
		return nil, nil
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, targetPlatform)
	if owner == "" || repo == "" {
		return nil, nil
	}
	targetPRs, err := client.ListPRs(ctx, owner, repo, "open")
	if err != nil {
		return nil, fmt.Errorf("list PRs on %s: %w", targetPlatform, err)
	}
	for _, tpr := range targetPRs {
		if tpr.BranchInfo != nil &&
			tpr.BranchInfo.HeadBranch == pr.BranchInfo.HeadBranch &&
			tpr.BranchInfo.BaseBranch == pr.BranchInfo.BaseBranch {
			return tpr, nil
		}
	}
	return nil, nil
}

// syncPRState closes/merges the corresponding PRs on target platforms after a successful sync.
// This prevents PRs from remaining open on platforms where the code has already been merged.
func (s *Syncer) syncPRState(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) {
	if !group.SyncPRState {
		return
	}
	if pr.BranchInfo == nil {
		slog.Info("syncPRState: no branch info, skipping", "pr", pr.PRNumber)
		return
	}
	targetPlatforms := s.getTargetPlatforms(group, pr.Platform)
	for _, target := range targetPlatforms {
		s.syncTargetPR(ctx, pr, group, target.name)
	}
}

func (s *Syncer) syncTargetPR(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup, targetPlatform string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	targetPR, err := s.findTargetPR(ctx, pr, group, targetPlatform)
	if err != nil {
		slog.Warn("syncPRState: findTargetPR failed",
			"target", targetPlatform, "pr", pr.PRNumber, "error", err)
		return
	}
	if targetPR == nil {
		return
	}

	client, ok := s.clients[platforms.PlatformType(targetPlatform)]
	if !ok {
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, targetPlatform)
	if owner == "" || repo == "" {
		return
	}

	if err := client.MergePR(ctx, owner, repo, targetPR.PRNumber, "merge"); err != nil {
		slog.Warn("syncPRState: merge failed on target, trying close",
			"target", targetPlatform, "pr", targetPR.PRNumber, "error", err)
		if closeErr := client.ClosePR(ctx, owner, repo, targetPR.PRNumber); closeErr != nil {
			slog.Error("syncPRState: close also failed on target",
				"target", targetPlatform, "pr", targetPR.PRNumber, "error", closeErr)
			s.notifySyncFailure(pr, targetPlatform,
				fmt.Sprintf("failed to sync PR state for #%d on %s: merge err=%v, close err=%v",
					targetPR.PRNumber, targetPlatform, err, closeErr))
			return
		}
	}

	slog.Info("syncPRState: synced PR state on target",
		"target", targetPlatform, "pr", targetPR.PRNumber, "source_pr", pr.PRNumber)
}

// preSyncConflictCheck checks target platforms for open PRs whose head branch
// matches the source PR's head branch. If found, it logs a warning because
// syncing the merge commit may cause those PRs to silently lose their changes
// (dabao1955's concern: PR B's changes get included in PR A's merge commit
// through the sync, making PR B appear already-merged on the target platform).
func (s *Syncer) preSyncConflictCheck(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) {
	if pr.BranchInfo == nil {
		return
	}
	targetPlatforms := s.getTargetPlatforms(group, pr.Platform)
	for _, target := range targetPlatforms {
		s.checkTargetConflict(ctx, pr, group, target.name)
	}
}

func (s *Syncer) checkTargetConflict(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup, targetPlatform string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client, ok := s.clients[platforms.PlatformType(targetPlatform)]
	if !ok {
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, targetPlatform)
	if owner == "" || repo == "" {
		return
	}

	targetPRs, err := client.ListPRs(ctx, owner, repo, "open")
	if err != nil {
		slog.Warn("preSyncConflictCheck: list PRs failed",
			"target", targetPlatform, "error", err)
		return
	}

	sourceHead := pr.BranchInfo.HeadBranch
	for _, tpr := range targetPRs {
		if tpr.BranchInfo == nil {
			continue
		}
		if tpr.BranchInfo.HeadBranch == sourceHead {
			slog.Warn("preSyncConflictCheck: target has open PR with same head branch — "+
				"this PR's changes may be silently lost after sync (dabao1955 scenario)",
				"target", targetPlatform,
				"target_pr", tpr.PRNumber,
				"target_pr_title", tpr.Title,
				"head_branch", sourceHead,
				"source_pr", pr.PRNumber)
			s.notifySyncFailure(pr, targetPlatform,
				fmt.Sprintf("⚠️ PR #%d on %s has same head branch (%s) as source PR #%d. "+
					"Merging source may cause target PR changes to be lost. "+
					"Consider merging target PR first.",
					tpr.PRNumber, targetPlatform, sourceHead, pr.PRNumber))
		}
	}
}
