package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	fingerprintSecret []byte
	fingerprintExpiry time.Duration
	fingerprintsMu    sync.RWMutex
	fingerprints      = make(map[string]fingerprintEntry)
)

type fingerprintEntry struct {
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// InitFingerprint initializes the fingerprint module with a secret and token expiry duration.
func InitFingerprint(secret string, expiry time.Duration) {
	fingerprintSecret = []byte(secret)
	fingerprintExpiry = expiry
}

// GenerateFingerprintToken creates a new fingerprint token for the given username.
// Format: base64(id):hmac where hmac = HMAC-SHA256(secret, username:id:expiry)
func GenerateFingerprintToken(username string) (string, error) {
	if len(fingerprintSecret) == 0 {
		return "", fmt.Errorf("fingerprint not initialized")
	}

	id := generateFingerprintID()
	expiresAt := time.Now().Add(fingerprintExpiry)

	fingerprintsMu.Lock()
	fingerprints[id] = fingerprintEntry{
		Username:  username,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}
	fingerprintsMu.Unlock()

	mac := hmac.New(sha256.New, fingerprintSecret)
	mac.Write([]byte(username))
	mac.Write([]byte(":"))
	mac.Write([]byte(id))
	mac.Write([]byte(":"))
	mac.Write([]byte(strconv.FormatInt(expiresAt.Unix(), 10)))
	signature := hex.EncodeToString(mac.Sum(nil))

	return id + ":" + signature, nil
}

// VerifyFingerprintToken verifies a fingerprint token and returns the associated username.
func VerifyFingerprintToken(token string) (string, error) {
	if len(fingerprintSecret) == 0 {
		return "", fmt.Errorf("fingerprint not initialized")
	}

	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid fingerprint token format")
	}

	id := parts[0]
	sig := parts[1]

	fingerprintsMu.RLock()
	entry, exists := fingerprints[id]
	fingerprintsMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("fingerprint not found")
	}

	if time.Now().After(entry.ExpiresAt) {
		InvalidateFingerprint(id)
		return "", fmt.Errorf("fingerprint expired")
	}

	mac := hmac.New(sha256.New, fingerprintSecret)
	mac.Write([]byte(entry.Username))
	mac.Write([]byte(":"))
	mac.Write([]byte(id))
	mac.Write([]byte(":"))
	mac.Write([]byte(strconv.FormatInt(entry.ExpiresAt.Unix(), 10)))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", fmt.Errorf("invalid fingerprint signature")
	}

	return entry.Username, nil
}

// InvalidateFingerprint removes a fingerprint token from the store.
func InvalidateFingerprint(id string) {
	fingerprintsMu.Lock()
	delete(fingerprints, id)
	fingerprintsMu.Unlock()
}

// InvalidateUserFingerprints removes all fingerprint tokens for a given username.
func InvalidateUserFingerprints(username string) {
	fingerprintsMu.Lock()
	defer fingerprintsMu.Unlock()
	for id, entry := range fingerprints {
		if entry.Username == username {
			delete(fingerprints, id)
		}
	}
}

// CleanupExpiredFingerprints removes all expired fingerprint entries.
func CleanupExpiredFingerprints() {
	now := time.Now()
	fingerprintsMu.Lock()
	defer fingerprintsMu.Unlock()
	for id, entry := range fingerprints {
		if now.After(entry.ExpiresAt) {
			delete(fingerprints, id)
		}
	}
}

// ListUserFingerprints returns all active fingerprint IDs for a username.
func ListUserFingerprints(username string) []string {
	fingerprintsMu.RLock()
	defer fingerprintsMu.RUnlock()
	var ids []string
	for id, entry := range fingerprints {
		if entry.Username == username {
			ids = append(ids, id)
		}
	}
	return ids
}

// CountFingerprints returns the total number of stored fingerprint entries.
func CountFingerprints() int {
	fingerprintsMu.RLock()
	defer fingerprintsMu.RUnlock()
	return len(fingerprints)
}

func generateFingerprintID() string {
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(b[8:], uint64(time.Now().Unix()))
	return hex.EncodeToString(b)
}
