package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/handlers/pr"
	"asika/daemon/queue"
	"asika/testutil"
)

func setupQueueHandlerTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	testutil.NewTestDB(t)

 	mock := testutil.NewMockPlatformClient()
	testClients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "queue-repo", Mode: "multi", GitHub: "owner/repo"},
		},
	}
	config.Store(cfg)

	qMgr := queue.NewManager(cfg, testClients)
	InitQueueMgr(qMgr)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "operator")
		c.Set("role", "operator")
		c.Next()
	})
	{
		q := protected.Group("/queue/:repo_group")
		{
			q.GET("", GetQueue)
			q.POST("/recheck", RecheckQueue)
		}
	}

	cleanup := func() {
		db.Close()
	}
	return engine, cleanup
}

func TestGetQueue_WithMgr(t *testing.T) {
	engine, cleanup := setupQueueHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/queue/queue-repo", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRecheckQueue_WithMgr(t *testing.T) {
	engine, cleanup := setupQueueHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/queue/queue-repo/recheck", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRecheckQueue_NotConfigured(t *testing.T) {
	engine, cleanup := setupQueueHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/queue/unknown/recheck", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for unconfigured repo group, got %d", w.Code)
	}
}

func TestPRHandlers_SingleMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-repo",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "team/docs",
			},
		},
	}
	config.Store(cfg)

	mock := testutil.NewMockPlatformClient()
	pr.InitClients(map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	})

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})
	{
		prs := protected.Group("/repos/:repo_group/prs")
		{
			prs.GET("", ListPRs)
			prs.GET("/:pr_id", GetPR)
			prs.POST("/:pr_id/approve", ApprovePR)
			prs.POST("/:pr_id/reopen", ReopenPR)
			prs.POST("/:pr_id/spam", MarkSpam)
		}
	}

	t.Run("list PRs for single mode", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/repos/single-repo/prs", nil)
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("get PR for single mode - not found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/repos/single-repo/prs/999", nil)
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if errStr, ok := resp["error"]; ok {
			t.Logf("expected 'not found' response: %v", errStr)
		}
	})

	t.Run("spam mark on single mode", func(t *testing.T) {
		pr := models.PRRecord{
			ID:        "spam-pr",
			RepoGroup: "single-repo",
			Platform:  "github",
			PRNumber:  42,
			Title:     "spam content",
			Author:    "bot1",
			State:     "open",
		}
		data, _ := json.Marshal(pr)
		db.PutPRWithIndex("single-repo#github#42", data, "spam-pr", "single-repo", 42)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/repos/single-repo/prs/42/spam", nil)
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for mark spam, got %d", w.Code)
		}

		// Verify PR marked as spam in DB
		stored, _ := db.Get(db.BucketPRs, "single-repo#github#42")
		var updated models.PRRecord
		json.Unmarshal(stored, &updated)
		if !updated.SpamFlag {
			t.Error("expected SpamFlag=true after marking as spam")
		}
		if updated.State != "spam" {
			t.Errorf("expected state=spam, got %q", updated.State)
		}
	})

	t.Run("approve on single mode fails when PR not found", func(t *testing.T) {
		mock.Err = fmt.Errorf("PR not found")
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/repos/single-repo/prs/9999/approve", nil)
		engine.ServeHTTP(w, req)
		mock.Err = nil

		if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
			t.Errorf("expected 404/500 for approve PR not found, got %d", w.Code)
		}
	})

	t.Run("reopen on single mode fails when PR not found", func(t *testing.T) {
		mock.Err = fmt.Errorf("PR not found")
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/repos/single-repo/prs/9999/reopen", nil)
		engine.ServeHTTP(w, req)
		mock.Err = nil

		if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
			t.Errorf("expected 404/500 for reopen PR not found, got %d", w.Code)
		}
	})

	t.Run("reopen on single mode fails when PR not found", func(t *testing.T) {
		mock.Err = fmt.Errorf("PR not found")
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/repos/single-repo/prs/9999/reopen", nil)
		engine.ServeHTTP(w, req)
		mock.Err = nil

		if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
			t.Errorf("expected 404/500 for reopen, got %d", w.Code)
		}
	})
}

func TestAuthHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testutil.NewTestDB(t)
	defer db.Close()

	engine := gin.New()
	api := engine.Group("/api/v1")
	auth := api.Group("/auth")
	{
		auth.POST("/login", Login)
		auth.POST("/logout", Logout)
	}

	t.Run("login with empty request", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty login, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("login with valid credentials", func(t *testing.T) {
		// Create a test user first
		passwordHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.DefaultCost)
		user := models.User{
			Username:     "testuser",
			PasswordHash: string(passwordHash),
			Role:         "admin",
		}
		data, _ := json.Marshal(user)
		db.Put(db.BucketUsers, "testuser", data)

		w := httptest.NewRecorder()
		body := strings.NewReader(`{"username":"testuser","password":"testpass"}`)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for valid login, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("logout", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
		engine.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for logout, got %d", w.Code)
		}
	})
}
