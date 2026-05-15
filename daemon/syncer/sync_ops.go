package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"asika/common/config"
	"asika/common/events"
	"asika/common/gitutil"
	"asika/common/models"
	"asika/daemon/hooks"
)

// SyncOnMerge handles a merge event and syncs to other platforms.
// Syncs the default branch, then optionally syncs all branches and tags.
func (s *Syncer) SyncOnMerge(ctx context.Context, pr *models.PRRecord) error {
	group := config.GetRepoGroupByName(s.cfg, pr.RepoGroup)
	if group == nil {
		return fmt.Errorf("repo group not found: %s", pr.RepoGroup)
	}

	if group.Mode != "multi" {
		slog.Info("skipping sync: repo group not in multi mode", "repo_group", pr.RepoGroup)
		return nil
	}

	mu := s.getOrCreateLock(pr.RepoGroup)
	mu.Lock()
	defer mu.Unlock()

	repoDir, cleanup, err := s.prepareRepoDir(pr, group)
	if err != nil {
		return err
	}
	if cleanup {
		defer gitutil.CleanupWorkdir(repoDir)
	}
	gitRepo, err := s.openOrClone(repoDir, pr, group)
	if err != nil {
		return err
	}

	sourceToken := config.GetToken(s.cfg, pr.Platform)

	if err := gitutil.FetchRemote(gitRepo, "origin", sourceToken); err != nil {
		slog.Warn("fetch failed, continuing", "error", err)
	}

	s.preSyncConflictCheck(ctx, pr, group)

	if err := s.syncDefaultBranch(gitRepo, pr, group); err != nil {
		return err
	}

	if group.BranchSync != "" {
		if err := s.syncAllBranches(gitRepo, pr, group); err != nil {
			slog.Error("branch sync failed", "error", err)
			s.notifySyncFailure(pr, "all-targets", fmt.Sprintf("branch sync failed: %v", err))
		}
	}

	if group.SyncTags {
		if err := s.syncAllTags(gitRepo, pr, group); err != nil {
			slog.Error("tag sync failed", "error", err)
			s.notifySyncFailure(pr, "all-targets", fmt.Sprintf("tag sync failed: %v", err))
		}
	}

	s.syncPRState(ctx, pr, group)

	slog.Info("sync completed for all targets", "repo_group", pr.RepoGroup)
	events.PublishPR(events.EventSyncCompleted, pr.RepoGroup, pr.Platform, pr, nil)
	return nil
}

// prepareRepoDir returns the repo directory path and whether it needs cleanup.
func (s *Syncer) prepareRepoDir(pr *models.PRRecord, group *models.RepoGroup) (string, bool, error) {
	if s.cfg.Git.RepoClonePath != "" {
		safeName := strings.ReplaceAll(pr.RepoGroup, "/", "_")
		return filepath.Join(s.cfg.Git.RepoClonePath, "sync-"+safeName), false, nil
	}
	dir, err := gitutil.CreateTempWorkdir("asika-sync-")
	if err != nil {
		return "", false, fmt.Errorf("failed to create temp workdir: %w", err)
	}
	return dir, true, nil
}

// openOrClone opens an existing bare repo or clones fresh.
func (s *Syncer) openOrClone(repoDir string, pr *models.PRRecord, group *models.RepoGroup) (*git.Repository, error) {
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	sourceRepo := owner + "/" + repo
	if sourceRepo == "" || sourceRepo == "/" {
		sourceRepo = group.Gerrit
	}
	if sourceRepo == "" {
		return nil, fmt.Errorf("no repo configured for source platform %s", pr.Platform)
	}

	sourceURL, err := s.getRepoURL(pr.Platform, sourceRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to get source repo URL: %w", err)
	}
	sourceToken := config.GetToken(s.cfg, pr.Platform)

	if s.cfg.Git.RepoClonePath != "" {
		if _, err := os.Stat(repoDir); os.IsNotExist(err) {
			slog.Info("cloning bare repo cache", "repo_dir", repoDir)
			auth := &http.BasicAuth{Username: "git", Password: sourceToken}
			_, err := git.PlainClone(repoDir, true, &git.CloneOptions{
				URL:  sourceURL,
				Auth: auth,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to clone bare repo: %w", err)
			}
		}
		return git.PlainOpen(repoDir)
	}

	return gitutil.CloneOrOpen(repoDir, sourceURL, sourceToken)
}

// syncDefaultBranch syncs the default branch merge commit to all targets.
func (s *Syncer) syncDefaultBranch(gitRepo *git.Repository, pr *models.PRRecord, group *models.RepoGroup) error {
	if err := gitutil.CheckoutBranch(gitRepo, group.DefaultBranch); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", group.DefaultBranch, err)
	}

	if group.HookPath != "" {
		hookRunner := hooks.NewRunner(group.HookPath)
		if err := hookRunner.Run("pre-sync", "", "", pr.MergeCommitSHA, "refs/heads/"+group.DefaultBranch); err != nil {
			slog.Warn("pre-sync hook failed", "error", err)
		}
	}

	sourceToken := config.GetToken(s.cfg, pr.Platform)
	if err := s.cherryPickWithRetry(gitRepo, pr.MergeCommitSHA, pr, group.DefaultBranch, sourceToken); err != nil {
		s.notifySyncFailure(pr, "", fmt.Sprintf("cherry-pick failed after %d retries: %v", syncMaxRetries, err))
		events.PublishPR(events.EventSyncFailed, pr.RepoGroup, pr.Platform, pr, err.Error())
		return err
	}

	return s.pushBranchToTargets(gitRepo, pr, group, group.DefaultBranch)
}

// syncAllBranches syncs all branches from source to target platforms.
func (s *Syncer) syncAllBranches(gitRepo *git.Repository, pr *models.PRRecord, group *models.RepoGroup) error {
	branchRefs, err := gitRepo.Branches()
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}

	var branches []string
	err = branchRefs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		if name != "HEAD" {
			branches = append(branches, name)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to iterate branches: %w", err)
	}

	targetPlatforms := s.getTargetPlatforms(group, pr.Platform)
	var failedBranches []string

	for _, branch := range branches {
		slog.Info("syncing branch", "branch", branch, "source", pr.Platform)

		if err := gitutil.CheckoutBranch(gitRepo, branch); err != nil {
			slog.Warn("failed to checkout branch, skipping", "branch", branch, "error", err)
			continue
		}

		branchFailed := false
		for _, target := range targetPlatforms {
			remoteName := "target-" + target.name
			targetURL, urlErr := s.getRepoURL(target.name, target.repo)
			if urlErr != nil {
				slog.Error("failed to get target repo URL", "target", target.name, "error", urlErr)
				branchFailed = true
				continue
			}
			if err := gitutil.AddRemote(gitRepo, remoteName, targetURL); err != nil {
				slog.Warn("add remote failed", "remote", remoteName, "error", err)
			}
			targetToken := config.GetToken(s.cfg, target.name)
			if err := s.pushRef(gitRepo, remoteName, "refs/heads/"+branch, targetToken, target.name); err != nil {
				slog.Error("failed to push branch", "branch", branch, "target", target.name, "error", err)
				if err := s.recordSync(pr, branch, target.name, "failed", err.Error()); err != nil {
					slog.Error("recordSync failed", "error", err)
				}
				branchFailed = true
				continue
			}
			if err := s.recordSync(pr, branch, target.name, "success", ""); err != nil {
				slog.Error("recordSync failed", "error", err)
			}
		}

		if branchFailed {
			failedBranches = append(failedBranches, branch)
		}
	}

	if len(failedBranches) > 0 {
		return fmt.Errorf("branch sync failed for: %s", strings.Join(failedBranches, ", "))
	}
	return nil
}

// syncAllTags syncs all tags from source to target platforms.
func (s *Syncer) syncAllTags(gitRepo *git.Repository, pr *models.PRRecord, group *models.RepoGroup) error {
	tagRefs, err := gitRepo.Tags()
	if err != nil {
		return fmt.Errorf("failed to list tags: %w", err)
	}

	var tags []string
	err = tagRefs.ForEach(func(ref *plumbing.Reference) error {
		tags = append(tags, ref.Name().Short())
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to iterate tags: %w", err)
	}

	targetPlatforms := s.getTargetPlatforms(group, pr.Platform)
	var failedTags []string

	for _, tag := range tags {
		slog.Info("syncing tag", "tag", tag, "source", pr.Platform)

		tagFailed := false
		for _, target := range targetPlatforms {
			remoteName := "target-" + target.name
			targetURL, urlErr := s.getRepoURL(target.name, target.repo)
			if urlErr != nil {
				slog.Error("failed to get target repo URL", "target", target.name, "error", urlErr)
				tagFailed = true
				continue
			}
			if err := gitutil.AddRemote(gitRepo, remoteName, targetURL); err != nil {
				slog.Warn("add remote failed", "remote", remoteName, "error", err)
			}
			targetToken := config.GetToken(s.cfg, target.name)
			if err := s.pushRef(gitRepo, remoteName, "refs/tags/"+tag, targetToken, target.name); err != nil {
				slog.Error("failed to push tag", "tag", tag, "target", target.name, "error", err)
				if err := s.recordSync(pr, "tag:"+tag, target.name, "failed", err.Error()); err != nil {
					slog.Error("recordSync failed", "error", err)
				}
				tagFailed = true
				continue
			}
			if err := s.recordSync(pr, "tag:"+tag, target.name, "success", ""); err != nil {
				slog.Error("recordSync failed", "error", err)
			}
		}

		if tagFailed {
			failedTags = append(failedTags, tag)
		}
	}

	if len(failedTags) > 0 {
		return fmt.Errorf("tag sync failed for: %s", strings.Join(failedTags, ", "))
	}
	return nil
}

// pushBranchToTargets pushes the current branch to all target platforms.
func (s *Syncer) pushBranchToTargets(gitRepo *git.Repository, pr *models.PRRecord, group *models.RepoGroup, branch string) error {
	targetPlatforms := s.getTargetPlatforms(group, pr.Platform)

	var failedTargets []string
	for _, target := range targetPlatforms {
		slog.Info("syncing to platform", "target", target.name, "repo", target.repo, "branch", branch)

		targetURL, err := s.getRepoURL(target.name, target.repo)
		if err != nil {
			slog.Error("failed to get target repo URL", "target", target.name, "error", err)
			failedTargets = append(failedTargets, target.name)
			continue
		}
		targetToken := config.GetToken(s.cfg, target.name)
		remoteName := "target-" + target.name

		if err := gitutil.AddRemote(gitRepo, remoteName, targetURL); err != nil {
			slog.Warn("add remote failed (may already exist)", "remote", remoteName, "error", err)
		}

		if err := s.pushWithRetry(gitRepo, remoteName, branch, targetToken, target.name, pr); err != nil {
			slog.Error("push failed after retries", "target", target.name, "error", err)
			if err := s.recordSync(pr, branch, target.name, "failed", fmt.Sprintf("push to %s failed: %v", target.name, err)); err != nil {
				slog.Error("recordSync failed", "error", err)
			}
			failedTargets = append(failedTargets, target.name)
			continue
		}

		if err := s.recordSync(pr, branch, target.name, "success", ""); err != nil {
			slog.Error("recordSync failed", "error", err)
		}
		slog.Info("sync completed", "target", target.name)
	}

	if len(failedTargets) > 0 {
		failMsg := fmt.Sprintf("sync partially failed: %d/%d targets failed (%s)",
			len(failedTargets), len(targetPlatforms), strings.Join(failedTargets, ", "))
		s.notifySyncFailure(pr, strings.Join(failedTargets, ","), failMsg)
		events.PublishPR(events.EventSyncFailed, pr.RepoGroup, pr.Platform, pr, failMsg)
		return fmt.Errorf("%s", failMsg)
	}
	return nil
}
