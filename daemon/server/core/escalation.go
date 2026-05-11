package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

// EscalationRule defines when and how to escalate PR notifications.
type EscalationRule struct {
	Name         string        `json:"name"`
	Priority     string        `json:"priority"`     // "normal"|"urgent"|"critical"
	Labels       []string      `json:"labels"`       // match any of these labels
	Authors      []string      `json:"authors"`      // match any of these authors
	FilePatterns []string      `json:"file_patterns"` // match any of these file patterns
	EscalateAfter string       `json:"escalate_after"` // e.g. "24h", "48h"
	EscalateTo   string        `json:"escalate_to"`   // "tech_lead"|"team"|"admin"
}

// PRPriority represents the calculated priority of a PR.
type PRPriority struct {
	PRID           string    `json:"pr_id"`
	RepoGroup      string    `json:"repo_group"`
	Priority       string    `json:"priority"`        // "normal"|"urgent"|"critical"
	AssignedAt     time.Time `json:"assigned_at"`
	LastEscalated  time.Time `json:"last_escalated,omitempty"`
	EscalationLevel int      `json:"escalation_level"` // 0=none, 1=reviewer, 2=team, 3=tech_lead
}

const (
	PriorityNormal   = "normal"
	PriorityUrgent   = "urgent"
	PriorityCritical = "critical"
)

// CalculatePRPriority determines the priority of a PR based on rules.
func CalculatePRPriority(pr *models.PRRecord) *PRPriority {
	p := &PRPriority{
		PRID:       pr.ID,
		RepoGroup:  pr.RepoGroup,
		Priority:   PriorityNormal,
		AssignedAt: pr.CreatedAt,
	}

	rules := loadEscalationRules()
	for _, rule := range rules {
		if matchesRule(pr, rule) {
			switch rule.Priority {
			case PriorityCritical:
				p.Priority = PriorityCritical
				return p
			case PriorityUrgent:
				if p.Priority == PriorityNormal {
					p.Priority = PriorityUrgent
				}
			}
		}
	}

	if len(pr.Labels) > 0 {
		for _, lbl := range pr.Labels {
			switch lbl {
			case "critical", "security", "breaking-change", "hotfix":
				p.Priority = PriorityCritical
				return p
			case "urgent", "high-priority", "needs-review":
				if p.Priority == PriorityNormal {
					p.Priority = PriorityUrgent
				}
			}
		}
	}

	return p
}

func matchesRule(pr *models.PRRecord, rule EscalationRule) bool {
	for _, l := range rule.Labels {
		for _, pl := range pr.Labels {
			if l == pl {
				return true
			}
		}
	}
	for _, a := range rule.Authors {
		if a == pr.Author {
			return true
		}
	}
	return false
}

func loadEscalationRules() []EscalationRule {
	data, err := db.Get(db.BucketEscalationRules, "default")
	if err != nil || data == nil {
		return defaultEscalationRules()
	}
	var rules []EscalationRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return defaultEscalationRules()
	}
	return rules
}

func defaultEscalationRules() []EscalationRule {
	return []EscalationRule{
		{
			Name:           "critical-labels",
			Priority:       PriorityCritical,
			Labels:         []string{"critical", "security", "breaking-change", "hotfix"},
			EscalateAfter:  "4h",
			EscalateTo:     "tech_lead",
		},
		{
			Name:           "urgent-labels",
			Priority:       PriorityUrgent,
			Labels:         []string{"urgent", "high-priority"},
			EscalateAfter:  "12h",
			EscalateTo:     "team",
		},
		{
			Name:           "normal-default",
			Priority:       PriorityNormal,
			EscalateAfter:  "48h",
			EscalateTo:     "team",
		},
	}
}

// EscalationWorker periodically checks for PRs that need escalation.
type EscalationWorker struct {
	stop chan struct{}
}

// NewEscalationWorker creates a new escalation worker.
func NewEscalationWorker() *EscalationWorker {
	return &EscalationWorker{stop: make(chan struct{})}
}

// Start begins the escalation check loop.
func (w *EscalationWorker) Start() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
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

// Stop signals the worker to stop.
func (w *EscalationWorker) Stop() {
	close(w.stop)
}

func (w *EscalationWorker) checkEscalations() {
	var openPRs []models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.State == "open" {
			openPRs = append(openPRs, pr)
		}
		return nil
	})

	now := time.Now()
	for _, pr := range openPRs {
		priority := CalculatePRPriority(&pr)
		w.escalateIfNeeded(&pr, priority, now)
	}
}

func (w *EscalationWorker) escalateIfNeeded(pr *models.PRRecord, priority *PRPriority, now time.Time) {
	var threshold time.Duration
	var target string

	switch priority.Priority {
	case PriorityCritical:
		threshold = 4 * time.Hour
		target = "tech_lead"
	case PriorityUrgent:
		threshold = 12 * time.Hour
		target = "team"
	case PriorityNormal:
		threshold = 48 * time.Hour
		target = "team"
	default:
		return
	}

	if now.Sub(pr.CreatedAt) < threshold {
		return
	}

	if !priority.LastEscalated.IsZero() && now.Sub(priority.LastEscalated) < threshold {
		return
	}

	if priority.EscalationLevel >= 3 {
		return
	}

	priority.LastEscalated = now
	priority.EscalationLevel++

	switch target {
	case "tech_lead":
		w.notifyTechLead(pr, priority)
	case "team":
		w.notifyTeam(pr, priority)
	}

	slog.Info("PR escalated",
		"pr_id", pr.ID,
		"priority", priority.Priority,
		"level", priority.EscalationLevel,
		"target", target)
}

func (w *EscalationWorker) notifyTechLead(pr *models.PRRecord, priority *PRPriority) {
	cfg := config.Current()
	if cfg == nil {
		return
	}

	title := fmt.Sprintf("🚨 Escalation: PR #%d needs immediate attention", pr.PRNumber)
	body := fmt.Sprintf(
		"PR '%s' by %s in %s has been open for %s without review.\nPriority: %s\nEscalation Level: %d\nURL: %s",
		pr.Title, pr.Author, pr.RepoGroup,
		time.Since(pr.CreatedAt).Round(time.Hour),
		priority.Priority, priority.EscalationLevel,
		pr.HTMLURL,
	)

	SendNotification(context.Background(), title, body)
}

func (w *EscalationWorker) notifyTeam(pr *models.PRRecord, priority *PRPriority) {
	title := fmt.Sprintf("⏰ PR #%d awaiting review for %s", pr.PRNumber, time.Since(pr.CreatedAt).Round(time.Hour))
	body := fmt.Sprintf(
		"PR '%s' by %s in %s needs review.\nPriority: %s\nURL: %s",
		pr.Title, pr.Author, pr.RepoGroup,
		priority.Priority, pr.HTMLURL,
	)

	SendNotification(context.Background(), title, body)
}
