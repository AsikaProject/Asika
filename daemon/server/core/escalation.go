package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
	"urgent": true, "high-priority": true,
}

var criticalPaths = []string{
	"src/core/", "src/security/", "cmd/", "internal/core/",
}

// EscalationLevel defines who gets notified at each level.
type EscalationLevel struct {
	Level    int
	Target   string        // "reviewer" | "team" | "tech_lead"
	Threshold time.Duration
}

var escalationLevels = map[string][]EscalationLevel{
	PriorityCritical: {
		{Level: 1, Target: "reviewer", Threshold: 1 * time.Hour},
		{Level: 2, Target: "team", Threshold: 2 * time.Hour},
		{Level: 3, Target: "tech_lead", Threshold: 4 * time.Hour},
	},
	PriorityUrgent: {
		{Level: 1, Target: "reviewer", Threshold: 4 * time.Hour},
		{Level: 2, Target: "team", Threshold: 8 * time.Hour},
		{Level: 3, Target: "tech_lead", Threshold: 12 * time.Hour},
	},
	PriorityNormal: {
		{Level: 1, Target: "reviewer", Threshold: 24 * time.Hour},
		{Level: 2, Target: "team", Threshold: 48 * time.Hour},
	},
}

// CalculatePRPriority determines the priority of a PR based on labels, author, and file paths.
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
	for _, f := range pr.DiffFiles {
		for _, p := range criticalPaths {
			if strings.HasPrefix(f, p) {
				return PriorityUrgent
			}
		}
	}
	return PriorityNormal
}

// EscalationWorker periodically checks for PRs that need escalation.
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
	levels, ok := escalationLevels[priority]
	if !ok || len(levels) == 0 {
		return
	}

	openFor := now.Sub(pr.CreatedAt)
	escKey := fmt.Sprintf("escalation:%s", pr.ID)

	var currentLevel int
	escData, err := db.Get(db.BucketEscalationRules, escKey)
	if err == nil && escData != nil {
		json.Unmarshal(escData, &currentLevel)
	}

	var triggered *EscalationLevel
	for i := range levels {
		if openFor >= levels[i].Threshold && currentLevel < levels[i].Level {
			triggered = &levels[i]
		}
	}

	if triggered == nil {
		return
	}

	w.notify(pr, priority, triggered, openFor)
	currentLevel = triggered.Level
	levelData, _ := json.Marshal(currentLevel)
	db.Put(db.BucketEscalationRules, escKey, levelData)

	slog.Info("PR escalated",
		"pr_id", pr.ID,
		"priority", priority,
		"level", triggered.Level,
		"target", triggered.Target,
		"open_for", openFor.Round(time.Hour))
}

func (w *EscalationWorker) notify(pr *models.PRRecord, priority string, level *EscalationLevel, openFor time.Duration) {
	title, body := formatEscalationMessage(pr, priority, level, openFor)

	switch level.Target {
	case "reviewer":
		SendNotification(context.Background(), title, body)
	case "team":
		SendNotification(context.Background(), title, body)
	case "tech_lead":
		SendNotification(context.Background(), title, body)
	}
}

func formatEscalationMessage(pr *models.PRRecord, priority string, level *EscalationLevel, openFor time.Duration) (string, string) {
	openForStr := openFor.Round(time.Hour).String()

	switch level.Target {
	case "reviewer":
		return fmt.Sprintf("📋 PR #%d needs your review", pr.PRNumber),
			fmt.Sprintf("PR '%s' by %s in %s is waiting for review.\nOpen for: %s\nPriority: %s\nURL: %s",
				pr.Title, pr.Author, pr.RepoGroup, openForStr, priority, pr.HTMLURL)
	case "team":
		return fmt.Sprintf("⏰ PR #%d awaiting review (%s)", pr.PRNumber, priority),
			fmt.Sprintf("PR '%s' by %s in %s has been open for %s.\nPriority: %s\nURL: %s",
				pr.Title, pr.Author, pr.RepoGroup, openForStr, priority, pr.HTMLURL)
	case "tech_lead":
		return fmt.Sprintf("🚨 CRITICAL: PR #%d needs immediate attention", pr.PRNumber),
			fmt.Sprintf("PR '%s' by %s in %s has been open for %s without review.\nPriority: %s\nEscalation Level: %d\nURL: %s",
				pr.Title, pr.Author, pr.RepoGroup, openForStr, priority, level.Level, pr.HTMLURL)
	default:
		return fmt.Sprintf("PR #%d escalation", pr.PRNumber),
			fmt.Sprintf("PR '%s' by %s in %s has been open for %s.",
				pr.Title, pr.Author, pr.RepoGroup, openForStr)
	}
}
