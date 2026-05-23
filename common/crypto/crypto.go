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
		slog.Warn("encryption not enabled: " + keyEnvVar + " environment variable not set; tokens will be stored in plaintext. Set " + keyEnvVar + " in production.")
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

func encryptToken(field *string) error {
	if *field == "" {
		return nil
	}
	encrypted, err := Encrypt(*field)
	if err != nil {
		return err
	}
	*field = encrypted
	return nil
}

func EncryptSecretsInConfig(cfg *models.Config) error {
	if !IsEncryptionEnabled() {
		return nil
	}

	var err error
	if err = encryptToken(&cfg.Tokens.GitHub); err != nil {
		return fmt.Errorf("encrypt github token: %w", err)
	}
	if err = encryptToken(&cfg.Tokens.GitLab); err != nil {
		return fmt.Errorf("encrypt gitlab token: %w", err)
	}
	if err = encryptToken(&cfg.Tokens.Gitea); err != nil {
		return fmt.Errorf("encrypt gitea token: %w", err)
	}
	if err = encryptToken(&cfg.Tokens.Forgejo); err != nil {
		return fmt.Errorf("encrypt forgejo token: %w", err)
	}
	if err = encryptToken(&cfg.Tokens.Codeberg); err != nil {
		return fmt.Errorf("encrypt codeberg token: %w", err)
	}
	if err = encryptToken(&cfg.Tokens.Bitbucket); err != nil {
		return fmt.Errorf("encrypt bitbucket token: %w", err)
	}

	if cfg.Tokens.Gerrit.Password != "" {
		cfg.Tokens.Gerrit.Password, err = Encrypt(cfg.Tokens.Gerrit.Password)
		if err != nil {
			return fmt.Errorf("encrypt gerrit password: %w", err)
		}
	}

	cfg.Auth.JWTSecret, err = Encrypt(cfg.Auth.JWTSecret)
	if err != nil {
		return fmt.Errorf("encrypt jwt secret: %w", err)
	}

	if cfg.Auth.FingerprintSecret != "" {
		cfg.Auth.FingerprintSecret, err = Encrypt(cfg.Auth.FingerprintSecret)
		if err != nil {
			return fmt.Errorf("encrypt fingerprint secret: %w", err)
		}
	}

	if cfg.Events.WebhookSecret != "" {
		cfg.Events.WebhookSecret, err = Encrypt(cfg.Events.WebhookSecret)
		if err != nil {
			return fmt.Errorf("encrypt webhook secret: %w", err)
		}
	}

	if cfg.Feishu.AppSecret != "" {
		cfg.Feishu.AppSecret, err = Encrypt(cfg.Feishu.AppSecret)
		if err != nil {
			return fmt.Errorf("encrypt feishu app secret: %w", err)
		}
	}
	if cfg.Feishu.EncryptKey != "" {
		cfg.Feishu.EncryptKey, err = Encrypt(cfg.Feishu.EncryptKey)
		if err != nil {
			return fmt.Errorf("encrypt feishu encrypt key: %w", err)
		}
	}

	if cfg.Telegram.Token != "" {
		cfg.Telegram.Token, err = Encrypt(cfg.Telegram.Token)
		if err != nil {
			return fmt.Errorf("encrypt telegram token: %w", err)
		}
	}
	if cfg.Discord.Token != "" {
		cfg.Discord.Token, err = Encrypt(cfg.Discord.Token)
		if err != nil {
			return fmt.Errorf("encrypt discord token: %w", err)
		}
	}
	if cfg.Slack.Token != "" {
		cfg.Slack.Token, err = Encrypt(cfg.Slack.Token)
		if err != nil {
			return fmt.Errorf("encrypt slack token: %w", err)
		}
	}
	if cfg.Slack.AppToken != "" {
		cfg.Slack.AppToken, err = Encrypt(cfg.Slack.AppToken)
		if err != nil {
			return fmt.Errorf("encrypt slack app token: %w", err)
		}
	}

	return nil
}

func EncryptTokensInConfig(cfg *models.Config) error {
	return EncryptSecretsInConfig(cfg)
}

func DecryptSecretsInConfig(cfg *models.Config) error {
	if !IsEncryptionEnabled() {
		encFields := collectEncryptedFields(cfg)
		if len(encFields) > 0 {
			return fmt.Errorf("ASIKA_MASTER_KEY not set but found encrypted values in fields: %s", strings.Join(encFields, ", "))
		}
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

	cfg.Tokens.Gerrit.Password, err = Decrypt(cfg.Tokens.Gerrit.Password)
	if err != nil {
		return fmt.Errorf("decrypt gerrit password: %w", err)
	}

	cfg.Auth.JWTSecret, err = Decrypt(cfg.Auth.JWTSecret)
	if err != nil {
		return fmt.Errorf("decrypt jwt secret: %w", err)
	}

	if cfg.Auth.FingerprintSecret != "" {
		cfg.Auth.FingerprintSecret, err = Decrypt(cfg.Auth.FingerprintSecret)
		if err != nil {
			return fmt.Errorf("decrypt fingerprint secret: %w", err)
		}
	}

	if cfg.Events.WebhookSecret != "" {
		cfg.Events.WebhookSecret, err = Decrypt(cfg.Events.WebhookSecret)
		if err != nil {
			return fmt.Errorf("decrypt webhook secret: %w", err)
		}
	}

	if cfg.Feishu.AppSecret != "" {
		cfg.Feishu.AppSecret, err = Decrypt(cfg.Feishu.AppSecret)
		if err != nil {
			return fmt.Errorf("decrypt feishu app secret: %w", err)
		}
	}
	if cfg.Feishu.EncryptKey != "" {
		cfg.Feishu.EncryptKey, err = Decrypt(cfg.Feishu.EncryptKey)
		if err != nil {
			return fmt.Errorf("decrypt feishu encrypt key: %w", err)
		}
	}

	if cfg.Telegram.Token != "" {
		cfg.Telegram.Token, err = Decrypt(cfg.Telegram.Token)
		if err != nil {
			return fmt.Errorf("decrypt telegram token: %w", err)
		}
	}
	if cfg.Discord.Token != "" {
		cfg.Discord.Token, err = Decrypt(cfg.Discord.Token)
		if err != nil {
			return fmt.Errorf("decrypt discord token: %w", err)
		}
	}
	if cfg.Slack.Token != "" {
		cfg.Slack.Token, err = Decrypt(cfg.Slack.Token)
		if err != nil {
			return fmt.Errorf("decrypt slack token: %w", err)
		}
	}
	if cfg.Slack.AppToken != "" {
		cfg.Slack.AppToken, err = Decrypt(cfg.Slack.AppToken)
		if err != nil {
			return fmt.Errorf("decrypt slack app token: %w", err)
		}
	}

	return nil
}

func DecryptTokensInConfig(cfg *models.Config) error {
	return DecryptSecretsInConfig(cfg)
}

func GenerateMasterKey() (string, error) {
	b := make([]byte, keyLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate master key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func collectEncryptedFields(cfg *models.Config) []string {
	var fields []string
	if strings.HasPrefix(cfg.Tokens.GitHub, prefix) {
		fields = append(fields, "tokens.github")
	}
	if strings.HasPrefix(cfg.Tokens.GitLab, prefix) {
		fields = append(fields, "tokens.gitlab")
	}
	if strings.HasPrefix(cfg.Tokens.Gitea, prefix) {
		fields = append(fields, "tokens.gitea")
	}
	if strings.HasPrefix(cfg.Tokens.Forgejo, prefix) {
		fields = append(fields, "tokens.forgejo")
	}
	if strings.HasPrefix(cfg.Tokens.Codeberg, prefix) {
		fields = append(fields, "tokens.codeberg")
	}
	if strings.HasPrefix(cfg.Tokens.Bitbucket, prefix) {
		fields = append(fields, "tokens.bitbucket")
	}
	if strings.HasPrefix(cfg.Tokens.Gerrit.Password, prefix) {
		fields = append(fields, "tokens.gerrit.password")
	}
	if strings.HasPrefix(cfg.Auth.JWTSecret, prefix) {
		fields = append(fields, "auth.jwt_secret")
	}
	if strings.HasPrefix(cfg.Auth.FingerprintSecret, prefix) {
		fields = append(fields, "auth.fingerprint_secret")
	}
	if strings.HasPrefix(cfg.Events.WebhookSecret, prefix) {
		fields = append(fields, "events.webhook_secret")
	}
	if strings.HasPrefix(cfg.Feishu.AppSecret, prefix) {
		fields = append(fields, "feishu.app_secret")
	}
	if strings.HasPrefix(cfg.Feishu.EncryptKey, prefix) {
		fields = append(fields, "feishu.encrypt_key")
	}
	if strings.HasPrefix(cfg.Telegram.Token, prefix) {
		fields = append(fields, "telegram.token")
	}
	if strings.HasPrefix(cfg.Discord.Token, prefix) {
		fields = append(fields, "discord.token")
	}
	if strings.HasPrefix(cfg.Slack.Token, prefix) {
		fields = append(fields, "slack.token")
	}
	if strings.HasPrefix(cfg.Slack.AppToken, prefix) {
		fields = append(fields, "slack.app_token")
	}
	for i, p := range cfg.Auth.OIDCProviders {
		if strings.HasPrefix(p.ClientSecret, prefix) {
			fields = append(fields, fmt.Sprintf("oidc_providers[%d].client_secret", i))
		}
	}
	return fields
}
