package auth

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateAndVerifyFingerprint(t *testing.T) {
	InitFingerprint("test-fp-secret", 24*time.Hour)

	token, err := GenerateFingerprintToken("testuser")
	if err != nil {
		t.Fatalf("GenerateFingerprintToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("expected token format id:sig, got %q", token)
	}

	username, err := VerifyFingerprintToken(token)
	if err != nil {
		t.Fatalf("VerifyFingerprintToken failed: %v", err)
	}
	if username != "testuser" {
		t.Errorf("username = %q, want testuser", username)
	}
}

func TestVerifyFingerprint_InvalidToken(t *testing.T) {
	InitFingerprint("test-fp-secret", 24*time.Hour)

	_, err := VerifyFingerprintToken("invalid-token")
	if err == nil {
		t.Error("expected error for invalid token format")
	}

	_, err = VerifyFingerprintToken("someid:somesig")
	if err == nil {
		t.Error("expected error for unknown fingerprint ID")
	}
}

func TestVerifyFingerprint_Expired(t *testing.T) {
	InitFingerprint("test-fp-secret", 1*time.Millisecond)

	token, err := GenerateFingerprintToken("testuser")
	if err != nil {
		t.Fatalf("GenerateFingerprintToken failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	_, err = VerifyFingerprintToken(token)
	if err == nil {
		t.Error("expected error for expired fingerprint")
	}
}

func TestInvalidateFingerprint(t *testing.T) {
	InitFingerprint("test-fp-secret", 24*time.Hour)

	token, _ := GenerateFingerprintToken("testuser")
	parts := strings.SplitN(token, ":", 2)
	id := parts[0]

	_, err := VerifyFingerprintToken(token)
	if err != nil {
		t.Fatalf("token should be valid before invalidation: %v", err)
	}

	InvalidateFingerprint(id)

	_, err = VerifyFingerprintToken(token)
	if err == nil {
		t.Error("expected error after invalidation")
	}
}

func TestInvalidateUserFingerprints(t *testing.T) {
	InitFingerprint("test-fp-secret", 24*time.Hour)

	token1, _ := GenerateFingerprintToken("user1")
	token2, _ := GenerateFingerprintToken("user1")
	token3, _ := GenerateFingerprintToken("user2")

	InvalidateUserFingerprints("user1")

	if _, err := VerifyFingerprintToken(token1); err == nil {
		t.Error("token1 should be invalidated")
	}
	if _, err := VerifyFingerprintToken(token2); err == nil {
		t.Error("token2 should be invalidated")
	}
	if _, err := VerifyFingerprintToken(token3); err != nil {
		t.Errorf("token3 should still be valid: %v", err)
	}
}

func TestCleanupExpiredFingerprints(t *testing.T) {
	InitFingerprint("test-fp-secret-cleanup", 1*time.Millisecond)

	fingerprintsMu.Lock()
	fingerprints = make(map[string]fingerprintEntry)
	fingerprintsMu.Unlock()

	GenerateFingerprintToken("user1")
	GenerateFingerprintToken("user2")

	if CountFingerprints() != 2 {
		t.Fatalf("expected 2 fingerprints, got %d", CountFingerprints())
	}

	time.Sleep(10 * time.Millisecond)

	CleanupExpiredFingerprints()

	if CountFingerprints() != 0 {
		t.Errorf("expected 0 fingerprints after cleanup, got %d", CountFingerprints())
	}
}

func TestListUserFingerprints(t *testing.T) {
	InitFingerprint("test-fp-secret-list", 24*time.Hour)

	fingerprintsMu.Lock()
	fingerprints = make(map[string]fingerprintEntry)
	fingerprintsMu.Unlock()

	GenerateFingerprintToken("user1")
	GenerateFingerprintToken("user1")
	GenerateFingerprintToken("user2")

	ids := ListUserFingerprints("user1")
	if len(ids) != 2 {
		t.Errorf("expected 2 fingerprints for user1, got %d", len(ids))
	}

	ids = ListUserFingerprints("user2")
	if len(ids) != 1 {
		t.Errorf("expected 1 fingerprint for user2, got %d", len(ids))
	}
}

func TestGenerateFingerprint_NotInitialized(t *testing.T) {
	fingerprintSecret = nil

	_, err := GenerateFingerprintToken("testuser")
	if err == nil {
		t.Error("expected error when fingerprint not initialized")
	}
}

func TestVerifyFingerprint_TamperedSignature(t *testing.T) {
	InitFingerprint("test-fp-secret", 24*time.Hour)

	token, _ := GenerateFingerprintToken("testuser")
	parts := strings.SplitN(token, ":", 2)

	tampered := parts[0] + ":0000000000000000000000000000000000000000000000000000000000000000"
	_, err := VerifyFingerprintToken(tampered)
	if err == nil {
		t.Error("expected error for tampered signature")
	}
}

func TestCountFingerprints(t *testing.T) {
	fingerprintsMu.Lock()
	fingerprints = make(map[string]fingerprintEntry)
	fingerprintsMu.Unlock()

	InitFingerprint("test-fp-secret-count", 24*time.Hour)

	if CountFingerprints() != 0 {
		t.Errorf("expected 0, got %d", CountFingerprints())
	}

	GenerateFingerprintToken("user1")
	if CountFingerprints() != 1 {
		t.Errorf("expected 1, got %d", CountFingerprints())
	}

	GenerateFingerprintToken("user2")
	if CountFingerprints() != 2 {
		t.Errorf("expected 2, got %d", CountFingerprints())
	}
}
