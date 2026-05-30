package db

import (
	"testing"
	"time"

	"asika/common/models"
)

func TestPutAPIKey_And_GetAPIKey(t *testing.T) {
	initTestDB(t)

	key := &models.APIKey{
		ID:                "key-1",
		Name:              "Test API Key",
		KeyHash:           "hash123",
		KeyHMAC:           "hmac456",
		Role:              "operator",
		CreatedAt:         time.Now(),
		CreatedBy:         "admin",
		AllowedRepoGroups: []string{"frontend", "backend"},
		Permissions: models.UserPermissions{
			CanApprove: true,
			CanMerge:   false,
		},
	}

	err := PutAPIKey(key)
	if err != nil {
		t.Fatalf("PutAPIKey failed: %v", err)
	}

	got, err := GetAPIKey("key-1")
	if err != nil {
		t.Fatalf("GetAPIKey failed: %v", err)
	}
	if got.Name != key.Name {
		t.Errorf("Name mismatch: got %q, want %q", got.Name, key.Name)
	}
	if got.Role != key.Role {
		t.Errorf("Role mismatch: got %q, want %q", got.Role, key.Role)
	}
	if len(got.AllowedRepoGroups) != 2 {
		t.Errorf("expected 2 allowed repo groups, got %d", len(got.AllowedRepoGroups))
	}
	if !got.Permissions.CanApprove {
		t.Error("expected CanApprove=true")
	}
}

func TestGetAPIKey_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetAPIKey("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteAPIKey(t *testing.T) {
	initTestDB(t)

	key := &models.APIKey{ID: "to-delete", Name: "Delete Me"}
	PutAPIKey(key)

	err := DeleteAPIKey("to-delete")
	if err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}

	_, err = GetAPIKey("to-delete")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListAPIKeys(t *testing.T) {
	initTestDB(t)

	for i := 0; i < 5; i++ {
		PutAPIKey(&models.APIKey{ID: string(rune('a' + i)), Name: "Key"})
	}

	keys, err := ListAPIKeys(10, 0)
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}
	if len(keys) != 5 {
		t.Errorf("expected 5 keys, got %d", len(keys))
	}
}

func TestListAPIKeys_WithLimit(t *testing.T) {
	initTestDB(t)

	for i := 0; i < 10; i++ {
		PutAPIKey(&models.APIKey{ID: string(rune('a' + i)), Name: "Key"})
	}

	keys, err := ListAPIKeys(3, 0)
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys with limit, got %d", len(keys))
	}
}

func TestListAPIKeys_WithOffset(t *testing.T) {
	initTestDB(t)

	for i := 0; i < 5; i++ {
		PutAPIKey(&models.APIKey{ID: string(rune('a' + i)), Name: "Key"})
	}

	keys, err := ListAPIKeys(10, 2)
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys after offset, got %d", len(keys))
	}
}

func TestPutSpamAuthor_And_GetSpamAuthor(t *testing.T) {
	initTestDB(t)

	author := &models.SpamAuthor{
		Author:    "spammer123",
		Platform:  "github",
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
		Count:     5,
	}

	err := PutSpamAuthor(author)
	if err != nil {
		t.Fatalf("PutSpamAuthor failed: %v", err)
	}

	got, err := GetSpamAuthor("spammer123", "github")
	if err != nil {
		t.Fatalf("GetSpamAuthor failed: %v", err)
	}
	if got.Count != author.Count {
		t.Errorf("Count mismatch: got %d, want %d", got.Count, author.Count)
	}
}

func TestGetSpamAuthor_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetSpamAuthor("nonexistent", "github")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListSpamAuthors(t *testing.T) {
	initTestDB(t)

	PutSpamAuthor(&models.SpamAuthor{Author: "spammer1", Platform: "github"})
	PutSpamAuthor(&models.SpamAuthor{Author: "spammer2", Platform: "gitlab"})
	PutSpamAuthor(&models.SpamAuthor{Author: "spammer3", Platform: "github"})

	authors, err := ListSpamAuthors()
	if err != nil {
		t.Fatalf("ListSpamAuthors failed: %v", err)
	}
	if len(authors) != 3 {
		t.Errorf("expected 3 authors, got %d", len(authors))
	}
}
