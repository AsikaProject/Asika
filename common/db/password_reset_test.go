package db

import (
	"errors"
	"testing"
	"time"
)

func TestPutPasswordResetToken(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	token := "test-token-123"
	username := "alice"
	expiresAt := time.Now().Add(15 * time.Minute)

	err = PutPasswordResetToken(token, username, expiresAt)
	if err != nil {
		t.Fatalf("PutPasswordResetToken failed: %v", err)
	}

	// Verify token was stored
	entry, err := GetPasswordResetToken(token)
	if err != nil {
		t.Fatalf("GetPasswordResetToken failed: %v", err)
	}
	if entry == nil {
		t.Fatal("token not found")
	}
	if entry.Username != username {
		t.Errorf("username mismatch: got %s, want %s", entry.Username, username)
	}
	if entry.TokenHash != hashToken(token) {
		t.Errorf("token hash mismatch")
	}
}

func TestGetPasswordResetToken_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	entry, err := GetPasswordResetToken("nonexistent-token")
	if err == nil {
		t.Fatal("expected 'not found' error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("expected nil for nonexistent token")
	}
}

func TestDeletePasswordResetToken(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	token := "test-token-to-delete"
	username := "bob"
	expiresAt := time.Now().Add(15 * time.Minute)

	// Store token
	err = PutPasswordResetToken(token, username, expiresAt)
	if err != nil {
		t.Fatalf("PutPasswordResetToken failed: %v", err)
	}

	// Delete token
	err = DeletePasswordResetToken(token)
	if err != nil {
		t.Fatalf("DeletePasswordResetToken failed: %v", err)
	}

	// Verify token was deleted
	entry, err := GetPasswordResetToken(token)
	if err == nil {
		t.Fatal("expected 'not found' error after deletion, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("token should have been deleted")
	}
}

func TestDeleteExpiredPasswordResetTokens(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	// Store expired token
	expiredToken := "expired-token"
	err = PutPasswordResetToken(expiredToken, "alice", time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("PutPasswordResetToken failed: %v", err)
	}

	// Store valid token
	validToken := "valid-token"
	err = PutPasswordResetToken(validToken, "bob", time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("PutPasswordResetToken failed: %v", err)
	}

	// Delete expired tokens
	err = DeleteExpiredPasswordResetTokens()
	if err != nil {
		t.Fatalf("DeleteExpiredPasswordResetTokens failed: %v", err)
	}

	// Verify expired token was deleted
	entry, err := GetPasswordResetToken(expiredToken)
	if err == nil {
		t.Fatal("expected 'not found' error for expired token, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("expired token should have been deleted")
	}

	// Verify valid token still exists
	entry, err = GetPasswordResetToken(validToken)
	if err != nil {
		t.Fatalf("GetPasswordResetToken failed: %v", err)
	}
	if entry == nil {
		t.Error("valid token should not have been deleted")
	}
}

func TestPasswordResetToken_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	token := "round-trip-token"
	username := "charlie"
	expiresAt := time.Now().Add(15 * time.Minute).Truncate(time.Second)

	err = PutPasswordResetToken(token, username, expiresAt)
	if err != nil {
		t.Fatalf("PutPasswordResetToken failed: %v", err)
	}

	entry, err := GetPasswordResetToken(token)
	if err != nil {
		t.Fatalf("GetPasswordResetToken failed: %v", err)
	}
	if entry == nil {
		t.Fatal("token not found")
	}

	if entry.Username != username {
		t.Errorf("username mismatch: got %s, want %s", entry.Username, username)
	}
	if !entry.ExpiresAt.Equal(expiresAt) {
		t.Errorf("expires_at mismatch: got %v, want %v", entry.ExpiresAt, expiresAt)
	}
}

func TestPasswordResetToken_MultipleTokens(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	// Store multiple tokens for same user
	tokens := []string{"token1", "token2", "token3"}
	for _, token := range tokens {
		err := PutPasswordResetToken(token, "alice", time.Now().Add(15*time.Minute))
		if err != nil {
			t.Fatalf("PutPasswordResetToken failed for %s: %v", token, err)
		}
	}

	// Verify all tokens exist
	for _, token := range tokens {
		entry, err := GetPasswordResetToken(token)
		if err != nil {
			t.Fatalf("GetPasswordResetToken failed for %s: %v", token, err)
		}
		if entry == nil {
			t.Errorf("token %s not found", token)
		}
	}

	// Delete one token
	err = DeletePasswordResetToken("token2")
	if err != nil {
		t.Fatalf("DeletePasswordResetToken failed: %v", err)
	}

	// Verify deleted token is gone
	entry, err := GetPasswordResetToken("token2")
	if err == nil {
		t.Fatal("expected 'not found' error after deletion, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("token2 should have been deleted")
	}

	// Verify other tokens still exist
	entry, err = GetPasswordResetToken("token1")
	if err != nil {
		t.Fatalf("GetPasswordResetToken failed: %v", err)
	}
	if entry == nil {
		t.Error("token1 should still exist")
	}

	entry, err = GetPasswordResetToken("token3")
	if err != nil {
		t.Fatalf("GetPasswordResetToken failed: %v", err)
	}
	if entry == nil {
		t.Error("token3 should still exist")
	}
}

func TestPasswordResetToken_HashConsistency(t *testing.T) {
	token := "test-token"

	hash1 := hashToken(token)
	hash2 := hashToken(token)

	if hash1 != hash2 {
		t.Errorf("hash inconsistency: %s != %s", hash1, hash2)
	}

	// Verify different tokens produce different hashes
	hash3 := hashToken("different-token")
	if hash1 == hash3 {
		t.Error("different tokens should produce different hashes")
	}
}

func TestPutPasswordResetToken_EmptyToken(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	err = PutPasswordResetToken("", "alice", time.Now().Add(15*time.Minute))
	if err != nil {
		t.Logf("PutPasswordResetToken rejects empty token: %v", err)
	} else {
		t.Log("warning: empty token is accepted — consider adding validation")
	}
}

func TestPutPasswordResetToken_EmptyUsername(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	token := "test-empty-username"
	err = PutPasswordResetToken(token, "", time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("PutPasswordResetToken with empty username failed: %v", err)
	}

	entry, err := GetPasswordResetToken(token)
	if err != nil {
		t.Fatalf("GetPasswordResetToken failed: %v", err)
	}
	if entry == nil {
		t.Fatal("token not found")
	}
	if entry.Username != "" {
		t.Errorf("expected empty username, got %s", entry.Username)
	}
}

func TestDeletePasswordResetToken_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	err = DeletePasswordResetToken("nonexistent-token")
	if err != nil {
		t.Logf("DeletePasswordResetToken on nonexistent token returns: %v", err)
	}
}

func TestDeleteExpiredPasswordResetTokens_AllValid(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	PutPasswordResetToken("tok1", "alice", time.Now().Add(1*time.Hour))
	PutPasswordResetToken("tok2", "bob", time.Now().Add(2*time.Hour))

	err = DeleteExpiredPasswordResetTokens()
	if err != nil {
		t.Fatalf("DeleteExpiredPasswordResetTokens failed: %v", err)
	}

	for _, tok := range []string{"tok1", "tok2"} {
		e, err := GetPasswordResetToken(tok)
		if err != nil {
			t.Fatalf("GetPasswordResetToken failed for %s: %v", tok, err)
		}
		if e == nil {
			t.Errorf("%s should still exist", tok)
		}
	}
}

func TestDeleteExpiredPasswordResetTokens_EmptyBucket(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	err = DeleteExpiredPasswordResetTokens()
	if err != nil {
		t.Fatalf("DeleteExpiredPasswordResetTokens on empty bucket failed: %v", err)
	}
}

func TestGetPasswordResetToken_CorruptedData(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	// Insert corrupted JSON directly
	corruptedKey := hashToken("corrupt")
	err = Put(BucketPasswordResetTokens, corruptedKey, []byte("not-json"))
	if err != nil {
		t.Fatalf("failed to insert corrupted data: %v", err)
	}

	entry, err := GetPasswordResetToken("corrupt")
	if err == nil {
		t.Fatal("expected error for corrupted token data")
	}
	if entry != nil {
		t.Error("entry should be nil on unmarshal error")
	}
}

func TestDeleteExpiredPasswordResetTokens_CorruptedEntry(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewBboltStorage(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer s.Close()
	InitWithStorage(s)

	// Insert corrupted data
	corruptedKey := hashToken("corrupted")
	err = Put(BucketPasswordResetTokens, corruptedKey, []byte("{bad json"))
	if err != nil {
		t.Fatalf("failed to insert corrupted data: %v", err)
	}

	// Insert a valid expired token
	err = PutPasswordResetToken("valid-expired", "alice", time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("PutPasswordResetToken failed: %v", err)
	}

	err = DeleteExpiredPasswordResetTokens()
	if err != nil {
		t.Fatalf("should not fail on corrupted entry: %v", err)
	}

	// Corrupted entry should remain (not deleted), valid expired should be deleted
	data, err := Get(BucketPasswordResetTokens, corruptedKey)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if data == nil {
		t.Error("corrupted entry should not have been deleted")
	}

	entry, err := GetPasswordResetToken("valid-expired")
	if err == nil {
		t.Fatal("expected 'not found' error for deleted token, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("valid-expired token should have been deleted")
	}
}

func TestHashToken_EmptyString(t *testing.T) {
	h := hashToken("")
	if h == "" {
		t.Error("hash of empty string should not be empty")
	}
	// sha256 of empty string is well-known
	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if h != expected {
		t.Errorf("hash mismatch: got %s, want %s", h, expected)
	}
}
