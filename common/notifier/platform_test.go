package notifier

import (
	"context"
	"testing"

	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func TestNewGitHubAtNotifier(t *testing.T) {
	n := NewGitHubAtNotifier(map[string]interface{}{
		"to":    []interface{}{"user1", "user2"},
		"owner": "myorg",
		"repo":  "myrepo",
	})

	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if n.Type() != "github_at" {
		t.Errorf("Type() = %q, want github_at", n.Type())
	}
	if len(n.to) != 2 {
		t.Errorf("to = %d entries, want 2", len(n.to))
	}
	if n.owner != "myorg" {
		t.Errorf("owner = %q, want myorg", n.owner)
	}
	if n.repo != "myrepo" {
		t.Errorf("repo = %q, want myrepo", n.repo)
	}
}

func TestNewGitLabAtNotifier(t *testing.T) {
	n := NewGitLabAtNotifier(map[string]interface{}{
		"to":    []interface{}{"dev1"},
		"owner": "group",
		"repo":  "project",
	})

	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if n.Type() != "gitlab_at" {
		t.Errorf("Type() = %q, want gitlab_at", n.Type())
	}
}

func TestNewGiteaAtNotifier(t *testing.T) {
	n := NewGiteaAtNotifier(map[string]interface{}{
		"to":    []interface{}{"maintainer"},
		"owner": "user",
		"repo":  "repo",
	})

	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if n.Type() != "gitea_at" {
		t.Errorf("Type() = %q, want gitea_at", n.Type())
	}
}

func TestNewGitHubAtNotifier_EmptyConfig(t *testing.T) {
	n := NewGitHubAtNotifier(map[string]interface{}{})
	if n == nil {
		t.Fatal("expected non-nil notifier even with empty config")
	}
	if len(n.to) != 0 {
		t.Errorf("expected empty to list, got %d", len(n.to))
	}
}

func TestPlatformNotifier_SetClient(t *testing.T) {
	n := NewGitHubAtNotifier(map[string]interface{}{
		"to": []interface{}{"user1"},
	})

	if n.client != nil {
		t.Error("client should be nil initially")
	}

	mock := testutil.NewMockPlatformClient()
	n.SetClient(mock)

	if n.client == nil {
		t.Error("client should be set")
	}
}

func TestPlatformNotifier_Send_NoClient(t *testing.T) {
	n := &PlatformNotifier{
		platform: "github",
		to:       []string{"user1"},
		owner:    "org",
		repo:     "repo",
	}

	err := n.Send(context.Background(), "Test", "Body")
	if err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestPlatformNotifier_Send_NoUsers(t *testing.T) {
	n := &PlatformNotifier{
		platform: "github",
		to:       []string{},
		owner:    "org",
		repo:     "repo",
		client:   testutil.NewMockPlatformClient(),
	}

	err := n.Send(context.Background(), "Test", "Body")
	if err == nil {
		t.Error("expected error when no users configured")
	}
}

func TestPlatformNotifier_Send_WithOpenPRs(t *testing.T) {
	mock := testutil.NewMockPlatformClient()
	mock.PRs["org/repo#1"] = &models.PRRecord{
		ID:       "1",
		PRNumber: 1,
		State:    "open",
	}

	n := &PlatformNotifier{
		platform: "github",
		to:       []string{"user1"},
		owner:    "org",
		repo:     "repo",
		client:   mock,
	}

	err := n.Send(context.Background(), "Test Title", "Test Body")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPlatformNotifier_Send_NoOpenPRs(t *testing.T) {
	mock := testutil.NewMockPlatformClient()
	// No PRs added — no open PRs

	n := &PlatformNotifier{
		platform: "github",
		to:       []string{"user1"},
		owner:    "org",
		repo:     "repo",
		client:   mock,
	}

	err := n.Send(context.Background(), "Test", "Body")
	if err != nil {
		t.Errorf("unexpected error with no open PRs: %v", err)
	}
}

func TestWirePlatformNotifiers(t *testing.T) {
	mockGH := testutil.NewMockPlatformClient()
	mockGL := testutil.NewMockPlatformClient()

	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mockGH,
		platforms.PlatformGitLab: mockGL,
	}

	ghNotifier := NewGitHubAtNotifier(map[string]interface{}{
		"to": []interface{}{"user1"},
	})
	glNotifier := NewGitLabAtNotifier(map[string]interface{}{
		"to": []interface{}{"user2"},
	})

	notifiers := []Notifier{ghNotifier, glNotifier}

	WirePlatformNotifiers(notifiers, clients)

	if ghNotifier.client == nil {
		t.Error("GitHub notifier should have client wired")
	}
	if glNotifier.client == nil {
		t.Error("GitLab notifier should have client wired")
	}
}

func TestWirePlatformNotifiers_NoMatchingClient(t *testing.T) {
	// Only GitHub client available
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	glNotifier := NewGitLabAtNotifier(map[string]interface{}{
		"to": []interface{}{"user1"},
	})

	notifiers := []Notifier{glNotifier}

	WirePlatformNotifiers(notifiers, clients)

	if glNotifier.client != nil {
		t.Error("GitLab notifier should not have client when not in map")
	}
}

func TestWirePlatformNotifiers_NonPlatformNotifier(t *testing.T) {
	// A non-PlatformNotifier should be skipped without panic
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	// Create a mock notifier that is not *PlatformNotifier
	mockNotifier := &mockNotifierType{}
	notifiers := []Notifier{mockNotifier}

	// Should not panic
	WirePlatformNotifiers(notifiers, clients)
}

func TestNewGitHubAtNotifier_MissingFields(t *testing.T) {
	n := NewGitHubAtNotifier(map[string]interface{}{
		"to": []interface{}{"user1"},
	})
	if n.owner != "" {
		t.Errorf("expected empty owner, got %q", n.owner)
	}
	if n.repo != "" {
		t.Errorf("expected empty repo, got %q", n.repo)
	}
}

func TestPlatformNotifier_Send_MultipleUsers(t *testing.T) {
	mock := testutil.NewMockPlatformClient()
	mock.PRs["org/repo#1"] = &models.PRRecord{
		ID:       "1",
		PRNumber: 1,
		State:    "open",
	}

	n := &PlatformNotifier{
		platform: "github",
		to:       []string{"user1", "user2", "user3"},
		owner:    "org",
		repo:     "repo",
		client:   mock,
	}

	err := n.Send(context.Background(), "Test", "Body")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPlatformNotifier_Send_AllPRsClosed(t *testing.T) {
	mock := testutil.NewMockPlatformClient()
	mock.PRs["org/repo#1"] = &models.PRRecord{
		ID:       "1",
		PRNumber: 1,
		State:    "closed",
	}

	n := &PlatformNotifier{
		platform: "github",
		to:       []string{"user1"},
		owner:    "org",
		repo:     "repo",
		client:   mock,
	}

	err := n.Send(context.Background(), "Test", "Body")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWirePlatformNotifiers_EmptyLists(t *testing.T) {
	clients := map[platforms.PlatformType]platforms.PlatformClient{}
	notifiers := []Notifier{}

	// Should not panic with empty lists
	WirePlatformNotifiers(notifiers, clients)
}

func TestWirePlatformNotifiers_MixedTypes(t *testing.T) {
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	ghNotifier := NewGitHubAtNotifier(map[string]interface{}{
		"to": []interface{}{"user1"},
	})

	notifiers := []Notifier{ghNotifier, &mockNotifierType{}}

	// Should not panic with mixed types
	WirePlatformNotifiers(notifiers, clients)

	if ghNotifier.client == nil {
		t.Error("GitHub notifier should have client wired")
	}
}

// mockNotifierType is a minimal Notifier implementation for testing
type mockNotifierType struct{}

func (m *mockNotifierType) Type() string { return "mock" }
func (m *mockNotifierType) Send(ctx context.Context, title, body string) error {
	return nil
}
