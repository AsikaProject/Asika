package core

import (
	"testing"
	"time"

	"asika/common/models"
)

func TestCalculatePRPriority(t *testing.T) {
	tests := []struct {
		name     string
		labels   []string
		files    []string
		expected string
	}{
		{"no labels", nil, nil, PriorityNormal},
		{"bug label", []string{"bug"}, nil, PriorityNormal},
		{"urgent label", []string{"urgent"}, nil, PriorityUrgent},
		{"high-priority label", []string{"high-priority"}, nil, PriorityUrgent},
		{"critical label", []string{"critical"}, nil, PriorityCritical},
		{"security label", []string{"security"}, nil, PriorityCritical},
		{"breaking-change label", []string{"breaking-change"}, nil, PriorityCritical},
		{"hotfix label", []string{"hotfix"}, nil, PriorityCritical},
		{"urgent + bug", []string{"bug", "urgent"}, nil, PriorityUrgent},
		{"critical + urgent", []string{"urgent", "critical"}, nil, PriorityCritical},
		{"critical file path", nil, []string{"src/core/handler.go"}, PriorityUrgent},
		{"security file path", nil, []string{"src/security/auth.go"}, PriorityUrgent},
		{"normal file path", nil, []string{"src/api/handler.go"}, PriorityNormal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &models.PRRecord{Labels: tt.labels, DiffFiles: tt.files}
			got := CalculatePRPriority(pr)
			if got != tt.expected {
				t.Errorf("CalculatePRPriority(labels=%v, files=%v) = %q, want %q",
					tt.labels, tt.files, got, tt.expected)
			}
		})
	}
}

func TestEscalationLevels(t *testing.T) {
	criticalLevels := escalationLevels[PriorityCritical]
	if len(criticalLevels) != 3 {
		t.Fatalf("Critical levels = %d, want 3", len(criticalLevels))
	}
	if criticalLevels[0].Target != "reviewer" {
		t.Errorf("Critical level 0 target = %q, want reviewer", criticalLevels[0].Target)
	}
	if criticalLevels[1].Target != "team" {
		t.Errorf("Critical level 1 target = %q, want team", criticalLevels[1].Target)
	}
	if criticalLevels[2].Target != "tech_lead" {
		t.Errorf("Critical level 2 target = %q, want tech_lead", criticalLevels[2].Target)
	}

	urgentLevels := escalationLevels[PriorityUrgent]
	if len(urgentLevels) != 3 {
		t.Fatalf("Urgent levels = %d, want 3", len(urgentLevels))
	}

	normalLevels := escalationLevels[PriorityNormal]
	if len(normalLevels) != 2 {
		t.Fatalf("Normal levels = %d, want 2", len(normalLevels))
	}
}

func TestEscalationThresholds(t *testing.T) {
	if escalationThresholds[PriorityCritical] != 4*time.Hour {
		t.Errorf("Critical threshold = %v, want 4h", escalationThresholds[PriorityCritical])
	}
	if escalationThresholds[PriorityUrgent] != 12*time.Hour {
		t.Errorf("Urgent threshold = %v, want 12h", escalationThresholds[PriorityUrgent])
	}
	if escalationThresholds[PriorityNormal] != 48*time.Hour {
		t.Errorf("Normal threshold = %v, want 48h", escalationThresholds[PriorityNormal])
	}
}

func TestFormatEscalationMessage(t *testing.T) {
	pr := &models.PRRecord{
		PRNumber:  42,
		Title:     "Test PR",
		Author:    "alice",
		RepoGroup: "backend",
		HTMLURL:   "https://github.com/org/repo/pull/42",
	}

	tests := []struct {
		level    string
		contains string
	}{
		{"reviewer", "needs your review"},
		{"team", "awaiting review"},
		{"tech_lead", "CRITICAL"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var target string
			switch tt.level {
			case "reviewer":
				target = "reviewer"
			case "team":
				target = "team"
			case "tech_lead":
				target = "tech_lead"
			}
			level := &EscalationLevel{Level: 1, Target: target}
			title, body := formatEscalationMessage(pr, PriorityNormal, level, 2*time.Hour)
			if title == "" {
				t.Error("Empty title")
			}
			if body == "" {
				t.Error("Empty body")
			}
		})
	}
}
