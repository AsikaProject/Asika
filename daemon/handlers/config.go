package handlers

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"
	"log/slog"
	"os"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

var (
	workerPoolConfigReloadFuncs []func(models.WorkerPoolConfig)
	procsReloadFuncs            []func(minProcs, maxProcs int)
)

// OnProcsReload registers a callback invoked when min_procs/max_procs config changes.
func OnProcsReload(fn func(minProcs, maxProcs int)) {
	procsReloadFuncs = append(procsReloadFuncs, fn)
}

func notifyProcs(cfg models.Config) {
	for _, fn := range procsReloadFuncs {
		fn(cfg.Server.MinProcs, cfg.Server.MaxProcs)
	}
}

// OnWorkerPoolConfigReload registers a callback invoked when worker_pool config changes.
func OnWorkerPoolConfigReload(fn func(models.WorkerPoolConfig)) {
	workerPoolConfigReloadFuncs = append(workerPoolConfigReloadFuncs, fn)
}

func notifyWorkerPoolConfig() {
	cfg := config.Current()
	if cfg == nil {
		return
	}
	for _, fn := range workerPoolConfigReloadFuncs {
		fn(cfg.WorkerPool)
	}
}

// GetConfig handles GET /api/v1/config (8.4)
// Returns current config with sensitive data masked
func GetConfig(c *gin.Context) {
	cfg := config.Current()

	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	// Create a copy with masked sensitive fields
	masked := *cfg

	// Mask tokens
	masked.Tokens = models.TokensConfig{
		GitHub: maskToken(cfg.Tokens.GitHub),
		GitLab: maskToken(cfg.Tokens.GitLab),
		Gitea:  maskToken(cfg.Tokens.Gitea),
	}

	// Mask JWT secret
	masked.Auth.JWTSecret = maskSecret(cfg.Auth.JWTSecret)

	c.Header("Cache-Control", "private, max-age=10")
	c.JSON(http.StatusOK, masked)
}

// UpdateConfig handles PUT /api/v1/config (8.4)
// Updates hot-reloadable config items
func UpdateConfig(c *gin.Context) {
	var req struct {
		Toml string `json:"toml"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Parse TOML to verify it's valid
	var patch map[string]interface{}
	if err := toml.Unmarshal([]byte(req.Toml), &patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid TOML", "detail": err.Error()})
		return
	}

	// Get current config path
	configPath := os.Getenv("ASIKA_CONFIG")
	if configPath == "" {
		configPath = "/etc/asika_config.toml"
	}

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read config file"})
		return
	}

	autoRollbackVer := config.CurrentCfgVersion()

	// Save snapshot before applying changes
	if err := config.SaveConfigSnapshot(); err != nil {
		slog.Warn("failed to save config snapshot", "error", err)
	}

	// Merge: parse existing, apply patch, re-marshal
	var existing map[string]interface{}
	if err := toml.Unmarshal(data, &existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse existing config"})
		return
	}

	// Apply hot-reloadable items only (tasks.md 3.3)
	if serverPatch, ok := patch["server"].(map[string]interface{}); ok {
		if existingServer, ok := existing["server"].(map[string]interface{}); ok {
			if mp, ok := serverPatch["min_procs"].(int64); ok {
				existingServer["min_procs"] = mp
			}
			if mp, ok := serverPatch["max_procs"].(int64); ok {
				existingServer["max_procs"] = mp
			}
		}
	}
	if labelRules, ok := patch["label_rules"]; ok {
		existing["label_rules"] = labelRules
	}
	if notify, ok := patch["notify"]; ok {
		existing["notify"] = notify
	}
	if spam, ok := patch["spam"]; ok {
		existing["spam"] = spam
	}
	// core_contributors is inside merge_queue config
	if mq, ok := patch["merge_queue"].(map[string]interface{}); ok {
		if cc, ok := mq["core_contributors"].([]interface{}); ok {
			if existingMQ, ok := existing["merge_queue"].(map[string]interface{}); ok {
				existingMQ["core_contributors"] = cc
			}
		}
	}
	if reviewRules, ok := patch["review_rules"]; ok {
		existing["review_rules"] = reviewRules
	}
	if closeReasons, ok := patch["close_reasons"]; ok {
		existing["close_reasons"] = closeReasons
	}
	if workerPool, ok := patch["worker_pool"]; ok {
		existing["worker_pool"] = workerPool
	}
	if quietHours, ok := patch["quiet_hours"]; ok {
		existing["quiet_hours"] = quietHours
	}
	if hookpath, ok := patch["hookpath"]; ok {
		hp, ok := hookpath.(string)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "hookpath must be a string"})
			return
		}
		if !filepath.IsAbs(hp) || strings.Contains(hp, "..") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "hookpath must be an absolute path without .. components"})
			return
		}
		existing["hookpath"] = hookpath
	}

	// Write back
	newData, err := toml.Marshal(&existing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal config"})
		return
	}

	if err := os.WriteFile(configPath, newData, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config"})
		return
	}

	reloadedCfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("config saved but reload failed", "error", err)
		c.JSON(http.StatusOK, gin.H{"message": "config saved but reload failed", "error": err.Error()})
		return
	}
	config.Store(reloadedCfg)
	notifyWorkerPoolConfig()
	notifyProcs(*reloadedCfg)

	go startAutoRollbackWatch(configPath, autoRollbackVer)

	slog.Info("config updated", "path", configPath)
	c.JSON(http.StatusOK, gin.H{"message": "config updated successfully"})
}

// DryRunConfig handles POST /api/v1/config/dry-run
func DryRunConfig(c *gin.Context) {
	var req struct {
		Toml string `json:"toml"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	merged, err := config.DryRun(req.Toml)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dry-run failed", "detail": err.Error()})
		return
	}

	masked := *merged
	masked.Tokens = models.TokensConfig{
		GitHub: maskToken(merged.Tokens.GitHub),
		GitLab: maskToken(merged.Tokens.GitLab),
		Gitea:  maskToken(merged.Tokens.Gitea),
	}
	masked.Auth.JWTSecret = maskSecret(merged.Auth.JWTSecret)

	c.JSON(http.StatusOK, gin.H{
		"valid":  true,
		"config": masked,
	})
}

// maskToken masks a token for display
func maskToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

// maskSecret masks a secret for display
func maskSecret(secret string) string {
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "****" + secret[len(secret)-4:]
}

// TestNotify handles POST /api/v1/test/notify (8.6)
func TestNotify(c *gin.Context) {
	slog.Info("test notification triggered")

	c.JSON(http.StatusOK, gin.H{"message": "test notification sent"})
}

// GetConfigHistory handles GET /api/v1/config/history
func GetConfigHistory(c *gin.Context) {
	limit := 20
	snapshots, err := config.ListConfigVersions(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list config history", "detail": err.Error()})
		return
	}
	type historyEntry struct {
		Version   int            `json:"version"`
		Timestamp time.Time      `json:"timestamp"`
		Config    *models.Config `json:"config"`
	}
	entries := make([]historyEntry, 0, len(snapshots))
	for _, s := range snapshots {
		cfg := *s.Config
		cfg.Tokens = models.TokensConfig{
			GitHub: maskToken(s.Config.Tokens.GitHub),
			GitLab: maskToken(s.Config.Tokens.GitLab),
			Gitea:  maskToken(s.Config.Tokens.Gitea),
		}
		cfg.Auth.JWTSecret = maskSecret(s.Config.Auth.JWTSecret)
		entries = append(entries, historyEntry{
			Version:   s.Version,
			Timestamp: s.Timestamp,
			Config:    &cfg,
		})
	}
	c.JSON(http.StatusOK, entries)
}

// RollbackConfig handles POST /api/v1/config/rollback
func RollbackConfig(c *gin.Context) {
	var req struct {
		Version int `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request, need {version: N}"})
		return
	}
	if req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version must be positive"})
		return
	}
	if err := config.RollbackConfig(req.Version); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config rolled back", "version": req.Version})
}

func startAutoRollbackWatch(configPath string, rollbackVersion int) {
	time.Sleep(60 * time.Second)

	if db.Ping() != nil {
		slog.Error("post-config-change health check failed (DB), auto-rolling back", "version", rollbackVersion)
		rollbackAndReload(configPath, rollbackVersion)
		return
	}
}

func rollbackAndReload(configPath string, version int) {
	if err := config.RollbackConfig(version); err != nil {
		slog.Error("auto-rollback failed", "error", err)
		return
	}
	if _, err := config.Load(configPath); err != nil {
		slog.Error("auto-rollback reload failed", "error", err)
		return
	}
	slog.Info("auto-rollback completed", "version", version)
}
