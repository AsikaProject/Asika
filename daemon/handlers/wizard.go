package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"log/slog"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

type wizardPayload struct {
	models.Config
	Users []wizardUser `json:"users"`
}

type wizardUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// GetWizardSteps handles GET /api/v1/wizard (10. WebUI Wizard)
func GetWizardSteps(c *gin.Context) {
	if config.Current() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already initialized"})
		return
	}

	steps := []string{
		"mode_selection",
		"database_config",
		"platform_tokens",
		"repository_group",
		"notification_channels",
		"admin_account",
		"self_update",
	}
	c.JSON(http.StatusOK, gin.H{"steps": steps})
}

// SubmitWizardStep handles POST /api/v1/wizard/step/:step
func SubmitWizardStep(c *gin.Context) {
	step := c.Param("step")

	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	slog.Info("wizard step submitted", "step", step)
	c.JSON(http.StatusOK, gin.H{"message": "step saved", "step": step})
}

// CompleteWizard handles POST /api/v1/wizard/step/complete (10. WebUI Wizard)
func CompleteWizard(c *gin.Context) {
	if config.Current() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already initialized"})
		return
	}

	slog.Info("wizard: completing setup")

	var payload wizardPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config", "detail": err.Error()})
		return
	}

	cfg := payload.Config

	// Set defaults
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "release"
	}
	if cfg.Database.Type == "" {
		cfg.Database.Type = "bbolt"
	}
	if cfg.Database.Path == "" {
		if cfg.Database.Type == "mongo" {
			cfg.Database.Path = "mongodb://localhost:27017"
		} else {
			cfg.Database.Path = "/var/lib/asika/asika.db"
		}
	}
	if cfg.Database.Type != "mongo" {
		if !filepath.IsAbs(cfg.Database.Path) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database.path must be an absolute path without .. components"})
			return
		}
	}
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = config.GenerateUUID()
	}
	if cfg.Auth.TokenExpiry == "" {
		cfg.Auth.TokenExpiry = "72h"
	}
	if cfg.Events.Mode == "" {
		cfg.Events.Mode = "webhook"
	}
	if cfg.Git.WorkDir == "" {
		cfg.Git.WorkDir = "/var/lib/asika/work"
	}
	if cfg.MergeQueue.RequiredApprovals == 0 {
		cfg.MergeQueue.RequiredApprovals = 1
		cfg.MergeQueue.CICheckRequired = true
	}

	if len(cfg.RepoGroups) == 0 {
		cfg.RepoGroups = []models.RepoGroupConfig{
			{
				Name: "default",
				Mode: "multi",
			},
		}
	}

	// Write config to file
	if err := config.SaveToFile(cfg); err != nil {
		slog.Error("wizard: failed to write config", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config file", "detail": err.Error()})
		return
	}

	// Store in memory so config.Current() returns non-nil
	config.Store(&cfg)

	// Ensure DB directory exists
	dbDir := filepath.Dir(cfg.Database.Path)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		slog.Error("wizard: failed to create db directory", "dir", dbDir, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create database directory", "detail": err.Error()})
		return
	}

	// Init database
	if err := db.Init(cfg.Database); err != nil {
		slog.Error("wizard: failed to init database", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize database", "detail": err.Error()})
		return
	}

	// Init auth
	auth.Init(cfg.Auth.JWTSecret, config.GenerateTokenExpiry(cfg.Auth.TokenExpiry))

	if cfg.Auth.FingerprintEnabled {
		fpExpiry := config.GenerateTokenExpiry(cfg.Auth.FingerprintExpiry)
		auth.InitFingerprint(cfg.Auth.FingerprintSecret, fpExpiry)
	}

	// Create admin users
	for _, u := range payload.Users {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err != nil {
			slog.Error("wizard: failed to hash password", "username", u.Username, "error", err)
			continue
		}
		if u.Role == "" {
			u.Role = "admin"
		}
		user := models.User{
			Username:     u.Username,
			PasswordHash: string(hash),
			Role:         u.Role,
			CreatedAt:    time.Now(),
		}
		data, err := json.Marshal(user)
		if err != nil {
			slog.Error("wizard: failed to marshal user", "username", u.Username, "error", err)
			continue
		}
		if err := db.Put(db.BucketUsers, u.Username, data); err != nil {
			slog.Error("wizard: failed to save user", "username", u.Username, "error", err)
			continue
		}
		slog.Info("wizard: admin user created", "username", u.Username)
	}

	slog.Info("wizard: setup complete", "path", config.ConfigPath)
	c.JSON(http.StatusOK, gin.H{"message": "setup complete", "path": config.ConfigPath})
}
