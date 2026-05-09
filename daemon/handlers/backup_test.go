package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"strings"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupBackupTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("backup-test-secret", 72*time.Hour)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})

	admin := protected.Group("/admin")
	{
		admin.POST("/backup", CreateBackup)
		admin.GET("/backups", ListBackups)
		admin.POST("/restore", RestoreBackup)
	}

	cleanup := func() { db.Close() }
	return engine, cleanup
}

func TestCreateBackup_Success(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")
	// Re-init DB at new location
	db.Close()
	db.Init(dbFile)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: dbFile},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/backup", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "backup created" {
		t.Errorf("message = %v, want backup created", resp["message"])
	}

	// Verify backup file exists
	path := resp["path"].(string)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("backup file should exist at %s", path)
	}
}

func TestCreateBackup_NoConfig(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()
	config.Store(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/backup", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestListBackups_Empty(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")
	db.Close()
	db.Init(dbFile)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: dbFile},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/backups", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	backups := resp["backups"].([]interface{})
	if len(backups) != 0 {
		t.Errorf("backups = %d, want 0", len(backups))
	}
}

func TestListBackups_WithBackups(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")
	db.Close()
	db.Init(dbFile)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: dbFile},
	}
	config.Store(cfg)

	// Create a backup
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/backup", nil)
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("backup creation failed: %d", w.Code)
	}

	// List backups
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/backups", nil)
	engine.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	backups := resp["backups"].([]interface{})
	if len(backups) != 1 {
		t.Errorf("backups = %d, want 1", len(backups))
	}
}

func TestListBackups_NoConfig(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()
	config.Store(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/backups", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestRestoreBackup_MissingFilename(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")
	db.Close()
	db.Init(dbFile)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: dbFile},
	}
	config.Store(cfg)

	body := `{}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRestoreBackup_FileNotFound(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")
	db.Close()
	db.Init(dbFile)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: dbFile},
	}
	config.Store(cfg)

	body := `{"filename": "nonexistent.db"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestRestoreBackup_NoConfig(t *testing.T) {
	engine, cleanup := setupBackupTest(t)
	defer cleanup()
	config.Store(nil)

	body := `{"filename": "test.db"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
