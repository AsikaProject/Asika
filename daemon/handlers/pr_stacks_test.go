package handlers

import (
	"testing"

	"asika/common/models"
)

func TestDetectStackLinks(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int
	}{
		{"no links", "This is a PR description", 0},
		{"single github link", "Depends on https://github.com/org/repo/pull/42", 1},
		{"multiple links", "Related:\n- https://github.com/org/repo/pull/42\n- https://gitlab.com/org/repo/-/merge_requests/15", 2},
		{"self reference", "This is PR https://github.com/org/repo/pull/123", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &models.PRRecord{Body: tt.body, RepoGroup: "test", Platform: "github"}
			members := DetectStackLinks(pr)
			if len(members) != tt.expected {
				t.Errorf("DetectStackLinks returned %d members, want %d", len(members), tt.expected)
			}
		})
	}
}

func TestDetectPlatformFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://github.com/org/repo/pull/1", "github"},
		{"https://gitlab.com/org/repo/-/merge_requests/1", "gitlab"},
		{"https://bitbucket.org/org/repo/pull-requests/1", "bitbucket"},
		{"https://codeberg.org/org/repo/pulls/1", "codeberg"},
		{"https://gitea.example.com/org/repo/pulls/1", "gitea"},
		{"https://unknown.example.com/pull/1", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := detectPlatformFromURL(tt.url)
			if got != tt.expected {
				t.Errorf("detectPlatformFromURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestCalculateStackState(t *testing.T) {
	tests := []struct {
		name     string
		members  []models.StackMember
		expected string
	}{
		{"empty", nil, "open"},
		{"all open", []models.StackMember{{State: "open"}, {State: "open"}}, "partial"},
		{"all merged", []models.StackMember{{State: "merged"}, {State: "merged"}}, "merged"},
		{"partial merged", []models.StackMember{{State: "merged"}, {State: "open"}}, "partial"},
		{"any failed", []models.StackMember{{State: "merged"}, {State: "failed"}}, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := &models.PRStack{Members: tt.members}
			got := calculateStackState(stack)
			if got != tt.expected {
				t.Errorf("calculateStackState = %q, want %q", got, tt.expected)
			}
		})
	}
}
