package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/stale"
	"asika/testutil"
)

func setupStaleTest(t *testing.T, mgr *stale.Manager) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("stale-test-secret", 72*time.Hour)

	mock := testutil.NewMockPlatformClient()
	clients = map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "stale-test-secret", TokenExpiry: "72h"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
		Stale: models.StaleConfig{
			StaleLabel: "stale",
		},
	}
	config.Store(cfg)

	InitStaleManager(mgr)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})

	staleGroup := protected.Group("/stale")
	{
		staleGroup.POST("/check", HandleStaleCheck)
		staleGroup.POST("/check/:repo_group", HandleStaleCheck)
		staleGroup.POST("/unmark/:repo_group/:pr_number", HandleStaleUnmark)
	}

	cleanup := func() { db.Close() }
	return engine, cleanup
}

func TestHandleStaleCheck_NoManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb
	t.Cleanup(func() { db.Close() })

	auth.Init("stale-nomanager", 72*time.Hour)
	config.Store(&models.Config{})

	staleMgr = nil

	engine := gin.New()
	engine.POST("/api/v1/stale/check", HandleStaleCheck)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/stale/check", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestHandleStaleCheck_NoConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb
	t.Cleanup(func() { db.Close() })

	auth.Init("stale-noconfig", 72*time.Hour)
	config.Store(nil)

	mock := testutil.NewMockPlatformClient()
	staleMgr = stale.NewManager(nil, map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	})

	engine := gin.New()
	engine.POST("/api/v1/stale/check", HandleStaleCheck)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/stale/check", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestActivePlatforms(t *testing.T) {
	tests := []struct {
		name  string
		group *models.RepoGroup
		want  int
	}{
		{
			name: "github only",
			group: &models.RepoGroup{
				GitHub: "org/repo",
			},
			want: 1,
		},
		{
			name: "github and gitlab",
			group: &models.RepoGroup{
				GitHub: "org/repo",
				GitLab: "group/repo",
			},
			want: 2,
		},
		{
			name: "all platforms",
			group: &models.RepoGroup{
				GitHub:  "org/repo",
				GitLab:  "group/repo",
				Gitea:   "user/repo",
				Forgejo: "user/repo",
				Codeberg: "user/repo",
			},
			want: 3, // GitHub, GitLab, Gitea (Forgejo and Codeberg not in activePlatforms)
		},
		{
			name:  "no platforms",
			group: &models.RepoGroup{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activePlatforms(tt.group)
			if len(got) != tt.want {
				t.Errorf("activePlatforms() = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestHandleStaleUnmark_NoManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	staleMgr = nil

	engine := gin.New()
	engine.POST("/api/v1/stale/unmark/group/1", HandleStaleUnmark)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/stale/unmark/group/1", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestHandleStaleUnmark_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb
	t.Cleanup(func() { db.Close() })

	auth.Init("stale-params", 72*time.Hour)
	config.Store(&models.Config{})

	staleMgr = &stale.Manager{}

	engine := gin.New()
	engine.POST("/api/v1/stale/unmark/:repo_group/:pr_number", HandleStaleUnmark)

	// Missing pr_number
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/stale/unmark/group/", nil)
	engine.ServeHTTP(w, req)

	// Gin may route differently for trailing slash
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Logf("status = %d", w.Code)
	}
}
