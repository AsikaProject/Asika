package crypto

import (
	"os"
	"strings"
	"testing"

	"asika/common/models"
)

func TestEncryptDecrypt(t *testing.T) {
	os.Setenv(keyEnvVar, "test-master-key-for-unit-tests")
	masterKey = deriveKey("test-master-key-for-unit-tests")

	plaintext := "ghp_secret_token_12345"
	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !strings.HasPrefix(ciphertext, prefix) {
		t.Errorf("Ciphertext missing prefix: %s", ciphertext)
	}
	if ciphertext == plaintext {
		t.Error("Ciphertext should not equal plaintext")
	}

	decrypted, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptPlaintext(t *testing.T) {
	os.Setenv(keyEnvVar, "test-key")
	masterKey = deriveKey("test-key")

	plaintext := "not-encrypted"
	decrypted, err := Decrypt(plaintext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypt of plaintext = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptIdempotent(t *testing.T) {
	os.Setenv(keyEnvVar, "test-key")
	masterKey = deriveKey("test-key")

	plaintext := "secret"
	enc1, _ := Encrypt(plaintext)
	enc2, _ := Encrypt(plaintext)

	if enc1 == enc2 {
		t.Error("Same plaintext should produce different ciphertexts (random nonce)")
	}

	dec1, _ := Decrypt(enc1)
	dec2, _ := Decrypt(enc2)
	if dec1 != plaintext || dec2 != plaintext {
		t.Errorf("Both should decrypt to %q, got %q and %q", plaintext, dec1, dec2)
	}
}

func TestEncryptionNotEnabled(t *testing.T) {
	masterKey = nil

	_, err := Encrypt("secret")
	if err == nil {
		t.Error("Expected error when encryption not enabled")
	}

	_, err = Decrypt("enc:dGVzdA==")
	if err == nil {
		t.Error("Expected error when encryption not enabled")
	}
}

func TestEncryptDecryptTokensInConfig(t *testing.T) {
	os.Setenv(keyEnvVar, "test-key")
	masterKey = deriveKey("test-key")

	cfg := &models.Config{
		Tokens: models.TokensConfig{
			GitHub:    "ghp_real_token",
			GitLab:    "glpat_real_token",
			Gitea:     "gitea_token",
			Forgejo:   "",
			Codeberg:  "",
			Bitbucket: "bb_token",
		},
	}

	err := EncryptTokensInConfig(cfg)
	if err != nil {
		t.Fatalf("EncryptTokensInConfig failed: %v", err)
	}

	if cfg.Tokens.GitHub == "ghp_real_token" {
		t.Error("GitHub token should be encrypted")
	}
	if !strings.HasPrefix(cfg.Tokens.GitHub, prefix) {
		t.Error("GitHub token should have enc: prefix")
	}

	err = DecryptTokensInConfig(cfg)
	if err != nil {
		t.Fatalf("DecryptTokensInConfig failed: %v", err)
	}

	if cfg.Tokens.GitHub != "ghp_real_token" {
		t.Errorf("GitHub token = %q, want %q", cfg.Tokens.GitHub, "ghp_real_token")
	}
	if cfg.Tokens.GitLab != "glpat_real_token" {
		t.Errorf("GitLab token = %q, want %q", cfg.Tokens.GitLab, "glpat_real_token")
	}
	if cfg.Tokens.Forgejo != "" {
		t.Errorf("Empty token should remain empty, got %q", cfg.Tokens.Forgejo)
	}
}

func TestGenerateMasterKey(t *testing.T) {
	key := GenerateMasterKey()
	if len(key) != 64 {
		t.Errorf("Master key length = %d, want 64", len(key))
	}

	key2 := GenerateMasterKey()
	if key == key2 {
		t.Error("Generated keys should be unique")
	}
}

func TestIsEncryptionEnabled(t *testing.T) {
	masterKey = nil
	if IsEncryptionEnabled() {
		t.Error("Should return false when key is nil")
	}

	masterKey = deriveKey("test")
	if !IsEncryptionEnabled() {
		t.Error("Should return true when key is set")
	}
}
