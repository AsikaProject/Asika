package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"asika/common/models"
)

const (
	keyEnvVar = "ASIKA_MASTER_KEY"
	keyLength = 32
	nonceSize = 12
	prefix    = "enc:"
)

var masterKey []byte

func init() {
	if key := os.Getenv(keyEnvVar); key != "" {
		masterKey = deriveKey(key)
	} else {
		slog.Warn("encryption not enabled: " + keyEnvVar + " environment variable not set; tokens will be stored in plaintext")
	}
}

func deriveKey(input string) []byte {
	h := sha256.Sum256([]byte(input))
	return h[:]
}

func IsEncryptionEnabled() bool {
	return masterKey != nil
}

func Encrypt(plaintext string) (string, error) {
	if len(masterKey) == 0 {
		return "", fmt.Errorf("encryption not enabled: set %s environment variable", keyEnvVar)
	}
	if strings.HasPrefix(plaintext, prefix) {
		return plaintext, nil
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(ciphertext string) (string, error) {
	if len(masterKey) == 0 {
		return "", fmt.Errorf("encryption not enabled: set %s environment variable", keyEnvVar)
	}
	if !strings.HasPrefix(ciphertext, prefix) {
		return ciphertext, nil
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, prefix))
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

func EncryptTokensInConfig(cfg *models.Config) error {
	if !IsEncryptionEnabled() {
		return nil
	}

	var err error
	cfg.Tokens.GitHub, err = Encrypt(cfg.Tokens.GitHub)
	if err != nil {
		return fmt.Errorf("encrypt github token: %w", err)
	}
	cfg.Tokens.GitLab, err = Encrypt(cfg.Tokens.GitLab)
	if err != nil {
		return fmt.Errorf("encrypt gitlab token: %w", err)
	}
	cfg.Tokens.Gitea, err = Encrypt(cfg.Tokens.Gitea)
	if err != nil {
		return fmt.Errorf("encrypt gitea token: %w", err)
	}
	cfg.Tokens.Forgejo, err = Encrypt(cfg.Tokens.Forgejo)
	if err != nil {
		return fmt.Errorf("encrypt forgejo token: %w", err)
	}
	cfg.Tokens.Codeberg, err = Encrypt(cfg.Tokens.Codeberg)
	if err != nil {
		return fmt.Errorf("encrypt codeberg token: %w", err)
	}
	cfg.Tokens.Bitbucket, err = Encrypt(cfg.Tokens.Bitbucket)
	if err != nil {
		return fmt.Errorf("encrypt bitbucket token: %w", err)
	}

	return nil
}

func DecryptTokensInConfig(cfg *models.Config) error {
	if !IsEncryptionEnabled() {
		return nil
	}

	var err error
	cfg.Tokens.GitHub, err = Decrypt(cfg.Tokens.GitHub)
	if err != nil {
		return fmt.Errorf("decrypt github token: %w", err)
	}
	cfg.Tokens.GitLab, err = Decrypt(cfg.Tokens.GitLab)
	if err != nil {
		return fmt.Errorf("decrypt gitlab token: %w", err)
	}
	cfg.Tokens.Gitea, err = Decrypt(cfg.Tokens.Gitea)
	if err != nil {
		return fmt.Errorf("decrypt gitea token: %w", err)
	}
	cfg.Tokens.Forgejo, err = Decrypt(cfg.Tokens.Forgejo)
	if err != nil {
		return fmt.Errorf("decrypt forgejo token: %w", err)
	}
	cfg.Tokens.Codeberg, err = Decrypt(cfg.Tokens.Codeberg)
	if err != nil {
		return fmt.Errorf("decrypt codeberg token: %w", err)
	}
	cfg.Tokens.Bitbucket, err = Decrypt(cfg.Tokens.Bitbucket)
	if err != nil {
		return fmt.Errorf("decrypt bitbucket token: %w", err)
	}

	return nil
}

func GenerateMasterKey() string {
	b := make([]byte, keyLength)
	rand.Read(b)
	return hex.EncodeToString(b)
}
