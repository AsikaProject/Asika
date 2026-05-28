package db

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPutNotificationPrefs_And_GetNotificationPrefs(t *testing.T) {
	initTestDB(t)

	prefs := map[string]interface{}{
		"username": "alice",
		"enabled":  true,
	}
	data, _ := json.Marshal(prefs)

	err := PutNotificationPrefs("alice", data)
	if err != nil {
		t.Fatalf("PutNotificationPrefs failed: %v", err)
	}

	got, err := GetNotificationPrefs("alice")
	if err != nil {
		t.Fatalf("GetNotificationPrefs failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("GetNotificationPrefs = %q, want %q", string(got), string(data))
	}
}

func TestGetNotificationPrefs_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetNotificationPrefs("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPutNotificationDedup_And_GetNotificationDedup(t *testing.T) {
	initTestDB(t)

	key := "dedup-key-1"
	data := []byte("dedup-value")

	err := PutNotificationDedup(key, data)
	if err != nil {
		t.Fatalf("PutNotificationDedup failed: %v", err)
	}

	got, err := GetNotificationDedup(key)
	if err != nil {
		t.Fatalf("GetNotificationDedup failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("GetNotificationDedup = %q, want %q", string(got), string(data))
	}
}

func TestDeleteNotificationDedup(t *testing.T) {
	initTestDB(t)

	key := "dedup-to-delete"
	PutNotificationDedup(key, []byte("value"))

	err := DeleteNotificationDedup(key)
	if err != nil {
		t.Fatalf("DeleteNotificationDedup failed: %v", err)
	}

	_, err = GetNotificationDedup(key)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestAppendNotificationDigest_And_List(t *testing.T) {
	initTestDB(t)

	err := AppendNotificationDigest("alice", "smtp", "Test Title", "Test Body")
	if err != nil {
		t.Fatalf("AppendNotificationDigest failed: %v", err)
	}

	digests, err := ListNotificationDigests()
	if err != nil {
		t.Fatalf("ListNotificationDigests failed: %v", err)
	}
	if len(digests["alice"]) != 1 {
		t.Errorf("expected 1 digest for alice, got %d", len(digests["alice"]))
	}
	if digests["alice"][0].Title != "Test Title" {
		t.Errorf("expected title 'Test Title', got %q", digests["alice"][0].Title)
	}
}

func TestAppendNotificationDigest(t *testing.T) {
	initTestDB(t)

	err := AppendNotificationDigest("alice", "smtp", "PR Opened", "New PR #123")
	if err != nil {
		t.Fatalf("AppendNotificationDigest failed: %v", err)
	}

	digests, err := ListNotificationDigests()
	if err != nil {
		t.Fatalf("ListNotificationDigests failed: %v", err)
	}
	if len(digests["alice"]) != 1 {
		t.Errorf("expected 1 digest for alice, got %d", len(digests["alice"]))
	}
	if digests["alice"][0].Title != "PR Opened" {
		t.Errorf("expected title 'PR Opened', got %q", digests["alice"][0].Title)
	}
}

func TestListNotificationDigests_MultipleUsers(t *testing.T) {
	initTestDB(t)

	AppendNotificationDigest("alice", "smtp", "Title 1", "Body 1")
	AppendNotificationDigest("bob", "telegram", "Title 2", "Body 2")
	AppendNotificationDigest("alice", "smtp", "Title 3", "Body 3")

	digests, err := ListNotificationDigests()
	if err != nil {
		t.Fatalf("ListNotificationDigests failed: %v", err)
	}
	if len(digests["alice"]) != 2 {
		t.Errorf("expected 2 digests for alice, got %d", len(digests["alice"]))
	}
	if len(digests["bob"]) != 1 {
		t.Errorf("expected 1 digest for bob, got %d", len(digests["bob"]))
	}
}

func TestDeleteNotificationDigests(t *testing.T) {
	initTestDB(t)

	AppendNotificationDigest("alice", "smtp", "Title 1", "Body 1")
	AppendNotificationDigest("alice", "smtp", "Title 2", "Body 2")
	AppendNotificationDigest("bob", "telegram", "Title 3", "Body 3")

	err := DeleteNotificationDigests("alice")
	if err != nil {
		t.Fatalf("DeleteNotificationDigests failed: %v", err)
	}

	digests, err := ListNotificationDigests()
	if err != nil {
		t.Fatalf("ListNotificationDigests failed: %v", err)
	}
	if len(digests["alice"]) != 0 {
		t.Errorf("expected 0 digests for alice after delete, got %d", len(digests["alice"]))
	}
	if len(digests["bob"]) != 1 {
		t.Errorf("expected 1 digest for bob (unchanged), got %d", len(digests["bob"]))
	}
}

func TestPutPendingPR_And_GetPendingPR(t *testing.T) {
	initTestDB(t)

	pr := &PendingPR{
		PRID:          "pr-123",
		RepoGroup:     "frontend",
		Platform:      "github",
		PRNumber:      123,
		Title:         "Test PR",
		Author:        "alice",
		ApprovalCount: 2,
		AddedAt:       time.Now(),
		LastChecked:   time.Now(),
	}

	err := PutPendingPR(pr)
	if err != nil {
		t.Fatalf("PutPendingPR failed: %v", err)
	}

	got, err := GetPendingPR("frontend", "github", 123)
	if err != nil {
		t.Fatalf("GetPendingPR failed: %v", err)
	}
	if got.PRID != pr.PRID {
		t.Errorf("PRID mismatch: got %q, want %q", got.PRID, pr.PRID)
	}
	if got.Title != pr.Title {
		t.Errorf("Title mismatch: got %q, want %q", got.Title, pr.Title)
	}
	if got.ApprovalCount != pr.ApprovalCount {
		t.Errorf("ApprovalCount mismatch: got %d, want %d", got.ApprovalCount, pr.ApprovalCount)
	}
}

func TestGetPendingPR_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetPendingPR("nonexistent", "github", 999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeletePendingPR(t *testing.T) {
	initTestDB(t)

	pr := &PendingPR{
		PRID:      "pr-to-delete",
		RepoGroup: "backend",
		Platform:  "gitlab",
		PRNumber:  456,
		Title:     "Delete Me",
	}
	PutPendingPR(pr)

	err := DeletePendingPR("backend", "gitlab", 456)
	if err != nil {
		t.Fatalf("DeletePendingPR failed: %v", err)
	}

	_, err = GetPendingPR("backend", "gitlab", 456)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListPendingPRs(t *testing.T) {
	initTestDB(t)

	prs := []*PendingPR{
		{PRID: "pr-1", RepoGroup: "frontend", Platform: "github", PRNumber: 1, Title: "PR 1"},
		{PRID: "pr-2", RepoGroup: "backend", Platform: "gitlab", PRNumber: 2, Title: "PR 2"},
		{PRID: "pr-3", RepoGroup: "frontend", Platform: "github", PRNumber: 3, Title: "PR 3"},
	}

	for _, pr := range prs {
		PutPendingPR(pr)
	}

	got, err := ListPendingPRs()
	if err != nil {
		t.Fatalf("ListPendingPRs failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 pending PRs, got %d", len(got))
	}
}

func TestListPendingPRs_Empty(t *testing.T) {
	initTestDB(t)

	got, err := ListPendingPRs()
	if err != nil {
		t.Fatalf("ListPendingPRs failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 pending PRs, got %d", len(got))
	}
}
