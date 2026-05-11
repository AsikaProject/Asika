package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/db"
	"asika/common/models"
)

const (
	PriorityNormal   = "normal"
	PriorityUrgent   = "urgent"
	PriorityCritical = "critical"
)

var escalationThresholds = map[string]time.Duration{
	PriorityCritical: 4 * time.Hour,
	PriorityUrgent:   12 * time.Hour,
	PriorityNormal:   48 * time.Hour,
}

var criticalLabels = map[string]bool{
	"critical": true, "security": true, "breaking-change": true, "hotfix": true,
}

var urgentLabels = map[string]bool{
	"urgent": true, "high-priority": true, "needs-review": true,
}

// CalculatePRPriority determines the priority of a PR based on its labels.
func CalculatePRPriority(pr *models.PRRecord) string {
	for _, lbl := range pr.Labels {
		if criticalLabels[lbl] {
			return PriorityCritical
		}
	}
	for _, lbl := range pr.Labels {
		if urgentLabels[lbl] {
			return PriorityUrgent
		}
	}
	return PriorityNormal
}

type EscalationWorker struct {
	stop chan struct{}
}

func NewEscalationWorker() *EscalationWorker {
	return &EscalationWorker{stop: make(chan struct{})}
}

func (w *EscalationWorker) Start() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		w.checkEscalations()
		for {
			select {
			case <-ticker.C:
				w.checkEscalations()
			case <-w.stop:
				slog.Info("escalation worker stopped")
				return
			}
		}
	}()
	slog.Info("escalation worker started")
}

func (w *EscalationWorker) Stop() {
	close(w.stop)
}

func (w *EscalationWorker) checkEscalations() {
	now := time.Now()
	var openPRs []models.PRRecord
	err := db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.State == "open" {
			openPRs = append(openPRs, pr)
		}
		return nil
	})
	if err != nil {
		slog.Error("escalation: failed to scan PRs", "error", err)
		return
	}

	for i := range openPRs {
		w.escalateIfNeeded(&openPRs[i], now)
	}
}

func (w *EscalationWorker) escalateIfNeeded(pr *models.PRRecord, now time.Time) {
	priority := CalculatePRPriority(pr)
	threshold, ok := escalationThresholds[priority]
	if !ok || threshold == 0 {
		return
	}

	if now.Sub(pr.CreatedAt) < threshold {
		return
	}

	escKey := fmt.Sprintf("escalation:%s", pr.ID)
	escData, err := db.Get(db.BucketEscalationRules, escKey)
	if err == nil && escData != nil {
		var lastEscalated time.Time
		if err := json.Unmarshal(escData, &lastEscalated); err == nil {
			if now.Sub(lastEscalated) < threshold {
				return
			}
		}
	}

	tsData, _ := json.Marshal(now)
	db.Put(db.BucketEscalationRules, escKey, tsData)

	title, body := w.formatNotification(pr, priority, now)
	SendNotification(context.Background(), title, body)

	slog.Info("PR escalated", "pr_id", pr.ID, "priority", priority, "open_for", now.Sub(pr.CreatedAt).Round(time.Hour))
}

func (w *EscalationWorker) formatNotification(pr *models.PRRecord, priority string, now time.Time) (string, body string) {
	openFor := now.Sub(pr.CreatedAt).Round(time.Hour)

	if priority == PriorityCritical {
		return fmt.Sprintf("🚨 CRITICAL: PR #%d needs immediate attention", pr.PRNumber),
			fmt.Sprintf("PR '%s' by %s in %s has been open for %s without review.\nPriority: %s\nURL: %s",
				pr.Title, pr.Author, pr.RepoGroup, openFor, priority, pr.HTMLURL)
	}

	return fmt.Sprintf("⏰ PR #%d awaiting review (%s)", pr.PRNumber, priority),
		fmt.Sprintf("PR '%s' by %s in %s has been open for %s.\nPriority: %s\nURL: %s",
			pr.Title, pr.Author, pr.RepoGroup, openFor, priority, pr.HTMLURL)
}
