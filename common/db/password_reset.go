package db

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type PasswordResetToken struct {
	TokenHash string    `json:"token_hash" bson:"token_hash"`
	Username  string    `json:"username" bson:"username"`
	ExpiresAt time.Time `json:"expires_at" bson:"expires_at"`
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

func PutPasswordResetToken(token string, username string, expiresAt time.Time) error {
	entry := PasswordResetToken{
		TokenHash: hashToken(token),
		Username:  username,
		ExpiresAt: expiresAt,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal reset token: %w", err)
	}
	return Put(BucketPasswordResetTokens, entry.TokenHash, data)
}

func GetPasswordResetToken(token string) (*PasswordResetToken, error) {
	data, err := Get(BucketPasswordResetTokens, hashToken(token))
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var entry PasswordResetToken
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal reset token: %w", err)
	}
	return &entry, nil
}

func DeletePasswordResetToken(token string) error {
	return Delete(BucketPasswordResetTokens, hashToken(token))
}

func DeleteExpiredPasswordResetTokens() error {
	var expired [][]byte
	if err := ForEach(BucketPasswordResetTokens, func(key, value []byte) error {
		var entry PasswordResetToken
		if err := json.Unmarshal(value, &entry); err != nil {
			return nil
		}
		if entry.ExpiresAt.Before(time.Now()) {
			expired = append(expired, key)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, key := range expired {
		if err := Delete(BucketPasswordResetTokens, string(key)); err != nil {
			return err
		}
	}
	return nil
}
