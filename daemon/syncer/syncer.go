package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/uuid"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/gitutil"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/hooks"
)

const (
	syncMaxRetries     = 3
	syncRetryBaseDelay = 2 * time.Second
)

// Syncer handles cross-platform synchronization
type Syncer struct {
	cfg       *models.Config
	clients   map[platforms.PlatformType]platforms.PlatformClient
	syncLocks sync.Map
	notifyFn  func(title, body string)
}

// NewSyncer creates a new syncer
func NewSyncer(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Syncer {
	return &Syncer{
		cfg:     cfg,
		clients: clients,
	}
}

// SetNotifyFunc sets the notification function for sync conflict alerts.
func (s *Syncer) SetNotifyFunc(fn func(title, body string)) {
	s.notifyFn = fn
}

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

	slog.Info("sync completed for all targets", "repo_group", pr.RepoGroup)
	events.PublishPR(events.EventSyncCompleted, pr.RepoGroup, pr.Platform, pr, nil)
	return nil
}

// prepareRepoDir returns the repo directory path and whether it needs cleanup.
// When using a bare cache, the repo is persistent and should not be cleaned up.
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

	sourceURL := s.getRepoURL(pr.Platform, sourceRepo)
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
			if err := gitutil.AddRemote(gitRepo, remoteName, s.getRepoURL(target.name, target.repo)); err != nil {
				slog.Warn("add remote failed", "remote", remoteName, "error", err)
			}
			targetToken := config.GetToken(s.cfg, target.name)
			if err := s.pushRef(gitRepo, remoteName, "refs/heads/"+branch, targetToken, target.name); err != nil {
				slog.Error("failed to push branch", "branch", branch, "target", target.name, "error", err)
				s.recordSync(pr, branch, target.name, "failed", err.Error())
				branchFailed = true
				continue
			}
			s.recordSync(pr, branch, target.name, "success", "")
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
			if err := gitutil.AddRemote(gitRepo, remoteName, s.getRepoURL(target.name, target.repo)); err != nil {
				slog.Warn("add remote failed", "remote", remoteName, "error", err)
			}
			targetToken := config.GetToken(s.cfg, target.name)
			if err := s.pushRef(gitRepo, remoteName, "refs/tags/"+tag, targetToken, target.name); err != nil {
				slog.Error("failed to push tag", "tag", tag, "target", target.name, "error", err)
				s.recordSync(pr, "tag:"+tag, target.name, "failed", err.Error())
				tagFailed = true
				continue
			}
			s.recordSync(pr, "tag:"+tag, target.name, "success", "")
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

		targetURL := s.getRepoURL(target.name, target.repo)
		targetToken := config.GetToken(s.cfg, target.name)
		remoteName := "target-" + target.name

		if err := gitutil.AddRemote(gitRepo, remoteName, targetURL); err != nil {
			slog.Warn("add remote failed (may already exist)", "remote", remoteName, "error", err)
		}

		if err := s.pushWithRetry(gitRepo, remoteName, branch, targetToken, target.name, pr); err != nil {
			slog.Error("push failed after retries", "target", target.name, "error", err)
			s.recordSync(pr, branch, target.name, "failed", fmt.Sprintf("push to %s failed: %v", target.name, err))
			failedTargets = append(failedTargets, target.name)
			continue
		}

		s.recordSync(pr, branch, target.name, "success", "")
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

// pushRef pushes a specific ref (branch or tag) to a target remote with retry.
func (s *Syncer) pushRef(gitRepo *git.Repository, remoteName, refSpec, token, targetName string) error {
	var lastErr error
	for attempt := 0; attempt < syncMaxRetries; attempt++ {
		if attempt > 0 {
			delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			time.Sleep(delay)
		}
		opts := &git.PushOptions{
			RefSpecs: []gogitconfig.RefSpec{gogitconfig.RefSpec(refSpec)},
			Force:    true,
		}
		if token != "" {
			opts.Auth = &http.BasicAuth{Username: "git", Password: token}
		}
		if err := gitRepo.Push(opts); err != nil {
			lastErr = err
			if isTransientError(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("push %s to %s failed after %d retries: %w", refSpec, targetName, syncMaxRetries, lastErr)
}

// cherryPickWithRetry attempts cherry-pick with exponential backoff on conflict.
func (s *Syncer) cherryPickWithRetry(gitRepo *git.Repository, commitSHA string, pr *models.PRRecord, branch, sourceToken string) error {
	var lastErr error
	for attempt := 0; attempt < syncMaxRetries; attempt++ {
		if attempt > 0 {
			delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			slog.Info("retrying cherry-pick", "attempt", attempt+1, "max", syncMaxRetries, "delay", delay, "commit", commitSHA)
			time.Sleep(delay)
			if err := gitutil.FetchRemote(gitRepo, "origin", sourceToken); err != nil {
				slog.Warn("fetch before retry failed", "error", err)
			}
		}
		if err := gitutil.CherryPick(gitRepo, commitSHA); err != nil {
			lastErr = err
			if isConflictError(err) {
				slog.Warn("cherry-pick conflict detected, will retry", "attempt", attempt+1, "commit", commitSHA)
				continue
			}
			return err
		}
		return nil
	}
	slog.Error("cherry-pick failed after all retries", "commit", commitSHA, "attempts", syncMaxRetries)
	return fmt.Errorf("cherry-pick failed after %d retries: %w", syncMaxRetries, lastErr)
}

// pushWithRetry attempts push with exponential backoff on transient errors.
// Uses force push to ensure target platform state matches source.
func (s *Syncer) pushWithRetry(gitRepo *git.Repository, remoteName, branch, token, targetName string, pr *models.PRRecord) error {
	var lastErr error
	for attempt := 0; attempt < syncMaxRetries; attempt++ {
		if attempt > 0 {
			delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			slog.Info("retrying push", "attempt", attempt+1, "max", syncMaxRetries, "delay", delay, "target", targetName)
			time.Sleep(delay)
		}
		if err := gitutil.Push(gitRepo, remoteName, branch, token); err != nil {
			lastErr = err
			if isTransientError(err) {
				slog.Warn("transient push error, retrying", "attempt", attempt+1, "target", targetName, "error", err)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("push to %s failed after %d retries: %w", targetName, syncMaxRetries, lastErr)
}

// getTargetPlatforms returns all configured target platforms for sync.
func (s *Syncer) getTargetPlatforms(group *models.RepoGroup, sourcePlatform string) []struct {
	name string
	repo string
} {
	targets := []struct {
		name string
		repo string
	}{
		{"github", group.GitHub},
		{"gitlab", group.GitLab},
		{"gitea", group.Gitea},
		{"forgejo", group.Forgejo},
		{"codeberg", group.Codeberg},
		{"bitbucket", group.Bitbucket},
		{"gerrit", group.Gerrit},
	}
	result := make([]struct {
		name string
		repo string
	}, 0, len(targets))
	for _, t := range targets {
		if t.name != sourcePlatform && t.repo != "" {
			result = append(result, t)
		}
	}
	return result
}

// SyncBranchDeletion syncs branch deletion to all configured target platforms with retry.
func (s *Syncer) SyncBranchDeletion(repoGroup, sourcePlatform, branch string) {
	group := config.GetRepoGroupByName(s.cfg, repoGroup)
	if group == nil || group.Mode != "multi" {
		return
	}

	ctx := context.Background()
	targets := s.getTargetPlatforms(group, sourcePlatform)

	for _, target := range targets {
		client := s.clients[platforms.PlatformType(target.name)]
		if client == nil {
			continue
		}

		owner, repo := config.GetOwnerRepoFromGroup(group, target.name)
		if owner == "" || repo == "" {
			continue
		}

		var lastErr error
		for attempt := 0; attempt < syncMaxRetries; attempt++ {
			if attempt > 0 {
				delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
				time.Sleep(delay)
			}
			if err := client.DeleteBranch(ctx, owner, repo, branch); err != nil {
				lastErr = err
				if isTransientError(err) {
					slog.Warn("transient error deleting branch, retrying",
						"platform", target.name, "branch", branch, "attempt", attempt+1)
					continue
				}
				break
			}
			slog.Info("branch deleted", "platform", target.name, "branch", branch)
			return
		}
		if lastErr != nil {
			slog.Error("failed to delete branch", "platform", target.name, "branch", branch, "error", lastErr)
		}
	}
}

// notifySyncFailure sends a notification about sync failure.
func (s *Syncer) notifySyncFailure(pr *models.PRRecord, targetPlatforms, reason string) {
	if s.notifyFn == nil {
		return
	}
	title := fmt.Sprintf("⚠️ Sync Failed: %s#%d", pr.RepoGroup, pr.PRNumber)
	body := fmt.Sprintf("PR: %s\nSource: %s\nTarget: %s\nReason: %s", pr.Title, pr.Platform, targetPlatforms, reason)
	s.notifyFn(title, body)
}

// recordSync records sync history in bbolt
func (s *Syncer) recordSync(pr *models.PRRecord, branch, targetPlatform, status, errorMsg string) {
	record := models.SyncRecord{
		ID:             uuid.New().String(),
		PRID:           pr.ID,
		RepoGroup:      pr.RepoGroup,
		SourcePlatform: pr.Platform,
		TargetPlatform: targetPlatform,
		Branch:         branch,
		CommitSHA:      pr.MergeCommitSHA,
		Status:         status,
		ErrorMessage:   errorMsg,
		Timestamp:      time.Now(),
	}

	data, _ := json.Marshal(record)
	db.Put(db.BucketSyncHistory, record.ID, data)
}

// getRepoURL returns the clone URL (with .git suffix) for a platform repo.
func (s *Syncer) getRepoURL(platform, repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return ""
	}

	switch platforms.PlatformType(platform) {
	case platforms.PlatformGitHub:
		return fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1])
	case platforms.PlatformGitLab:
		base := s.cfg.GitLabBaseURL
		if base == "" {
			base = "https://gitlab.com"
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s/%s.git", base, parts[0], parts[1])
	case platforms.PlatformGitea:
		base := s.cfg.GiteaBaseURL
		if base == "" {
			base = "https://gitea.example.com"
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s/%s.git", base, parts[0], parts[1])
	case platforms.PlatformForgejo:
		base := s.cfg.ForgejoBaseURL
		if base == "" {
			base = "https://forgejo.example.com"
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s/%s.git", base, parts[0], parts[1])
	case platforms.PlatformCodeberg:
		return fmt.Sprintf("https://codeberg.org/%s/%s.git", parts[0], parts[1])
	case platforms.PlatformBitbucket:
		return fmt.Sprintf("https://bitbucket.org/%s/%s.git", parts[0], parts[1])
	case platforms.PlatformGerrit:
		base := s.cfg.Tokens.Gerrit.URL
		if base == "" {
			return ""
		}
		base = strings.TrimSuffix(base, "/")
		return fmt.Sprintf("%s/%s", base, repo)
	}
	return ""
}

func (s *Syncer) getOrCreateLock(repoGroup string) *sync.Mutex {
	actual, _ := s.syncLocks.LoadOrStore(repoGroup, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// isConflictError checks if an error is a merge/push conflict.
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "conflict") ||
		strings.Contains(msg, "merge conflict") ||
		strings.Contains(msg, "cherry-pick conflict") ||
		strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "rejected") ||
		strings.Contains(msg, "failed to push")
}

// isTransientError checks if an error is likely temporary.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "no such host")
}
