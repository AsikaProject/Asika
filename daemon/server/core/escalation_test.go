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
		expected string
	}{
		{"no labels", nil, PriorityNormal},
		{"normal label", []string{"bug"}, PriorityNormal},
		{"urgent label", []string{"urgent"}, PriorityUrgent},
		{"high-priority label", []string{"high-priority"}, PriorityUrgent},
		{"critical label", []string{"critical"}, PriorityCritical},
		{"security label", []string{"security"}, PriorityCritical},
		{"breaking-change label", []string{"breaking-change"}, PriorityCritical},
		{"hotfix label", []string{"hotfix"}, PriorityCritical},
		{"urgent + bug", []string{"bug", "urgent"}, PriorityUrgent},
		{"critical + urgent", []string{"urgent", "critical"}, PriorityCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &models.PRRecord{Labels: tt.labels}
			got := CalculatePRPriority(pr)
			if got != tt.expected {
				t.Errorf("CalculatePRPriority(%v) = %q, want %q", tt.labels, got, tt.expected)
			}
		})
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
