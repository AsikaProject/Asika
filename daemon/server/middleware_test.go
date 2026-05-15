package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func TestExtractToken_BearerPrefixRequired(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid bearer", "Bearer token123", "token123"},
		{"lowercase bearer", "bearer token456", "token456"},
		{"no prefix", "token789", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
		{"empty header", "", ""},
		{"bearer with empty token", "Bearer ", ""},
		{"single word", "Bearer", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = &http.Request{Header: make(http.Header)}
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}

			got := extractToken(c)
			if got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractToken_FromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}
	c.Request.Header.Set("Authorization", "Bearer test-token")

	token := extractToken(c)
	if token != "test-token" {
		t.Errorf("extractToken() = %q, want test-token", token)
	}
}

func TestExtractToken_FromCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}
	c.Request.AddCookie(&http.Cookie{
		Name:  "asika_token",
		Value: "cookie-token",
	})

	token := extractToken(c)
	if token != "cookie-token" {
		t.Errorf("extractToken() = %q, want cookie-token", token)
	}
}

func TestExtractToken_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}

	token := extractToken(c)
	if token != "" {
		t.Errorf("extractToken() = %q, want empty string", token)
	}
}

func TestRequireSpaceAccess_AdminBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Set("role", "admin")
	c.Set("username", "admin")

	mw(c)

	if c.IsAborted() {
		t.Error("admin should bypass space access check")
	}
}

func TestRequireSpaceAccess_NoRepoGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/logs", nil)
	c.Set("role", "viewer")
	c.Set("username", "alice")

	mw(c)

	if c.IsAborted() {
		t.Error("empty repo_group should pass through")
	}
}

func TestRequireSpaceAccess_NoSpaceOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "test"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "frontend", GitHub: "org/frontend"},
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
		Events:     models.EventsConfig{Mode: "webhook", PollingInterval: "30s"},
	}
	config.Store(cfg)

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "frontend"}}
	c.Set("role", "viewer")
	c.Set("username", "alice")

	mw(c)

	if c.IsAborted() {
		t.Errorf("repo group not owned by any space should pass through, got status %d", w.Code)
	}
}

func TestRequireSpaceAccess_MemberAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "test"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "frontend", GitHub: "org/frontend"},
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
		Events:     models.EventsConfig{Mode: "webhook", PollingInterval: "30s"},
	}
	config.Store(cfg)

	db.PutTeamSpace(&models.TeamSpace{
		Name:        "space-a",
		Description: "test space",
		RepoGroups:  []string{"frontend"},
	})
	db.PutSpaceMember("space-a", "alice", "space_viewer")

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "frontend"}}
	c.Set("role", "viewer")
	c.Set("username", "alice")

	mw(c)

	if c.IsAborted() {
		t.Errorf("space member should be allowed, got status %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireSpaceAccess_NonMemberDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "test"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "frontend", GitHub: "org/frontend"},
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
		Events:     models.EventsConfig{Mode: "webhook", PollingInterval: "30s"},
	}
	config.Store(cfg)

	db.PutTeamSpace(&models.TeamSpace{
		Name:        "space-a",
		Description: "test space",
		RepoGroups:  []string{"frontend"},
	})
	db.PutSpaceMember("space-a", "alice", "space_viewer")

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "frontend"}}
	c.Set("role", "viewer")
	c.Set("username", "bob")

	mw(c)

	if !c.IsAborted() {
		t.Error("non-member should be aborted")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("non-member should get 403, got %d", w.Code)
	}
}

func TestRequireSpaceAccess_NilUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "frontend"}}
	c.Set("role", "viewer")

	mw(c)

	if !c.IsAborted() {
		t.Error("nil username should be aborted")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("nil username should get 401, got %d", w.Code)
	}
}

func TestRequireSpaceAccess_SetsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "test"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "frontend", GitHub: "org/frontend"},
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
		Events:     models.EventsConfig{Mode: "webhook", PollingInterval: "30s"},
	}
	config.Store(cfg)

	db.PutTeamSpace(&models.TeamSpace{
		Name:       "space-a",
		RepoGroups: []string{"frontend"},
	})
	db.PutSpaceMember("space-a", "alice", "space_admin")

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "frontend"}}
	c.Set("role", "viewer")
	c.Set("username", "alice")

	mw(c)

	if c.IsAborted() {
		t.Fatalf("space member should be allowed, got status %d", w.Code)
	}

	spaceVal, spaceOK := c.Get("space_name")
	if !spaceOK || spaceVal.(string) != "space-a" {
		t.Errorf("expected space_name=space-a, got %v (ok=%v)", spaceVal, spaceOK)
	}
	roleVal, roleOK := c.Get("space_role")
	if !roleOK || roleVal.(string) != "space_admin" {
		t.Errorf("expected space_role=space_admin, got %v (ok=%v)", roleVal, roleOK)
	}
}

func TestRequireSpaceAccess_NoSpaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "test"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "frontend", GitHub: "org/frontend"},
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
		Events:     models.EventsConfig{Mode: "webhook", PollingInterval: "30s"},
	}
	config.Store(cfg)

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/frontend/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "frontend"}}
	c.Set("role", "viewer")
	c.Set("username", "alice")

	mw(c)

	if c.IsAborted() {
		t.Errorf("no spaces defined should pass through, got status %d", w.Code)
	}
}

func TestRequireSpaceAccess_GroupNotInConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testutil.NewTestDB(t)
	defer db.Close()

	cfg := &models.Config{
		Server:   models.ServerConfig{Listen: ":8080"},
		Database: models.DatabaseConfig{Type: "bbolt", Path: "/tmp/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "test"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "backend", GitHub: "org/backend"},
		},
		QuietHours: models.QuietHoursConfig{},
		MergeQueue: models.MergeQueueConfig{RequiredApprovals: 1},
		WorkerPool: models.WorkerPoolConfig{MinWorkers: 2, MaxWorkers: 8},
		Events:     models.EventsConfig{Mode: "webhook", PollingInterval: "30s"},
	}
	config.Store(cfg)

	mw := RequireSpaceAccess()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/repos/unknown/prs", nil)
	c.Params = []gin.Param{{Key: "repo_group", Value: "unknown"}}
	c.Set("role", "viewer")
	c.Set("username", "alice")

	mw(c)

	if c.IsAborted() {
		t.Errorf("unknown repo group not in any space should pass through, got status %d", w.Code)
	}
}
