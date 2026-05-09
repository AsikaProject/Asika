package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"golang.org/x/crypto/bcrypt"

	"asika/common/models"
)

// WizardStep represents a step in the initialization wizard
type WizardStep struct {
	Step        int               `json:"step"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"fields"`
}

// WizardData holds temporary wizard data
type WizardData struct {
	Tokens     models.TokensConfig
	RepoGroups []models.RepoGroupConfig
	SingleRepo models.SingleRepoConfig
	Events     models.EventsConfig
	Notify     []models.NotifyConfig
	AdminUser  models.User
}

// GetWizardSteps returns the list of wizard steps
func GetWizardSteps() []WizardStep {
	return []WizardStep{
		{
			Step:        1,
			Title:       "Mode Selection",
			Description: "Choose single repo or multi-platform mode",
			Fields:      map[string]string{"mode": "single|multi"},
		},
		{
			Step:        2,
			Title:       "Platform Tokens",
			Description: "Enter platform access tokens",
			Fields:      map[string]string{"github": "token", "gitlab": "token", "gitea": "token"},
		},
		{
			Step:        3,
			Title:       "Repository Configuration",
			Description: "Configure repositories",
			Fields:      map[string]string{"repos": "config"},
		},
		{
			Step:        4,
			Title:       "Event Source",
			Description: "Choose webhook or polling",
			Fields:      map[string]string{"events": "config"},
		},
		{
			Step:        5,
			Title:       "Notification",
			Description: "Configure notifications",
			Fields:      map[string]string{"notify": "config"},
		},
		{
			Step:        6,
			Title:       "Admin Account",
			Description: "Create admin user",
			Fields:      map[string]string{"username": "string", "password": "string"},
		},
	}
}

// GenerateConfig generates TOML config from wizard data
func GenerateConfig(data *WizardData) (string, error) {
	cfg := models.Config{
		Server: models.ServerConfig{
			Listen: ":8080",
			Mode:   "release",
		},
		Database: models.DatabaseConfig{
			Path: "/var/lib/asika/asika.db",
		},
		Auth: models.AuthConfig{
			JWTSecret:   GenerateUUID(),
			TokenExpiry: "72h",
		},
		Tokens: data.Tokens,
		Events: data.Events,
		Notify: data.Notify,
		MergeQueue: models.MergeQueueConfig{
			RequiredApprovals: 1,
		},
		HookPath: "",
	}

	// Set mode from first repo group or default to multi
	if len(data.RepoGroups) > 0 {
		cfg.RepoGroups = data.RepoGroups
	} else {
		// Create a default repo group
		mode := "multi"
		cfg.RepoGroups = []models.RepoGroupConfig{
			{
				Name: "default",
				Mode: mode,
			},
		}
	}

	// Hash admin password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(data.AdminUser.PasswordHash), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	data.AdminUser.PasswordHash = string(hashedPassword)
	data.AdminUser.Role = "admin"
	data.AdminUser.CreatedAt = time.Now()

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return "", fmt.Errorf("failed to encode config: %w", err)
	}

	return buf.String(), nil
}

// WriteConfig writes the config to file
func WriteConfig(path, content string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0600)
}

// IsInitialized checks if the config file exists
func IsInitialized(configPath string) bool {
	_, err := os.Stat(configPath)
	return err == nil
}
