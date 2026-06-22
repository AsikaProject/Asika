package workflow

import (
	"testing"
)

func TestEvaluateSimpleConditions(t *testing.T) {
	ctx := &PRContext{
		CIPassed:     true,
		HasConflicts: false,
		IsApproved:   true,
		IsDraft:      false,
		Labels:       []string{"bug", "urgent"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		{"ci_passed", true},
		{"ci_failed", false},
		{"has_conflicts", false},
		{"approved", true},
		{"draft", false},
		{"!draft", true},
		{"ci_passed && approved", true},
		{"ci_passed && has_conflicts", false},
		{"ci_passed || has_conflicts", true},
		{"!ci_passed && approved", false},
	}

	for _, tt := range tests {
		result, err := evaluateCondition(tt.condition, ctx)
		if err != nil {
			t.Errorf("condition '%s' failed: %v", tt.condition, err)
			continue
		}
		if result != tt.expected {
			t.Errorf("condition '%s': expected %v, got %v", tt.condition, tt.expected, result)
		}
	}
}

func TestEvaluateHasLabel(t *testing.T) {
	ctx := &PRContext{
		Labels: []string{"bug", "ready-to-merge"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		{`has_label("bug")`, true},
		{`has_label("urgent")`, false},
		{`has_label("ready-to-merge")`, true},
	}

	for _, tt := range tests {
		result, err := evaluateCondition(tt.condition, ctx)
		if err != nil {
			t.Errorf("condition '%s' failed: %v", tt.condition, err)
			continue
		}
		if result != tt.expected {
			t.Errorf("condition '%s': expected %v, got %v", tt.condition, tt.expected, result)
		}
	}
}
