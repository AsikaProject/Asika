package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
)

const backupDirPerm = 0750

// CreateBackup handles POST /api/v1/admin/backup
// Creates a hot backup of the bbolt database file.
func CreateBackup(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}
	dbPath := cfg.Database.Path
	if dbPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database path not configured"})
		return
	}

	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")
	if err := os.MkdirAll(backupDir, backupDirPerm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create backup directory", "detail": err.Error()})
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("asika_%s.db", timestamp))

	if err := db.BackupToFile(backupPath); err != nil {
		slog.Error("backup failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup failed", "detail": err.Error()})
		return
	}

	slog.Info("database backup created", "path", backupPath)
	c.JSON(http.StatusOK, gin.H{
		"message":   "backup created",
		"path":      backupPath,
		"timestamp": timestamp,
	})
}

// ListBackups handles GET /api/v1/admin/backups
// Lists all backup files in the backup directory.
func ListBackups(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}
	dbPath := cfg.Database.Path
	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"backups": []gin.H{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read backup directory"})
		return
	}

	type backupInfo struct {
		Filename string    `json:"filename"`
		Size     int64     `json:"size_bytes"`
		Modified time.Time `json:"modified_at"`
	}
	backups := make([]backupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupInfo{
			Filename: entry.Name(),
			Size:     info.Size(),
			Modified: info.ModTime(),
		})
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

// RestoreBackup handles POST /api/v1/admin/restore
// Restores the database from a backup file.
// Requires server restart after restore.
func RestoreBackup(c *gin.Context) {
	var req struct {
		Filename string `json:"filename"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}
	dbPath := cfg.Database.Path
	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")
	backupPath := filepath.Join(backupDir, req.Filename)

	// Validate the backup file exists and is within the backup directory
	absBackup, err := filepath.Abs(backupPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid backup path"})
		return
	}
	absDir, _ := filepath.Abs(backupDir)
	if len(absBackup) <= len(absDir) || absBackup[:len(absDir)] != absDir {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup path outside backup directory"})
		return
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "backup file not found"})
		return
	}

	// Close current DB, restore from backup, reopen
	db.Close()

	// Copy backup over current DB
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read backup file"})
		return
	}
	if err := os.WriteFile(dbPath, backupData, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restore backup"})
		return
	}

	slog.Info("database restored from backup", "backup", backupPath, "note", "server restart required")
	c.JSON(http.StatusOK, gin.H{
		"message": "database restored successfully — restart server to take effect",
		"backup":  req.Filename,
	})
}
