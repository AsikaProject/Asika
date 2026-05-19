package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/gitutil"
	"asika/common/models"
	handlerspr "asika/daemon/handlers/pr"
)

// AutoRebaseWorker periodically checks for PRs that need rebasing
type AutoRebaseWorker struct {
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

// NewAutoRebaseWorker creates a new auto-rebase worker
func NewAutoRebaseWorker() *AutoRebaseWorker {
	return &AutoRebaseWorker{
		stopCh: make(chan struct{}),
	}
}

// Start starts the auto-rebase worker
func (w *AutoRebaseWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go w.run()
	slog.Info("auto-rebase worker started")
}

// Stop stops the auto-rebase worker
func (w *AutoRebaseWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	close(w.stopCh)
	slog.Info("auto-rebase worker stopped")
}

func (w *AutoRebaseWorker) run() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.checkAndRebase()
		}
	}
}

func (w *AutoRebaseWorker) checkAndRebase() {
	cfg := config.Current()
	if cfg == nil || !cfg.AutoRebase.Enabled {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Scan all open PRs
	var prs []models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.State == "open" {
			prs = append(prs, pr)
		}
		return nil
	})

	for _, pr := range prs {
		// Check if PR should be excluded
		if w.shouldExclude(pr, cfg) {
			continue
		}

		// Check if PR has conflicts
		if !pr.HasConflict {
			continue
		}

		// Perform rebase
		if err := w.rebasePR(ctx, &pr, cfg); err != nil {
			slog.Error("auto-rebase failed", "error", err, "pr_id", pr.ID, "repo_group", pr.RepoGroup)
		}
	}
}

func (w *AutoRebaseWorker) shouldExclude(pr models.PRRecord, cfg *models.Config) bool {
	// Check exclude labels
	for _, label := range cfg.AutoRebase.ExcludeLabels {
		for _, prLabel := range pr.Labels {
			if label == prLabel {
				return true
			}
		}
	}

	// Check exclude authors
	for _, author := range cfg.AutoRebase.ExcludeAuthors {
		if pr.Author == author {
			return true
		}
	}

	return false
}

func (w *AutoRebaseWorker) rebasePR(ctx context.Context, pr *models.PRRecord, cfg *models.Config) error {
	group := config.GetRepoGroupByName(cfg, pr.RepoGroup)
	if group == nil {
		return fmt.Errorf("repo group not found: %s", pr.RepoGroup)
	}

	platform := pr.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client := handlerspr.GetClientForGroup(group, platform)
	if client == nil {
		return fmt.Errorf("platform client not available: %s", platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return fmt.Errorf("cannot resolve repo for platform %s", platform)
	}

	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get branch info: %w", err)
	}

	if !branchInfo.MaintainerCanModify {
		return fmt.Errorf("rebase not allowed: PR author has not enabled 'allow edits from maintainers'")
	}

	pr.BranchInfo = branchInfo
	prKey := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	prData, err := json.Marshal(pr)
	if err != nil {
		return fmt.Errorf("failed to marshal PR: %w", err)
	}
	if err := db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber); err != nil {
		slog.Error("failed to save PR before rebase", "error", err, "pr_id", pr.ID)
	}

	cloneURL := config.GetCloneURL(platform, owner, repo)
	clonePath := cfg.Git.RepoClonePath

	rebaseErr := performRebaseForAutoRebase(ctx, cloneURL, config.GetToken(cfg, platform), branchInfo, clonePath)
	if rebaseErr != nil {
		pr.Events = append(pr.Events, models.PREvent{
			Action: "auto_rebase_failed",
			Detail: rebaseErr.Error(),
		})
		prData, _ := json.Marshal(pr)
		if prData != nil {
			db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
		}
		return fmt.Errorf("rebase failed: %w", rebaseErr)
	}

	pr.HasConflict = false
	pr.Events = append(pr.Events, models.PREvent{
		Action: "auto_rebased",
		Detail: fmt.Sprintf("Auto-rebased onto %s", branchInfo.BaseBranch),
	})
	prData, _ = json.Marshal(pr)
	if prData != nil {
		db.PutPRWithIndex(prKey, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	}

	db.AppendAuditLog("info", "PR auto-rebased", map[string]interface{}{
		"pr_id":       pr.ID,
		"repo_group":  pr.RepoGroup,
		"platform":    platform,
		"head_branch": branchInfo.HeadBranch,
		"base_branch": branchInfo.BaseBranch,
	})

	slog.Info("PR auto-rebased", "pr_id", pr.PRNumber, "repo_group", pr.RepoGroup, "platform", platform)
	return nil
}

func performRebaseForAutoRebase(ctx context.Context, cloneURL, token string, branchInfo *models.PRBranchInfo, clonePath string) error {
	return gitutil.Rebase("", cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
}

// TriggerAutoRebase triggers auto-rebase for a specific repo group
func TriggerAutoRebase(repoGroup string) {
	cfg := config.Current()
	if cfg == nil || !cfg.AutoRebase.Enabled {
		return
	}

	go func() {
		worker := NewAutoRebaseWorker()
		worker.checkAndRebase()
	}()
}
