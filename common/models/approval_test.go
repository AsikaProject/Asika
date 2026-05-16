package models

import (
	"testing"
)

func TestComputeApprovalStatus(t *testing.T) {
	tests := []struct {
		name        string
		approvers   []string
		blockers    []string
		wantState   ApprovalState
		wantMessage string
		wantLabel   string
	}{
		{
			name:        "no approvals",
			approvers:   nil,
			blockers:    nil,
			wantState:   ApprovalStatePending,
			wantMessage: "Needs two more approvals",
			wantLabel:   "lgtm/need 2",
		},
		{
			name:        "one approval",
			approvers:   []string{"alice"},
			blockers:    nil,
			wantState:   ApprovalStatePending,
			wantMessage: "Needs one more approval",
			wantLabel:   "lgtm/need 1",
		},
		{
			name:        "two approvals",
			approvers:   []string{"alice", "bob"},
			blockers:    nil,
			wantState:   ApprovalStateSuccess,
			wantMessage: "Approved by 2 people",
			wantLabel:   "lgtm/done",
		},
		{
			name:        "three approvals",
			approvers:   []string{"alice", "bob", "charlie"},
			blockers:    nil,
			wantState:   ApprovalStateSuccess,
			wantMessage: "Approved by 3 people",
			wantLabel:   "lgtm/done",
		},
		{
			name:        "one blocker",
			approvers:   []string{"alice"},
			blockers:    []string{"bob"},
			wantState:   ApprovalStateFailure,
			wantMessage: "Blocked by bob",
			wantLabel:   "lgtm/blocked",
		},
		{
			name:        "multiple blockers",
			approvers:   []string{"alice"},
			blockers:    []string{"bob", "charlie"},
			wantState:   ApprovalStateFailure,
			wantMessage: "Blocked by bob, charlie",
			wantLabel:   "lgtm/blocked",
		},
		{
			name:        "blocker overrides approval from same user",
			approvers:   nil,
			blockers:    []string{"alice"},
			wantState:   ApprovalStateFailure,
			wantMessage: "Blocked by alice",
			wantLabel:   "lgtm/blocked",
		},
		{
			name:        "empty approvers and blockers",
			approvers:   []string{},
			blockers:    []string{},
			wantState:   ApprovalStatePending,
			wantMessage: "Needs two more approvals",
			wantLabel:   "lgtm/need 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &ApprovalStatus{
				Approvers: tt.approvers,
				Blockers:  tt.blockers,
			}
			state, message, label := ComputeApprovalStatus(status)
			if state != tt.wantState {
				t.Errorf("state = %q, want %q", state, tt.wantState)
			}
			if message != tt.wantMessage {
				t.Errorf("message = %q, want %q", message, tt.wantMessage)
			}
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
		})
	}
}
