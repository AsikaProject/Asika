package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/gitutil"
	"asika/common/models"
	"asika/common/platforms"
)

// SerialWorker handles serial merge validation: rebase → CI re-run → merge.
// Ensures each PR in the queue is validated against the latest main before merging.
type SerialWorker struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
	stop    chan struct{}
}

// NewSerialWorker creates a new serial validation worker.
func NewSerialWorker(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *SerialWorker {
	return &SerialWorker{
		cfg:     cfg,
		clients: clients,
		stop:    make(chan struct{}),
	}
}

// Start begins the serial validation loop.
func (w *SerialWorker) Start() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.process()
			case <-w.stop:
				slog.Info("serial worker stopped")
				return
			}
		}
	}()
	slog.Info("serial validation worker started")
}

// Stop signals the worker to stop.
func (w *SerialWorker) Stop() {
	close(w.stop)
}

// Enqueue adds a PR to the serial validation queue.
func (w *SerialWorker) Enqueue(item *models.QueueItem) error {
	item.ValidationStatus = "validating"
	item.ValidationStarted = time.Now()
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s#%s", item.RepoGroup, item.PRID)
	return db.Put(db.BucketSerialQueue, key, data)
}

func (w *SerialWorker) process() {
	var items []models.QueueItem
	var keys []string
	err := db.ForEach(db.BucketSerialQueue, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		items = append(items, item)
		keys = append(keys, string(key))
		return nil
	})
	if err != nil {
		slog.Error("serial worker: failed to read queue", "error", err)
		return
	}

	for i, item := range items {
		w.processOne(&item, keys[i])
	}
}

func (w *SerialWorker) processOne(item *models.QueueItem, key string) {
	switch item.ValidationStatus {
	case "validating":
		w.startRebase(item, key)
	case "rebooting":
		w.checkRebaseStatus(item, key)
	case "waiting_ci":
		w.waitForCI(item, key)
	case "ci_running":
		w.pollCIStatus(item, key)
	case "ready":
		w.markMergeable(item, key)
	case "validation_failed":
		slog.Warn("serial validation failed", "pr_id", item.PRID, "reason", item.ValidationDetail)
	default:
		slog.Warn("unknown validation status", "pr_id", item.PRID, "status", item.ValidationStatus)
	}
}

func (w *SerialWorker) startRebase(item *models.QueueItem, key string) {
	pr, err := FindPRByID(item.PRID)
	if err != nil {
		w.fail(item, key, fmt.Sprintf("PR not found: %v", err))
		return
	}

	group := config.GetRepoGroupByName(w.cfg, pr.RepoGroup)
	if group == nil {
		w.fail(item, key, "repo group not found")
		return
	}

	client := w.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		w.fail(item, key, fmt.Sprintf("no client for platform: %s", pr.Platform))
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	branchInfo, err := client.GetPRBranchInfo(context.Background(), owner, repo, pr.PRNumber)
	if err != nil {
		w.fail(item, key, fmt.Sprintf("failed to get branch info: %v", err))
		return
	}

	if !branchInfo.MaintainerCanModify {
		w.fail(item, key, "PR author has not enabled 'allow edits from maintainers'")
		return
	}

	cloneURL := config.GetCloneURL(pr.Platform, owner, repo)
	token := config.GetToken(w.cfg, pr.Platform)
	clonePath := w.cfg.Git.RepoClonePath

	rebaseErr := gitutil.Rebase("", cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch, clonePath)
	if rebaseErr != nil {
		w.fail(item, key, fmt.Sprintf("rebase failed: %v", rebaseErr))
		return
	}

	item.ValidationStatus = "waiting_ci"
	item.ValidationDetail = "rebased, waiting for CI"
	w.updateItem(item, key)
	slog.Info("serial: rebase succeeded, waiting for CI", "pr_id", item.PRID)
}

func (w *SerialWorker) checkRebaseStatus(item *models.QueueItem, key string) {
	if time.Since(item.ValidationStarted) > 10*time.Minute {
		w.fail(item, key, "rebase timed out after 10 minutes")
		return
	}
}

func (w *SerialWorker) waitForCI(item *models.QueueItem, key string) {
	pr, err := FindPRByID(item.PRID)
	if err != nil {
		w.fail(item, key, fmt.Sprintf("PR not found: %v", err))
		return
	}

	group := config.GetRepoGroupByName(w.cfg, pr.RepoGroup)
	if group == nil {
		w.fail(item, key, "repo group not found")
		return
	}

	client := w.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		w.fail(item, key, "no client")
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	commits, err := client.GetPRCommits(context.Background(), owner, repo, pr.PRNumber)
	if err != nil {
		slog.Warn("serial: failed to get commits, retrying", "pr_id", item.PRID, "error", err)
		return
	}
	if len(commits) == 0 {
		return
	}

	lastCommit := commits[len(commits)-1]
	ciStatus, err := client.GetCIStatus(context.Background(), owner, repo, lastCommit)
	if err != nil {
		slog.Warn("serial: failed to get CI status, retrying", "pr_id", item.PRID, "error", err)
		return
	}

	if ciStatus == "pending" || ciStatus == "running" {
		item.ValidationStatus = "ci_running"
		item.ValidationDetail = fmt.Sprintf("CI running for commit %s", lastCommit[:8])
		w.updateItem(item, key)
		return
	}

	if ciStatus == "success" {
		item.ValidationStatus = "ready"
		item.ValidationDetail = "CI passed, ready to merge"
		w.updateItem(item, key)
		events.PublishPR(events.EventSyncCompleted, pr.RepoGroup, pr.Platform, pr, nil)
		slog.Info("serial: CI passed, marked ready", "pr_id", item.PRID)
		return
	}

	if ciStatus == "failure" || ciStatus == "error" {
		w.fail(item, key, fmt.Sprintf("CI failed for commit %s", lastCommit[:8]))
		return
	}

	slog.Info("serial: CI status unknown, treating as ready", "pr_id", item.PRID, "status", ciStatus)
	item.ValidationStatus = "ready"
	item.ValidationDetail = fmt.Sprintf("CI status: %s", ciStatus)
	w.updateItem(item, key)
}

func (w *SerialWorker) pollCIStatus(item *models.QueueItem, key string) {
	pr, err := FindPRByID(item.PRID)
	if err != nil {
		w.fail(item, key, fmt.Sprintf("PR not found: %v", err))
		return
	}

	group := config.GetRepoGroupByName(w.cfg, pr.RepoGroup)
	if group == nil {
		w.fail(item, key, "repo group not found")
		return
	}

	client := w.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		w.fail(item, key, "no client")
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	commits, err := client.GetPRCommits(context.Background(), owner, repo, pr.PRNumber)
	if err != nil {
		return
	}
	if len(commits) == 0 {
		return
	}

	lastCommit := commits[len(commits)-1]
	ciStatus, err := client.GetCIStatus(context.Background(), owner, repo, lastCommit)
	if err != nil {
		return
	}

	if ciStatus == "success" {
		item.ValidationStatus = "ready"
		item.ValidationDetail = "CI passed, ready to merge"
		w.updateItem(item, key)
		slog.Info("serial: CI passed", "pr_id", item.PRID)
		return
	}

	if ciStatus == "failure" || ciStatus == "error" {
		w.fail(item, key, fmt.Sprintf("CI failed for commit %s", lastCommit[:8]))
		return
	}

	if time.Since(item.ValidationStarted) > 30*time.Minute {
		w.fail(item, key, "CI timed out after 30 minutes")
	}
}

func (w *SerialWorker) markMergeable(item *models.QueueItem, key string) {
	item.Status = "ready"
	item.ValidationDetail = "serial validation complete, ready for merge"
	w.updateItem(item, key)
	slog.Info("serial: PR marked ready for merge", "pr_id", item.PRID)
}

func (w *SerialWorker) fail(item *models.QueueItem, key, reason string) {
	item.ValidationStatus = "validation_failed"
	item.ValidationDetail = reason
	item.FailureReason = reason
	w.updateItem(item, key)
	slog.Warn("serial: validation failed", "pr_id", item.PRID, "reason", reason)
}

func (w *SerialWorker) updateItem(item *models.QueueItem, key string) {
	data, _ := json.Marshal(item)
	db.Put(db.BucketSerialQueue, key, data)
}
