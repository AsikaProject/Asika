package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setParam(c *gin.Context, key, value string) {
	c.Params = append(c.Params, gin.Param{Key: key, Value: value})
}

func TestGetNotificationPrefs_Authenticated(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/v1/users/alice/notifications", nil)
	setParam(c, "username", "alice")

	GetNotificationPrefs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var prefs models.NotificationPreferences
	if err := json.Unmarshal(w.Body.Bytes(), &prefs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if prefs.Username != "alice" {
		t.Errorf("expected username alice, got %s", prefs.Username)
	}
	if !prefs.Enabled {
		t.Error("expected enabled by default")
	}
}

func TestGetNotificationPrefs_MissingUsername(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/v1/users//notifications", nil)

	GetNotificationPrefs(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNotificationPrefs_Success(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	prefs := models.NotificationPreferences{
		Username:         "bob",
		Enabled:          true,
		EnabledNotifiers: []string{"telegram", "smtp"},
		EventPrefs:       map[string]bool{"pr_opened": true, "pr_merged": false},
		DigestMode:       "hourly",
	}
	body, _ := json.Marshal(prefs)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("PUT", "/api/v1/users/bob/notifications", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	setParam(c, "username", "bob")

	UpdateNotificationPrefs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result models.NotificationPreferences
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Username != "bob" {
		t.Errorf("expected bob, got %s", result.Username)
	}
	if result.DigestMode != "hourly" {
		t.Errorf("expected hourly, got %s", result.DigestMode)
	}
}

func TestUpdateNotificationPrefs_InvalidBody(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("PUT", "/api/v1/users/bob/notifications", bytes.NewReader([]byte("invalid")))
	c.Request.Header.Set("Content-Type", "application/json")
	setParam(c, "username", "bob")

	UpdateNotificationPrefs(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateNotificationPrefs_MissingUsername(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	gin.SetMode(gin.TestMode)
	prefs := models.NotificationPreferences{Enabled: true}
	body, _ := json.Marshal(prefs)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("PUT", "/api/v1/users/bob/notifications", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	UpdateNotificationPrefs(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCheckNotificationDedup(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	key := "pr_opened:pr-123:telegram"
	exists, err := db.GetNotificationDedup(key)
	if err != nil {
		t.Fatalf("GetNotificationDedup failed: %v", err)
	}
	if exists != nil {
		t.Fatal("expected nil for new dedup key")
	}
}

func TestRecordAndCheckDedup(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	key1 := "pr_opened:pr-456:smtp"
	ts := time.Now()
	data, _ := json.Marshal(ts)
	db.PutNotificationDedup(key1, data)

	stored, err := db.GetNotificationDedup(key1)
	if err != nil || stored == nil {
		t.Fatal("expected stored dedup entry")
	}

	key2 := "pr_opened:pr-456:telegram"
	stored2, _ := db.GetNotificationDedup(key2)
	if stored2 != nil {
		t.Fatal("expected nil for different notifier")
	}

	key3 := "pr_opened:pr-999:smtp"
	stored3, _ := db.GetNotificationDedup(key3)
	if stored3 != nil {
		t.Fatal("expected nil for different PR")
	}
}
