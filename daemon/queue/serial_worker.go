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

// SerialWorker handles serial merge validation: rebase → CI re-run → merge.
type SerialWorker struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
	stop    chan struct{}
}

func NewSerialWorker(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *SerialWorker {
	return &SerialWorker{
		cfg:     cfg,
		clients: clients,
		stop:    make(chan struct{}),
	}
}

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

func (w *SerialWorker) Stop() {
	if w.stop != nil {
		close(w.stop)
		w.stop = nil
	}
}

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

	for i := range items {
		w.processOne(&items[i], keys[i])
	}
}

func (w *SerialWorker) processOne(item *models.QueueItem, key string) {
	switch item.ValidationStatus {
	case "validating":
		w.startRebase(item, key)
	case "rebasing":
		w.checkRebaseStatus(item, key)
	case "waiting_ci":
		w.waitForCI(item, key)
	case "ci_running":
		w.pollCIStatus(item, key)
	case "ready":
		w.markMergeable(item, key)
	case "validation_failed":
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

	rebaseErr := gitutil.RebaseAndPush(clonePath, cloneURL, token, branchInfo.HeadBranch, branchInfo.BaseBranch)
	if rebaseErr != nil {
		w.fail(item, key, fmt.Sprintf("rebase+push failed: %v", rebaseErr))
		return
	}

	item.ValidationStatus = "waiting_ci"
	item.ValidationDetail = "rebased and pushed, waiting for CI"
	w.updateItem(item, key)
	slog.Info("serial: rebase succeeded, waiting for CI", "pr_id", item.PRID, "branch", branchInfo.HeadBranch)
}

func (w *SerialWorker) checkRebaseStatus(item *models.QueueItem, key string) {
	if time.Since(item.ValidationStarted) > 10*time.Minute {
		w.fail(item, key, "rebase timed out after 10 minutes")
	}
}

func (w *SerialWorker) waitForCI(item *models.QueueItem, key string) {
	pr, err := FindPRByID(item.PRID)
	if err != nil {
		w.fail(item, key, fmt.Sprintf("PR not found: %v", err))
		return
	}

	ciStatus, err := w.getCIStatus(pr)
	if err != nil {
		slog.Warn("serial: failed to get CI status, retrying", "pr_id", item.PRID, "error", err)
		return
	}

	switch {
	case ciStatus == "pending" || ciStatus == "running":
		item.ValidationStatus = "ci_running"
		item.ValidationDetail = "CI running"
		w.updateItem(item, key)
	case ciStatus == "success":
		item.ValidationStatus = "ready"
		item.ValidationDetail = "CI passed, ready to merge"
		w.updateItem(item, key)
		slog.Info("serial: CI passed, marked ready", "pr_id", item.PRID)
	case ciStatus == "failure" || ciStatus == "error":
		w.fail(item, key, "CI failed")
	default:
		item.ValidationStatus = "ready"
		item.ValidationDetail = fmt.Sprintf("CI status: %s", ciStatus)
		w.updateItem(item, key)
	}
}

func (w *SerialWorker) pollCIStatus(item *models.QueueItem, key string) {
	pr, err := FindPRByID(item.PRID)
	if err != nil {
		w.fail(item, key, fmt.Sprintf("PR not found: %v", err))
		return
	}

	ciStatus, err := w.getCIStatus(pr)
	if err != nil {
		return
	}

	switch {
	case ciStatus == "success":
		item.ValidationStatus = "ready"
		item.ValidationDetail = "CI passed, ready to merge"
		w.updateItem(item, key)
		slog.Info("serial: CI passed", "pr_id", item.PRID)
	case ciStatus == "failure" || ciStatus == "error":
		w.fail(item, key, "CI failed")
	default:
		if time.Since(item.ValidationStarted) > 30*time.Minute {
			w.fail(item, key, "CI timed out after 30 minutes")
		}
	}
}

func (w *SerialWorker) getCIStatus(pr *models.PRRecord) (string, error) {
	group := config.GetRepoGroupByName(w.cfg, pr.RepoGroup)
	if group == nil {
		return "", fmt.Errorf("repo group not found")
	}

	client := w.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		return "", fmt.Errorf("no client for platform: %s", pr.Platform)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	commits, err := client.GetPRCommits(context.Background(), owner, repo, pr.PRNumber)
	if err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("no commits found")
	}

	lastCommit := commits[len(commits)-1]
	return client.GetCIStatus(context.Background(), owner, repo, lastCommit)
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
