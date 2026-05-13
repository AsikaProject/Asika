package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/gitutil"
	"asika/common/models"
	"asika/common/platforms"
)

// Manager manages the merge queue
type Manager struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
	checker *Checker
	stop    chan struct{}
}

// NewManager creates a new queue manager
func NewManager(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Manager {
	return &Manager{
		cfg:     cfg,
		clients: clients,
		checker: NewChecker(cfg, clients),
		stop:    make(chan struct{}),
	}
}

// Recover resets stale in-flight queue items from a previous run.
// Items left in "merging" or "checking" state indicate the daemon crashed
// mid-processing. They are reset to "waiting" so the next CheckQueue cycle
// will re-evaluate them. Before resetting, we verify the PR has not already
// been merged on the platform to avoid double-merges.
func (m *Manager) Recover() {
	var toReset []struct {
		key  string
		item models.QueueItem
	}
	err := db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if item.Status == "merging" || item.Status == "checking" {
			toReset = append(toReset, struct {
				key  string
				item models.QueueItem
			}{key: string(key), item: item})
		}
		return nil
	})
	if err != nil {
		slog.Error("queue recovery: failed to scan items", "error", err)
		return
	}

	for _, entry := range toReset {
		pr, findErr := FindPRByID(entry.item.PRID)
		if findErr == nil && pr != nil && pr.State == "merged" {
			slog.Info("queue recovery: PR already merged, removing from queue", "pr_id", entry.item.PRID)
			db.Delete(db.BucketQueueItems, entry.key)
			continue
		}
		entry.item.Status = "waiting"
		entry.item.FailureReason = ""
		data, _ := json.Marshal(entry.item)
		if putErr := db.Put(db.BucketQueueItems, entry.key, data); putErr != nil {
			slog.Error("queue recovery: failed to reset item", "pr_id", entry.item.PRID, "error", putErr)
		} else {
			slog.Info("queue recovery: reset stale item to waiting", "pr_id", entry.item.PRID, "old_status", "merging/checking")
		}
	}

	if len(toReset) > 0 {
		slog.Info("queue recovery complete", "reset_count", len(toReset))
	}
}

// AddToQueue adds a PR to the merge queue
func (m *Manager) AddToQueue(pr *models.PRRecord) error {
	return m.AddToQueueScheduled(pr, time.Time{})
}

// AddToQueueScheduled adds a PR to the merge queue with a scheduled merge time.
// If scheduleAt is zero, the PR is queued immediately.
func (m *Manager) AddToQueueScheduled(pr *models.PRRecord, scheduleAt time.Time) error {
	key := fmt.Sprintf("%s#%s", pr.RepoGroup, pr.ID)

	// Check if already in queue
	existing, err := db.Get(db.BucketQueueItems, key)
	if err == nil && existing != nil {
		slog.Info("PR already in queue", "pr_id", pr.ID)
		return nil
	}

	// Skip draft PRs
	if pr.IsDraft {
		slog.Info("skipping draft PR", "pr_id", pr.ID, "title", pr.Title)
		return nil
	}

	// Skip non-open PRs (merged, closed, etc.)
	if pr.State != "" && pr.State != "open" {
		slog.Info("skipping non-open PR", "pr_id", pr.ID, "title", pr.Title, "state", pr.State)
		return nil
	}

	// Get merge criteria from repo group config
	group := config.GetRepoGroupByName(m.cfg, pr.RepoGroup)
	criteria := models.MergeCriteria{
		RequiredApprovals: 1,
		CIStatus:          "pending",
	}
	if group != nil {
		criteria.RequiredApprovals = group.MergeQueue.RequiredApprovals
	}

	item := models.QueueItem{
		PRID:       pr.ID,
		RepoGroup:  pr.RepoGroup,
		Status:     "waiting",
		AddedAt:    time.Now(),
		Criteria:   criteria,
		ScheduleAt: scheduleAt,
	}

	data, err := json.Marshal(item)
	if err != nil {
		return err
	}

	slog.Info("PR added to merge queue", "pr_id", pr.ID, "repo_group", pr.RepoGroup)
	return db.Put(db.BucketQueueItems, key, data)
}

// CheckQueue checks all items in the queue
func (m *Manager) CheckQueue() {
	// First, read all items and collect done keys for cleanup
	var items []models.QueueItem
	var keys []string
	var doneKeys []string
	err := db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			slog.Warn("skipping corrupted queue item", "key", string(key), "error", err)
			return nil
		}
		// Collect completed items for cleanup
		if item.Status == "done" {
			doneKeys = append(doneKeys, string(key))
			return nil
		}
		// Process waiting, checking, and failed items (failed items can be retried)
		if item.Status != "waiting" && item.Status != "checking" && item.Status != "failed" {
			return nil
		}
		items = append(items, item)
		keys = append(keys, string(key))
		return nil
	})
	if err != nil {
		slog.Error("failed to read queue items", "error", err)
		return
	}

	// Clean up completed items outside the read transaction
	for _, dk := range doneKeys {
		slog.Info("removing completed item from queue", "pr_id", dk)
		if delErr := db.Delete(db.BucketQueueItems, dk); delErr != nil {
			slog.Error("failed to remove completed queue item", "error", delErr)
		}
	}

	// Process items outside any db transaction
	now := time.Now()
	for i, item := range items {
		if !item.ScheduleAt.IsZero() && item.ScheduleAt.After(now) {
			continue
		}

		item.Status = "checking"
		item.LastChecked = now

		shouldMerge, err := m.checker.ShouldMerge(&item)
		if err != nil {
			if isTransientError(err) {
				slog.Warn("transient check error, keeping as waiting", "error", err, "pr_id", item.PRID)
				item.Status = "waiting"
			} else {
				slog.Error("check failed", "error", err, "pr_id", item.PRID)
				item.Status = "failed"
				item.FailureReason = err.Error()
			}
			updated, _ := json.Marshal(item)
			if putErr := db.Put(db.BucketQueueItems, keys[i], updated); putErr != nil {
				slog.Error("failed to update queue item", "error", putErr, "pr_id", item.PRID)
			}
		} else if shouldMerge {
			item.Status = "merging"
			if err := m.merge(&item); err != nil {
				item.Status = "failed"
				item.FailureReason = err.Error()
				updated, _ := json.Marshal(item)
				if putErr := db.Put(db.BucketQueueItems, keys[i], updated); putErr != nil {
					slog.Error("failed to update queue item", "error", putErr, "pr_id", item.PRID)
				}
			} else {
				item.Status = "done"
				slog.Info("removing completed item from queue", "pr_id", item.PRID)
				if delErr := db.Delete(db.BucketQueueItems, keys[i]); delErr != nil {
					slog.Error("failed to remove completed queue item", "error", delErr, "pr_id", item.PRID)
				}
			}
		} else {
			item.Status = "waiting"
			updated, _ := json.Marshal(item)
			if putErr := db.Put(db.BucketQueueItems, keys[i], updated); putErr != nil {
				slog.Error("failed to update queue item", "error", putErr, "pr_id", item.PRID)
			}
		}
	}
}

// merge performs the merge operation
func (m *Manager) merge(item *models.QueueItem) error {
	ctx := context.Background()

	// Find PR in bbolt
	pr, err := FindPRByID(item.PRID)
	if err != nil {
		return err
	}

	// Get platform client
	client := m.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return fmt.Errorf("no client for platform: %s", pr.Platform)
	}

	// Get repo group config
	group := config.GetRepoGroupByName(m.cfg, pr.RepoGroup)
	if group == nil {
		return fmt.Errorf("repo group not found: %s", pr.RepoGroup)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return fmt.Errorf("cannot resolve repo for platform %s", pr.Platform)
	}

	// Determine merge method
	method, err := client.GetDefaultMergeMethod(ctx, owner, repo)
	if err != nil {
		slog.Warn("failed to get default merge method, using default", "error", err)
		method = "merge"
	}

	// Fast-forward only: auto-rebase before merge to ensure linear history
	if group.MergeQueue.FastForwardOnly {
		slog.Info("fast-forward only: auto-rebasing before merge", "pr_id", pr.ID, "pr_number", pr.PRNumber)
		if rebaseErr := m.tryRebaseBeforeMerge(ctx, pr, group); rebaseErr != nil {
			slog.Error("fast-forward auto-rebase failed", "pr_id", pr.ID, "error", rebaseErr)
			return fmt.Errorf("fast-forward rebase failed: %w", rebaseErr)
		}
	}

	slog.Info("merging PR", "pr_id", pr.ID, "pr_number", pr.PRNumber, "platform", pr.Platform, "method", method)
	err = client.MergePR(ctx, owner, repo, pr.PRNumber, method)
	if err != nil {
		slog.Error("merge failed", "pr_id", pr.ID, "error", err)
	} else {
		slog.Info("merge succeeded", "pr_id", pr.ID)
		// Fetch updated PR info from platform to get merge_commit_sha
		updated, getErr := client.GetPR(ctx, owner, repo, pr.PRNumber)
		if getErr == nil && updated != nil {
			pr.State = updated.State
			pr.MergeCommitSHA = updated.MergeCommitSHA
			pr.UpdatedAt = updated.UpdatedAt
		} else {
			pr.State = "merged"
			pr.UpdatedAt = time.Now()
		}
		key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
		data, _ := json.Marshal(pr)
		db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	}
	return err
}

// FindPRByID finds a PR by its ID in bbolt (exported for use by serial worker).
func FindPRByID(prID string) (*models.PRRecord, error) {
	data, err := db.GetPRByIndex(prID, "", 0)
	if err == nil && data != nil {
		var pr models.PRRecord
		if err := json.Unmarshal(data, &pr); err == nil {
			return &pr, nil
		}
	}

	var found *models.PRRecord
	_ = db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var record models.PRRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return nil
		}
		if record.ID == prID {
			found = &record
			return errStopEach
		}
		return nil
	})
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("PR not found: %s", prID)
}

var errStopEach = fmt.Errorf("stop")

// GetQueueItems returns all queue items for a repo group
func (m *Manager) GetQueueItems(repoGroup string) ([]models.QueueItem, error) {
	var items []models.QueueItem
	prefix := repoGroup + "#"
	err := db.BucketForEachPrefix(db.BucketQueueItems, prefix, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return err
		}
		items = append(items, item)
		return nil
	})
	return items, err
}

// RemoveFromQueue removes a single queue item by repo group and PR ID.
func (m *Manager) RemoveFromQueue(repoGroup, prID string) error {
	key := fmt.Sprintf("%s#%s", repoGroup, prID)
	data, err := db.Get(db.BucketQueueItems, key)
	if err != nil || data == nil {
		return fmt.Errorf("queue item not found: %s", key)
	}
	return db.Delete(db.BucketQueueItems, key)
}

// ClearQueue removes all queue items for a repo group.
func (m *Manager) ClearQueue(repoGroup string) (int, error) {
	var keys []string
	prefix := repoGroup + "#"
	err := db.BucketForEachPrefix(db.BucketQueueItems, prefix, func(key, value []byte) error {
		keys = append(keys, string(key))
		return nil
	})
	if err != nil {
		return 0, err
	}
	var deleted int
	for _, k := range keys {
		if delErr := db.Delete(db.BucketQueueItems, k); delErr != nil {
			slog.Error("failed to delete queue item", "key", k, "error", delErr)
		} else {
			deleted++
		}
	}
	slog.Info("queue cleared", "repo_group", repoGroup, "requested", len(keys), "deleted", deleted)
	return deleted, nil
}

// Stop signals the periodic checker goroutine to stop.
func (m *Manager) Stop() {
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
}

// StopChan returns the stop channel for external select loops.
func (m *Manager) StopChan() <-chan struct{} {
	return m.stop
}

// tryRebaseBeforeMerge auto-rebases a PR branch onto its base branch before merge.
// This ensures the merge will be a fast-forward, producing linear history.
func (m *Manager) tryRebaseBeforeMerge(ctx context.Context, pr *models.PRRecord, group *models.RepoGroup) error {
	platform := pr.Platform
	client := m.clients[platforms.PlatformType(platform)]
	if client == nil {
		return fmt.Errorf("no client for platform: %s", platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return fmt.Errorf("cannot resolve repo for platform %s", platform)
	}

	// Fetch branch info from platform API
	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get branch info: %w", err)
	}

	if !branchInfo.MaintainerCanModify {
		return fmt.Errorf("PR author has not enabled 'allow edits from maintainers'")
	}

	cloneURL := config.GetCloneURL(platform, owner, repo)
	token := config.GetToken(m.cfg, platform)
	clonePath := m.cfg.Git.RepoClonePath

	rebaseErr := gitutil.Rebase("", cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
	if rebaseErr != nil {
		return fmt.Errorf("rebase failed: %w", rebaseErr)
	}

	slog.Info("fast-forward auto-rebase succeeded", "pr_id", pr.ID, "head_branch", branchInfo.HeadBranch, "base_branch", branchInfo.BaseBranch)
	return nil
}
