package queue

import (
	"log/slog"
	"time"

	"asika/common/db"
	"asika/common/models"
)

const pendingCheckInterval = 1 * time.Minute

type PendingQueue struct {
	manager *Manager
}

func NewPendingQueue(manager *Manager) *PendingQueue {
	return &PendingQueue{manager: manager}
}

func (pq *PendingQueue) AddPending(pr *models.PRRecord, approvalCount int) {
	pending := &db.PendingPR{
		PRID:          pr.ID,
		RepoGroup:     pr.RepoGroup,
		Platform:      pr.Platform,
		PRNumber:      pr.PRNumber,
		Title:         pr.Title,
		Author:        pr.Author,
		ApprovalCount: approvalCount,
		AddedAt:       time.Now(),
		LastChecked:   time.Now(),
	}
	if err := db.PutPendingPR(pending); err != nil {
		slog.Error("failed to add pending PR", "error", err, "pr_id", pr.ID)
		return
	}
	slog.Info("PR added to pending queue", "pr_id", pr.ID, "approvals", approvalCount)
}

func (pq *PendingQueue) CheckAll() {
	pendingPRs, err := db.ListPendingPRs()
	if err != nil {
		slog.Error("failed to list pending PRs", "error", err)
		return
	}
	if len(pendingPRs) == 0 {
		return
	}
	slog.Info("checking pending PRs", "count", len(pendingPRs))
	for _, pending := range pendingPRs {
		pq.checkOne(pending)
	}
}

func (pq *PendingQueue) checkOne(pending *db.PendingPR) {
	pr, err := getPRFromDB(pending.RepoGroup, pending.PRID)
	if err != nil {
		slog.Warn("pending PR not found in DB, removing", "pr_id", pending.PRID)
		db.DeletePendingPR(pending.RepoGroup, pending.Platform, pending.PRNumber)
		return
	}
	if pr.State != "open" || pr.IsDraft || pr.SpamFlag {
		slog.Info("pending PR no longer open, removing", "pr_id", pending.PRID, "state", pr.State)
		db.DeletePendingPR(pending.RepoGroup, pending.Platform, pending.PRNumber)
		return
	}
	ready, err := pq.manager.IsReadyToMerge(pr)
	if err != nil {
		slog.Error("failed to check pending PR readiness", "error", err, "pr_id", pending.PRID)
		pending.LastChecked = time.Now()
		db.PutPendingPR(pending)
		return
	}
	if !ready {
		slog.Debug("pending PR not ready yet", "pr_id", pending.PRID)
		pending.LastChecked = time.Now()
		db.PutPendingPR(pending)
		return
	}
	slog.Info("pending PR is ready, promoting to merge queue", "pr_id", pending.PRID)
	if err := pq.manager.AddToQueue(pr); err != nil {
		slog.Error("failed to promote pending PR to merge queue", "error", err, "pr_id", pending.PRID)
		return
	}
	db.DeletePendingPR(pending.RepoGroup, pending.Platform, pending.PRNumber)
}

func (pq *PendingQueue) Start() {
	go func() {
		ticker := time.NewTicker(pendingCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pq.CheckAll()
			case <-pq.manager.StopChan():
				slog.Info("pending queue worker stopped")
				return
			}
		}
	}()
	slog.Info("pending queue worker started", "interval", pendingCheckInterval)
}
