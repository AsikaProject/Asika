package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupRulesTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("rules-test-secret", 72*time.Hour)

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "rules-test-secret", TokenExpiry: "72h"},
	}
	config.Store(cfg)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})

	rules := protected.Group("/rules")
	{
		rules.GET("/labels", GetLabelRules)
		rules.PUT("/labels", UpdateLabelRules)
	}

	cleanup := func() { db.Close() }
	return engine, cleanup
}

func TestGetLabelRules_FromConfig(t *testing.T) {
	engine, cleanup := setupRulesTest(t)
	defer cleanup()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "rules-test-secret", TokenExpiry: "72h"},
		LabelRules: []models.LabelRule{
			{Pattern: "*.go", Label: "go-code", Color: "00ADD8"},
			{Pattern: "*.md", Label: "docs", Color: "0075CA"},
		},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/rules/labels", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var rules []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules) != 2 {
		t.Errorf("rules = %d, want 2", len(rules))
	}
}

func TestGetLabelRules_FromDB(t *testing.T) {
	engine, cleanup := setupRulesTest(t)
	defer cleanup()

	// Store rules in DB
	dbRules := []models.LabelRule{
		{Pattern: "src/*", Label: "source"},
	}
	data, _ := json.Marshal(dbRules)
	db.Put(db.BucketConfig, "label_rules", data)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/rules/labels", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var rules []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules) != 1 {
		t.Errorf("rules = %d, want 1", len(rules))
	}
}

func TestGetLabelRules_NoConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb
	t.Cleanup(func() { db.Close() })

	auth.Init("rules-noconfig-test", 72*time.Hour)
	config.Store(nil)

	engine := gin.New()
	engine.GET("/api/v1/rules/labels", GetLabelRules)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/rules/labels", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestUpdateLabelRules_Success(t *testing.T) {
	engine, cleanup := setupRulesTest(t)
	defer cleanup()

	rules := []models.LabelRule{
		{Pattern: "*.go", Label: "go-code", Color: "00ADD8"},
		{Pattern: "*.md", Label: "docs", Color: "0075CA"},
	}
	body, _ := json.Marshal(rules)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/rules/labels", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	// Verify rules were saved to DB
	data, err := db.Get(db.BucketConfig, "label_rules")
	if err != nil {
		t.Fatalf("rules not saved to DB: %v", err)
	}
	var saved []models.LabelRule
	json.Unmarshal(data, &saved)
	if len(saved) != 2 {
		t.Errorf("saved rules = %d, want 2", len(saved))
	}

	// Verify in-memory config was updated
	cfg := config.Current()
	if len(cfg.LabelRules) != 2 {
		t.Errorf("config rules = %d, want 2", len(cfg.LabelRules))
	}
}

func TestUpdateLabelRules_InvalidJSON(t *testing.T) {
	engine, cleanup := setupRulesTest(t)
	defer cleanup()

	body := `not json`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/rules/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUpdateLabelRules_EmptyArray(t *testing.T) {
	engine, cleanup := setupRulesTest(t)
	defer cleanup()

	body := `[]`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/rules/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}
