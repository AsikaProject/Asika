package syncer

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "strings"
    "sync"
    "time"

    "asika/common/config"
    "asika/common/db"
    "asika/common/models"
    "asika/common/gitutil"
    "asika/common/platforms"
    "asika/daemon/hooks"

    "github.com/google/uuid"
)

// Syncer handles cross-platform synchronization
type Syncer struct {
    cfg       *models.Config
    clients   map[platforms.PlatformType]platforms.PlatformClient
    syncLocks sync.Map
}

// NewSyncer creates a new syncer
func NewSyncer(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Syncer {
    return &Syncer{
        cfg:     cfg,
        clients: clients,
    }
}

// SyncOnMerge handles a merge event and syncs to other platforms
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

    // Create temp workdir for this sync
	workdir, err := gitutil.CreateTempWorkdir("asika-sync-")
	if err != nil {
		return fmt.Errorf("failed to create workdir: %w", err)
	}
	defer gitutil.CleanupWorkdir(workdir)

	// Get source repo URL
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	sourceRepo := owner + "/" + repo
	if sourceRepo == "" {
		return fmt.Errorf("no repo configured for source platform %s", pr.Platform)
	}

	sourceURL := s.getRepoURL(pr.Platform, sourceRepo)
	sourceToken := s.getTokenForPlatform(pr.Platform)

	// Clone source repo
	gitRepo, err := gitutil.CloneOrOpen(workdir, sourceURL, sourceToken)
	if err != nil {
		return fmt.Errorf("failed to clone source repo: %w", err)
	}

	// Checkout default branch
	if err := gitutil.CheckoutBranch(gitRepo, group.DefaultBranch); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", group.DefaultBranch, err)
	}

	// Fetch latest
	if err := gitutil.FetchRemote(gitRepo, "origin", sourceToken); err != nil {
		slog.Warn("fetch failed, continuing", "error", err)
	}

	// Run pre-sync hooks
	if group.HookPath != "" {
		hookRunner := hooks.NewRunner(group.HookPath)
		if err := hookRunner.Run("pre-sync", workdir, "", pr.MergeCommitSHA, "refs/heads/"+group.DefaultBranch); err != nil {
			slog.Warn("pre-sync hook failed", "error", err)
		}
	}

	// Cherry-pick the merge commit
	if err := gitutil.CherryPick(gitRepo, pr.MergeCommitSHA); err != nil {
		slog.Error("cherry-pick failed", "commit", pr.MergeCommitSHA, "error", err)
		s.recordSync(pr, group.DefaultBranch, "", "failed", fmt.Sprintf("cherry-pick failed: %v", err))
		return fmt.Errorf("cherry-pick failed: %w", err)
	}

	// Sync to each target platform
	targetPlatforms := []struct {
		name string
		repo string
	}{
		{"github", group.GitHub},
		{"gitlab", group.GitLab},
		{"gitea", group.Gitea},
	}

	for _, target := range targetPlatforms {
		if target.name == pr.Platform || target.repo == "" {
			continue
		}

		slog.Info("syncing to platform", "target", target.name, "repo", target.repo)

		// Add target remote
		targetURL := s.getRepoURL(target.name, target.repo)
		targetToken := s.getTokenForPlatform(target.name)
		remoteName := "target-" + target.name

		if err := gitutil.AddRemote(gitRepo, remoteName, targetURL); err != nil {
			slog.Warn("add remote failed (may already exist)", "remote", remoteName, "error", err)
		}

		// Push to target platform
		if err := gitutil.Push(gitRepo, remoteName, group.DefaultBranch, targetToken); err != nil {
			slog.Error("push failed", "target", target.name, "error", err)
			s.recordSync(pr, group.DefaultBranch, target.name, "failed", fmt.Sprintf("push to %s failed: %v", target.name, err))
			continue
		}

		s.recordSync(pr, group.DefaultBranch, target.name, "success", "")
		slog.Info("sync completed", "target", target.name)
	}

	// Publish sync_completed event
	slog.Info("sync completed for all targets", "repo_group", pr.RepoGroup)
	return nil
}

// SyncBranchDeletion syncs branch deletion to other platforms
func (s *Syncer) SyncBranchDeletion(repoGroup, sourcePlatform, branch string) {
	group := config.GetRepoGroupByName(s.cfg, repoGroup)
	if group == nil || group.Mode != "multi" {
		return
	}

	ctx := context.Background()
	targetPlatforms := []struct {
		name string
		repo string
	}{
		{"github", group.GitHub},
		{"gitlab", group.GitLab},
		{"gitea", group.Gitea},
	}

	for _, target := range targetPlatforms {
		if target.name == sourcePlatform || target.repo == "" {
			continue
		}

		client := s.clients[platforms.PlatformType(target.name)]
		if client == nil {
			continue
		}

		owner, repo := config.GetOwnerRepoFromGroup(group, target.name)
		if owner == "" || repo == "" {
			continue
		}

		// Delete branch on target platform
		if err := client.DeleteBranch(ctx, owner, repo, branch); err != nil {
			slog.Error("failed to delete branch", "platform", target.name, "branch", branch, "error", err)
		} else {
			slog.Info("branch deleted", "platform", target.name, "branch", branch)
		}
	}
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

// getTokenForPlatform returns the configured token for a platform
func (s *Syncer) getTokenForPlatform(platform string) string {
	switch platforms.PlatformType(platform) {
	case platforms.PlatformGitHub:
		return s.cfg.Tokens.GitHub
	case platforms.PlatformGitLab:
		return s.cfg.Tokens.GitLab
	case platforms.PlatformGitea:
		return s.cfg.Tokens.Gitea
	case platforms.PlatformForgejo:
		return s.cfg.Tokens.Forgejo
	case platforms.PlatformCodeberg:
		return s.cfg.Tokens.Codeberg
	case platforms.PlatformBitbucket:
		return s.cfg.Tokens.Bitbucket
	}
	return ""
}

// getRepoURL returns the clone URL for a platform repo
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
    }
    return ""
}

func (s *Syncer) getOrCreateLock(repoGroup string) *sync.Mutex {
    actual, _ := s.syncLocks.LoadOrStore(repoGroup, &sync.Mutex{})
    return actual.(*sync.Mutex)
}