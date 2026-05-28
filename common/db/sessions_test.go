package db

import (
	"testing"
	"time"

	"asika/common/models"
)

func TestPutSession_And_GetSession(t *testing.T) {
	initTestDB(t)

	session := &models.Session{
		ID:          "session-1",
		Username:    "alice",
		TokenPrefix: "abc123",
		IssuedAt:    time.Now(),
		LastUsedAt:  time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		IPAddress:   "192.168.1.1",
		UserAgent:   "Mozilla/5.0",
	}

	err := PutSession(session)
	if err != nil {
		t.Fatalf("PutSession failed: %v", err)
	}

	got, err := GetSession("session-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.Username != session.Username {
		t.Errorf("Username mismatch: got %q, want %q", got.Username, session.Username)
	}
	if got.IPAddress != session.IPAddress {
		t.Errorf("IPAddress mismatch: got %q, want %q", got.IPAddress, session.IPAddress)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetSession("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	initTestDB(t)

	session := &models.Session{
		ID:       "session-to-delete",
		Username: "bob",
	}
	PutSession(session)

	err := DeleteSession("session-to-delete")
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	_, err = GetSession("session-to-delete")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListUserSessions(t *testing.T) {
	initTestDB(t)

	sessions := []*models.Session{
		{ID: "s1", Username: "alice"},
		{ID: "s2", Username: "alice"},
		{ID: "s3", Username: "bob"},
	}
	for _, s := range sessions {
		PutSession(s)
	}

	aliceSessions, err := ListUserSessions("alice")
	if err != nil {
		t.Fatalf("ListUserSessions failed: %v", err)
	}
	if len(aliceSessions) != 2 {
		t.Errorf("expected 2 sessions for alice, got %d", len(aliceSessions))
	}

	bobSessions, err := ListUserSessions("bob")
	if err != nil {
		t.Fatalf("ListUserSessions failed: %v", err)
	}
	if len(bobSessions) != 1 {
		t.Errorf("expected 1 session for bob, got %d", len(bobSessions))
	}
}

func TestListAllSessions(t *testing.T) {
	initTestDB(t)

	for i := 0; i < 5; i++ {
		PutSession(&models.Session{ID: string(rune('a' + i)), Username: "user"})
	}

	sessions, err := ListAllSessions()
	if err != nil {
		t.Fatalf("ListAllSessions failed: %v", err)
	}
	if len(sessions) != 5 {
		t.Errorf("expected 5 sessions, got %d", len(sessions))
	}
}

func TestDeleteUserSessions(t *testing.T) {
	initTestDB(t)

	PutSession(&models.Session{ID: "s1", Username: "alice"})
	PutSession(&models.Session{ID: "s2", Username: "alice"})
	PutSession(&models.Session{ID: "s3", Username: "bob"})

	err := DeleteUserSessions("alice")
	if err != nil {
		t.Fatalf("DeleteUserSessions failed: %v", err)
	}

	aliceSessions, _ := ListUserSessions("alice")
	if len(aliceSessions) != 0 {
		t.Errorf("expected 0 sessions for alice after delete, got %d", len(aliceSessions))
	}

	bobSessions, _ := ListUserSessions("bob")
	if len(bobSessions) != 1 {
		t.Errorf("expected 1 session for bob (unchanged), got %d", len(bobSessions))
	}
}

func TestDeleteInactiveSessions(t *testing.T) {
	initTestDB(t)

	now := time.Now()
	PutSession(&models.Session{ID: "old1", Username: "alice", LastUsedAt: now.Add(-48 * time.Hour)})
	PutSession(&models.Session{ID: "old2", Username: "bob", LastUsedAt: now.Add(-48 * time.Hour)})
	PutSession(&models.Session{ID: "new1", Username: "charlie", LastUsedAt: now})

	cutoff := now.Add(-24 * time.Hour)
	deleted, err := DeleteInactiveSessions(cutoff)
	if err != nil {
		t.Fatalf("DeleteInactiveSessions failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 sessions deleted, got %d", deleted)
	}

	remaining, _ := ListAllSessions()
	if len(remaining) != 1 {
		t.Errorf("expected 1 session remaining, got %d", len(remaining))
	}
}

func TestUpdateSessionActivity(t *testing.T) {
	initTestDB(t)

	oldTime := time.Now().Add(-1 * time.Hour)
	PutSession(&models.Session{ID: "s1", Username: "alice", LastUsedAt: oldTime})

	err := UpdateSessionActivity("s1")
	if err != nil {
		t.Fatalf("UpdateSessionActivity failed: %v", err)
	}

	got, _ := GetSession("s1")
	if !got.LastUsedAt.After(oldTime) {
		t.Errorf("LastUsedAt should be updated to a newer time")
	}
}

func TestUpdateSessionActivity_NotFound(t *testing.T) {
	initTestDB(t)

	err := UpdateSessionActivity("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPutOIDCLink_And_GetOIDCLink(t *testing.T) {
	initTestDB(t)

	link := &models.OIDCLink{
		Provider:   "google",
		Subject:    "123456789",
		Username:   "alice",
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
	}

	err := PutOIDCLink(link)
	if err != nil {
		t.Fatalf("PutOIDCLink failed: %v", err)
	}

	got, err := GetOIDCLink("google", "123456789")
	if err != nil {
		t.Fatalf("GetOIDCLink failed: %v", err)
	}
	if got.Username != link.Username {
		t.Errorf("Username mismatch: got %q, want %q", got.Username, link.Username)
	}
}

func TestGetOIDCLink_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetOIDCLink("nonexistent", "999")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteOIDCLink(t *testing.T) {
	initTestDB(t)

	link := &models.OIDCLink{Provider: "github", Subject: "abc", Username: "bob"}
	PutOIDCLink(link)

	err := DeleteOIDCLink("github", "abc")
	if err != nil {
		t.Fatalf("DeleteOIDCLink failed: %v", err)
	}

	_, err = GetOIDCLink("github", "abc")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListOIDCLinks(t *testing.T) {
	initTestDB(t)

	PutOIDCLink(&models.OIDCLink{Provider: "google", Subject: "1", Username: "alice"})
	PutOIDCLink(&models.OIDCLink{Provider: "github", Subject: "2", Username: "alice"})
	PutOIDCLink(&models.OIDCLink{Provider: "google", Subject: "3", Username: "bob"})

	aliceLinks, err := ListOIDCLinks("alice")
	if err != nil {
		t.Fatalf("ListOIDCLinks failed: %v", err)
	}
	if len(aliceLinks) != 2 {
		t.Errorf("expected 2 links for alice, got %d", len(aliceLinks))
	}

	bobLinks, err := ListOIDCLinks("bob")
	if err != nil {
		t.Fatalf("ListOIDCLinks failed: %v", err)
	}
	if len(bobLinks) != 1 {
		t.Errorf("expected 1 link for bob, got %d", len(bobLinks))
	}
}
