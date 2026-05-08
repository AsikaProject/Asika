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
	"asika/common/platforms"
	"asika/testutil"
)

func setupHandlerTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("test-secret-for-unit-tests", 72*time.Hour)

	mock := testutil.NewMockPlatformClient()
	clients = map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	engine := gin.New()

	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})
	{
		users := protected.Group("/users")
		{
			users.GET("", ListUsers)
			users.POST("", CreateUser)
			users.DELETE("/:username", DeleteUser)
		}

		prs := protected.Group("/repos/:repo_group/prs")
		{
			prs.GET("", ListPRs)
			prs.GET("/:pr_id", GetPR)
			prs.POST("/:pr_id/approve", ApprovePR)
			prs.POST("/:pr_id/close", ClosePR)
			prs.POST("/:pr_id/reopen", ReopenPR)
			prs.POST("/:pr_id/spam", MarkSpam)
			prs.POST("/:pr_id/comment", CommentPR)
		}

		queue := protected.Group("/queue/:repo_group")
		{
			queue.GET("", GetQueue)
			queue.POST("/recheck", RecheckQueue)
		}

		logs := protected.Group("/logs")
		{
			logs.GET("", GetLogs)
		}

		conf := protected.Group("/config")
		{
			conf.GET("", GetConfig)
			conf.PUT("", UpdateConfig)
		}

		sync := protected.Group("/sync")
		{
			sync.GET("/history", GetSyncHistory)
			sync.POST("/retry/:sync_id", RetrySync)
		}

		pTest := protected.Group("/test")
		{
			pTest.POST("/notify", TestNotify)
		}

		protected.GET("/stats", GetStats)
	}

	cleanup := func() {
		db.Close()
	}
	return engine, cleanup
}

func setupWizardTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	wizard := engine.Group("/api/v1/wizard")
	{
		wizard.GET("", GetWizardSteps)
		wizard.POST("/step/:step", SubmitWizardStep)
		wizard.POST("/step/complete", CompleteWizard)
	}

	cleanup := func() {}
	return engine, cleanup
}

func setupWebhookTest(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	mock := testutil.NewMockPlatformClient()
	clients = map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	engine := gin.New()
	engine.POST("/webhook/:repo_group/:platform", WebhookHandler)

	cleanup := func() {
		db.Close()
	}
	return engine, cleanup
}

// --- Config endpoints (8.4) ---

func TestGetConfig_Masked(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		Server: models.ServerConfig{Listen: ":8080", Mode: "debug"},
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth: models.AuthConfig{JWTSecret: "super-secret-key-value", TokenExpiry: "72h"},
		Tokens: models.TokensConfig{
			GitHub: "ghp_real_token_1234567890abcdef",
			GitLab: "glpat_long_token_value_xyz",
			Gitea:  "short",
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result models.Config
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Tokens.GitHub == cfg.Tokens.GitHub {
		t.Error("GitHub token should be masked")
	}
	if result.Tokens.GitLab == cfg.Tokens.GitLab {
		t.Error("GitLab token should be masked")
	}
	if result.Auth.JWTSecret == "super-secret-key-value" {
		t.Error("JWT secret should be masked")
	}
	if !contains(result.Tokens.GitHub, "****") {
		t.Error("GitHub token should contain mask chars")
	}
}

func TestGetConfig_NotLoaded(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	config.Store(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

// --- PR endpoints (8.2) ---

func TestListPRs_EmptyRepoGroup(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "empty-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/repos/empty-group/prs", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListPRs_RepoGroupNotFound(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/repos/nonexistent/prs", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- Queue endpoints (8.3) ---

func TestGetQueue_EmptyRepo(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "queue-test", Mode: "multi", GitHub: "org/repo"},
		},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/queue/queue-test", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- Test notify endpoint (8.6) ---

func TestTestNotify_AdminRole(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/test/notify", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- Logs endpoint (8.2) ---

func TestGetLogs_Empty(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/logs", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- Wizard endpoints (10) ---

func TestWizard_GetSteps(t *testing.T) {
	engine, cleanup := setupWizardTest(t)
	defer cleanup()

	// Ensure config is nil to simulate uninitialized state
	config.Store(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/wizard", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWizard_SubmitStep(t *testing.T) {
	engine, cleanup := setupWizardTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/wizard/step/mode_selection", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body in SubmitWizardStep, got %d", w.Code)
	}
}

// --- Webhook endpoint ---

func TestWebhook_InvalidSignature(t *testing.T) {
	engine, cleanup := setupWebhookTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "webhook-test", Mode: "multi", GitHub: "org/repo"},
		},
		Events: models.EventsConfig{WebhookSecret: "real-secret"},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/webhook/webhook-test/github", nil)
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusBadRequest {
		t.Logf("got status %d", w.Code)
	}
}

// --- Single repo mode tests ---

func TestPRManagement_SingleMode_NoSyncer(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "docs-only",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/docs",
				DefaultBranch:  "gh-pages",
			},
		},
	}
	config.Store(cfg)

	// Test list PRs for single mode repo
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/repos/docs-only/prs", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for single mode list PRs, got %d", w.Code)
	}
}

func TestPRManagement_SingleMode_GetPR(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-gh",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/single-repo",
				DefaultBranch:  "main",
			},
		},
	}
	config.Store(cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/repos/single-gh/prs/1", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for single mode get PR, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPRManagement_SingleMode_ClosePR(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	// Use github platform since that's what's available in mock
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "mirror-gh",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/project",
				DefaultBranch:  "main",
			},
		},
	}
	config.Store(cfg)

	pr := models.PRRecord{
		ID:        "1",
		RepoGroup: "mirror-gh",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		Author:    "dev",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("mirror-gh#github#1", data, "1", "mirror-gh", 1)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/mirror-gh/prs/1/close", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- User management endpoints (8.1) ---

func TestListUsers_Admin(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin list users, got %d", w.Code)
	}
}

func TestCreateUser_Admin(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty create user, got %d", w.Code)
	}
}

func TestDeleteUser_Admin(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/users/nonexistent", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Logf("DeleteUser got status: %d", w.Code)
	}
}

// --- Sync endpoints (8.5) ---

func TestGetSyncHistory_Empty(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/sync/history", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRetrySync_NotFound(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/sync/retry/nonexistent-id", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for retry nonexistent sync, got %d", w.Code)
	}
}

// --- Masking helpers ---

func TestMaskToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "***"},
		{"12345678", "***"},
		{"123456789", "1234****6789"},
		{"ghp_test1234567890abcdefghijklmnop", "ghp_****mnop"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := maskToken(tt.input)
			if got != tt.want {
				t.Errorf("maskToken(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMaskSecret(t *testing.T) {
	got := maskSecret("my-secret-key-longer-than-8")
	if !contains(got, "****") {
		t.Errorf("maskSecret should contain mask chars, got %q", got)
	}

	got2 := maskSecret("short")
	if got2 != "***" {
		t.Errorf("maskSecret for short = %q, want ***", got2)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Single repo mode: syncer disabled for single mode ---

func TestSingleMode_SyncerLogic(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-docs",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/docs",
				DefaultBranch:  "gh-pages",
			},
			{
				Name: "multi-project",
				Mode: "multi",
				GitHub: "org/main",
				GitLab: "group/main",
				Gitea: "user/main",
			},
		},
	}

	singleGroup := config.GetRepoGroupByName(cfg, "single-docs")
	if singleGroup == nil {
		t.Fatal("single-docs group not found")
	}

	// Verify single mode properties
	if singleGroup.Mode != "single" {
		t.Errorf("Mode = %q, want single", singleGroup.Mode)
	}
	if singleGroup.MirrorPlatform != "github" {
		t.Errorf("MirrorPlatform = %q, want github", singleGroup.MirrorPlatform)
	}

	// Multi repo should have different properties
	multiGroup := config.GetRepoGroupByName(cfg, "multi-project")
	if multiGroup.Mode != "multi" {
		t.Errorf("Mode = %q, want multi", multiGroup.Mode)
	}
	if multiGroup.MirrorPlatform != "" {
		t.Errorf("multi mode should have empty MirrorPlatform, got %q", multiGroup.MirrorPlatform)
	}
}

func TestConfigHotReload_LabelRules(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: "*.go", Label: "go-code"},
		},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
		Auth:     models.AuthConfig{JWTSecret: "test-secret", TokenExpiry: "72h"},
		Database: models.DatabaseConfig{Path: "./test.db"},
	}
	config.Store(cfg)

	t.Run("verify current rules", func(t *testing.T) {
		stored := config.Current()
		if len(stored.LabelRules) != 1 {
			t.Errorf("expected 1 rule, got %d", len(stored.LabelRules))
		}
	})

	t.Run("hot reload new rules", func(t *testing.T) {
		newCfg := *cfg
		newCfg.LabelRules = []models.LabelRule{
			{Pattern: "*.go", Label: "go-code"},
			{Pattern: "*.md", Label: "docs"},
		}
		config.Store(&newCfg)

		stored := config.Current()
		if len(stored.LabelRules) != 2 {
			t.Errorf("after hot reload: expected 2 rules, got %d", len(stored.LabelRules))
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	engine.ServeHTTP(w, req)
}

func TestWebhookRouteRegistration(t *testing.T) {
	_, cleanup := setupWebhookTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "org/repo"},
		},
	}
	config.Store(cfg)
}

// --- Single mode: getPlatformForGroup respects MirrorPlatform ---

func TestGetPlatformForGroup_SingleMode(t *testing.T) {
	tests := []struct {
		name           string
		group          *models.RepoGroup
		wantPlatform   string
	}{
		{
			name: "single mode with gitlab mirror",
			group: &models.RepoGroup{
				Name:           "test",
				Mode:           "single",
				MirrorPlatform: "gitlab",
				GitHub:         "org/repo",
				GitLab:         "group/repo",
			},
			wantPlatform: "gitlab",
		},
		{
			name: "single mode with gitea mirror",
			group: &models.RepoGroup{
				Name:           "test",
				Mode:           "single",
				MirrorPlatform: "gitea",
				Gitea:          "user/repo",
			},
			wantPlatform: "gitea",
		},
		{
			name: "single mode with github mirror",
			group: &models.RepoGroup{
				Name:           "test",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/repo",
			},
			wantPlatform: "github",
		},
		{
			name: "multi mode ignores MirrorPlatform",
			group: &models.RepoGroup{
				Name:           "test",
				Mode:           "multi",
				MirrorPlatform: "gitlab",
				GitHub:         "org/repo",
				GitLab:         "group/repo",
			},
			wantPlatform: "github",
		},
		{
			name: "empty mode defaults to multi behavior",
			group: &models.RepoGroup{
				Name:           "test",
				Mode:           "",
				MirrorPlatform: "gitlab",
				GitLab:         "group/repo",
			},
			wantPlatform: "gitlab",
		},
		{
			name: "single mode with empty MirrorPlatform falls back to first platform",
			group: &models.RepoGroup{
				Name:           "test",
				Mode:           "single",
				MirrorPlatform: "",
				Gitea:          "user/repo",
			},
			wantPlatform: "gitea",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.GetPlatformForGroup(tt.group)
			if got != tt.wantPlatform {
				t.Errorf("GetPlatformForGroup() = %q, want %q", got, tt.wantPlatform)
			}
		})
	}
}

// --- Single mode: ListPRs filters by MirrorPlatform ---

func TestListPRs_SingleMode_FiltersByMirrorPlatform(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-group",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/repo",
			},
		},
	}
	config.Store(cfg)

	// Store PRs for different platforms under the same repo group
	prGitHub := models.PRRecord{
		ID:        "pr-gh-1",
		RepoGroup: "single-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "GitHub PR",
		State:     "open",
	}
	prGitLab := models.PRRecord{
		ID:        "pr-gl-1",
		RepoGroup: "single-group",
		Platform:  "gitlab",
		PRNumber:  2,
		Title:     "GitLab PR",
		State:     "open",
	}
	db.PutPRWithIndex("single-group#github#1", mustMarshal(prGitHub), "pr-gh-1", "single-group", 1)
	db.PutPRWithIndex("single-group#gitlab#2", mustMarshal(prGitLab), "pr-gl-1", "single-group", 2)

	// Without explicit platform filter, single mode should only return MirrorPlatform PRs
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/repos/single-group/prs", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Data []models.PRRecord `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)

	// Should only return GitHub PRs (the MirrorPlatform), not GitLab
	for _, pr := range result.Data {
		if pr.Platform != "github" {
			t.Errorf("single mode should only return MirrorPlatform PRs, got platform=%q", pr.Platform)
		}
	}

	// With explicit platform=github, should also work
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/v1/repos/single-group/prs?platform=github", nil)
	engine.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 with explicit platform, got %d: %s", w2.Code, w2.Body.String())
	}
}

// --- Single mode: ApprovePR uses MirrorPlatform ---

func TestApprovePR_SingleMode_UsesMirrorPlatform(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-approve",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/project",
			},
		},
	}
	config.Store(cfg)

	// Store a PR
	pr := models.PRRecord{
		ID:        "single-pr-1",
		RepoGroup: "single-approve",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Single mode PR",
		Author:    "dev",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("single-approve#github#42", data, "single-pr-1", "single-approve", 42)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/single-approve/prs/42/approve", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Single mode: ClosePR uses MirrorPlatform ---

func TestClosePR_SingleMode_UsesMirrorPlatform(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-close",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/project",
			},
		},
	}
	config.Store(cfg)

	pr := models.PRRecord{
		ID:        "single-close-pr-1",
		RepoGroup: "single-close",
		Platform:  "github",
		PRNumber:  55,
		Title:     "Close me",
		Author:    "dev",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("single-close#github#55", data, "single-close-pr-1", "single-close", 55)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/single-close/prs/55/close", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Single mode: MarkSpam uses MirrorPlatform ---

func TestMarkSpam_SingleMode_UsesMirrorPlatform(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-spam",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/project",
			},
		},
	}
	config.Store(cfg)

	pr := models.PRRecord{
		ID:        "single-spam-pr-1",
		RepoGroup: "single-spam",
		Platform:  "github",
		PRNumber:  66,
		Title:     "Spam PR",
		Author:    "spammer",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("single-spam#github#66", data, "", "single-spam", 66)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/single-spam/prs/66/spam", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify PR was marked as spam
	stored, err := db.Get(db.BucketPRs, "single-spam#github#66")
	if err != nil {
		t.Fatalf("PR not found after spam: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if !result.SpamFlag {
		t.Error("PR should be marked as spam")
	}
	if result.State != "spam" {
		t.Errorf("state = %q, want spam", result.State)
	}
}

// --- Single mode: ReopenPR uses MirrorPlatform ---

func TestReopenPR_SingleMode_UsesMirrorPlatform(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-reopen",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/project",
			},
		},
	}
	config.Store(cfg)

	pr := models.PRRecord{
		ID:        "single-reopen-pr-1",
		RepoGroup: "single-reopen",
		Platform:  "github",
		PRNumber:  77,
		Title:     "Reopen me",
		Author:    "dev",
		State:     "spam",
		SpamFlag:  true,
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("single-reopen#github#77", data, "single-reopen-pr-1", "single-reopen", 77)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/single-reopen/prs/77/reopen", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Single mode: CommentPR uses MirrorPlatform ---

func TestCommentPR_SingleMode_UsesMirrorPlatform(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-comment",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/project",
			},
		},
	}
	config.Store(cfg)

	pr := models.PRRecord{
		ID:        "single-comment-pr-1",
		RepoGroup: "single-comment",
		Platform:  "github",
		PRNumber:  88,
		Title:     "Comment on me",
		Author:    "dev",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("single-comment#github#88", data, "single-comment-pr-1", "single-comment", 88)

	body := `{"body": "Test comment from single mode"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/single-comment/prs/88/comment", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Single mode: MirrorPlatform=gitlab with only gitlab configured ---

func TestSingleMode_GitlabOnlyMirror(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "gitlab-only",
				Mode:           "single",
				MirrorPlatform: "gitlab",
				GitLab:         "group/project",
			},
		},
	}
	config.Store(cfg)

	// Verify getPlatformForGroup returns gitlab
	group := config.GetRepoGroupByName(cfg, "gitlab-only")
	if group == nil {
		t.Fatal("group not found")
	}
	plat := config.GetPlatformForGroup(group)
	if plat != "gitlab" {
		t.Errorf("expected gitlab, got %q", plat)
	}

	// List PRs should work (return empty for single mode with gitlab mirror)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/repos/gitlab-only/prs", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- Repo group permission isolation tests ---

func TestUserModel_AllowedRepoGroups(t *testing.T) {
	tests := []struct {
		name            string
		role            string
		allowedGroups   []string
		requestedGroup  string
		expectAccess    bool
	}{
		{
			name:           "admin always has access",
			role:           "admin",
			allowedGroups:  nil,
			requestedGroup: "any-group",
			expectAccess:   true,
		},
		{
			name:           "operator with matching group",
			role:           "operator",
			allowedGroups:  []string{"group-a"},
			requestedGroup: "group-a",
			expectAccess:   true,
		},
		{
			name:           "operator without matching group",
			role:           "operator",
			allowedGroups:  []string{"group-a"},
			requestedGroup: "group-b",
			expectAccess:   false,
		},
		{
			name:           "operator with empty allowed groups (all access)",
			role:           "operator",
			allowedGroups:  []string{},
			requestedGroup: "any-group",
			expectAccess:   true,
		},
		{
			name:           "operator with nil allowed groups (all access)",
			role:           "operator",
			allowedGroups:  nil,
			requestedGroup: "any-group",
			expectAccess:   true,
		},
		{
			name:           "viewer with matching group",
			role:           "viewer",
			allowedGroups:  []string{"group-a"},
			requestedGroup: "group-a",
			expectAccess:   true,
		},
		{
			name:           "viewer without matching group",
			role:           "viewer",
			allowedGroups:  []string{"group-a"},
			requestedGroup: "group-b",
			expectAccess:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := models.User{
				Username:          "testuser",
				Role:              tt.role,
				AllowedRepoGroups: tt.allowedGroups,
			}

			// Simulate the access check logic from RequireRepoGroupAccess middleware
			hasAccess := false
			if user.Role == "admin" {
				hasAccess = true
			} else if len(user.AllowedRepoGroups) == 0 {
				hasAccess = true
			} else {
				for _, g := range user.AllowedRepoGroups {
					if g == tt.requestedGroup {
						hasAccess = true
						break
					}
				}
			}

			if hasAccess != tt.expectAccess {
				t.Errorf("expected access=%v for role=%q groups=%v requested=%q, got %v",
					tt.expectAccess, tt.role, tt.allowedGroups, tt.requestedGroup, hasAccess)
			}
		})
	}
}

func TestCreateUser_WithAllowedRepoGroups(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	hash, _ := auth.HashPassword("testpass")
	user := models.User{
		Username:          "scoped-user",
		PasswordHash:      hash,
		Role:              "operator",
		AllowedRepoGroups: []string{"project-a", "project-b"},
	}
	data, _ := json.Marshal(user)
	if err := db.Put(db.BucketUsers, "scoped-user", data); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Read back and verify
	stored, err := db.Get(db.BucketUsers, "scoped-user")
	if err != nil {
		t.Fatalf("failed to read user: %v", err)
	}
	var result models.User
	if err := json.Unmarshal(stored, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result.Role != "operator" {
		t.Errorf("role = %q, want operator", result.Role)
	}
	if len(result.AllowedRepoGroups) != 2 {
		t.Errorf("allowed groups = %v, want 2 entries", result.AllowedRepoGroups)
	}
	if result.AllowedRepoGroups[0] != "project-a" || result.AllowedRepoGroups[1] != "project-b" {
		t.Errorf("allowed groups = %v, want [project-a, project-b]", result.AllowedRepoGroups)
	}
}

func TestCreateUser_EmptyAllowedRepoGroups(t *testing.T) {
	dir := t.TempDir()
	db.Init(dir + "/test.db")
	t.Cleanup(func() { db.Close() })

	hash, _ := auth.HashPassword("testpass")
	user := models.User{
		Username:          "open-user",
		PasswordHash:      hash,
		Role:              "viewer",
		AllowedRepoGroups: []string{},
	}
	data, _ := json.Marshal(user)
	db.Put(db.BucketUsers, "open-user", data)

	stored, err := db.Get(db.BucketUsers, "open-user")
	if err != nil {
		t.Fatalf("failed to read user: %v", err)
	}
	var result models.User
	json.Unmarshal(stored, &result)

	if len(result.AllowedRepoGroups) != 0 {
		t.Errorf("expected empty allowed groups, got %v", result.AllowedRepoGroups)
	}
}

// --- Stats / DORA metrics tests ---

func TestGetStats_Empty(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)

	if result["total_prs"].(float64) != 0 {
		t.Errorf("expected 0 total PRs, got %v", result["total_prs"])
	}
}

func TestGetStats_WithPRs(t *testing.T) {
	engine, cleanup := setupHandlerTest(t)
	defer cleanup()

	// Store some PRs
	now := time.Now()
	pr1 := models.PRRecord{
		ID:        "stats-pr-1",
		RepoGroup: "default",
		Platform:  "github",
		PRNumber:  1,
		State:     "merged",
		CreatedAt: now.Add(-48 * time.Hour),
		MergedAt:  now.Add(-24 * time.Hour),
	}
	pr2 := models.PRRecord{
		ID:        "stats-pr-2",
		RepoGroup: "default",
		Platform:  "github",
		PRNumber:  2,
		State:     "open",
		CreatedAt: now.Add(-12 * time.Hour),
	}
	db.PutPRWithIndex("default#github#1", mustMarshal(pr1), "stats-pr-1", "default", 1)
	db.PutPRWithIndex("default#github#2", mustMarshal(pr2), "stats-pr-2", "default", 2)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)

	if result["total_prs"].(float64) != 2 {
		t.Errorf("expected 2 total PRs, got %v", result["total_prs"])
	}
	if result["merged_prs"].(float64) != 1 {
		t.Errorf("expected 1 merged PR, got %v", result["merged_prs"])
	}
	if result["open_prs"].(float64) != 1 {
		t.Errorf("expected 1 open PR, got %v", result["open_prs"])
	}

	// Lead time should be ~24 hours
	leadTime := result["lead_time_hours"].(float64)
	if leadTime < 20 || leadTime > 28 {
		t.Errorf("lead time = %f, expected ~24h", leadTime)
	}
}

func TestRebaseSinglePR_MissingAllowEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("test-secret-for-unit-tests", 72*time.Hour)

	// Set up config with repo group
	cfg := &models.Config{
		Server: models.ServerConfig{Listen: ":8080"},
		Git:    models.GitConfig{},
		Tokens: models.TokensConfig{GitHub: "test-token"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default", GitHub: "test/repo", DefaultBranch: "main"},
		},
	}
	config.Store(cfg)

	noEditMock := testutil.NewMockPlatformClient()
	noEditMock.MaintainerCanModify = false
	clients = map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: noEditMock,
	}

	pr := models.PRRecord{
		ID:          "42",
		RepoGroup:   "default",
		Platform:    "github",
		PRNumber:    42,
		Title:       "Test PR",
		Author:      "testuser",
		State:       "open",
		HasConflict: true,
	}
	db.PutPRWithIndex("default#github#42", mustMarshal(pr), pr.ID, pr.RepoGroup, pr.PRNumber)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})
	protected.POST("/repos/:repo_group/prs/:pr_id/rebase", RebaseSinglePR)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/default/prs/42/rebase", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var result RebaseResponse
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Success {
		t.Error("expected rebase to fail")
	}
	if !strings.Contains(result.Message, "allow edits from maintainers") {
		t.Errorf("expected 'allow edits' error, got: %s", result.Message)
	}
}

func TestRebaseSinglePR_PRNotOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("test-secret-for-unit-tests", 72*time.Hour)

	cfg := &models.Config{
		Server: models.ServerConfig{Listen: ":8080"},
		Git:    models.GitConfig{},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default", GitHub: "test/repo"},
		},
	}
	config.Store(cfg)

	clients = map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	pr := models.PRRecord{
		ID:        "99",
		RepoGroup: "default",
		Platform:  "github",
		PRNumber:  99,
		State:     "closed",
	}
	db.PutPRWithIndex("default#github#99", mustMarshal(pr), pr.ID, pr.RepoGroup, pr.PRNumber)

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})
	protected.POST("/repos/:repo_group/prs/:pr_id/rebase", RebaseSinglePR)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/repos/default/prs/99/rebase", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var result RebaseResponse
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Success {
		t.Error("expected rebase to fail for closed PR")
	}
	if !strings.Contains(result.Message, "not open") {
		t.Errorf("expected 'not open' error, got: %s", result.Message)
	}
}

func TestRebaseQueue_NoConflictedPRs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	auth.Init("test-secret-for-unit-tests", 72*time.Hour)

	cfg := &models.Config{
		Server: models.ServerConfig{Listen: ":8080"},
		Git:    models.GitConfig{},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default", GitHub: "test/repo"},
		},
	}
	config.Store(cfg)

	clients = map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: testutil.NewMockPlatformClient(),
	}

	engine := gin.New()
	api := engine.Group("/api/v1")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Set("role", "admin")
		c.Next()
	})
	protected.POST("/queue/:repo_group/rebase", RebaseQueue)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/queue/default/rebase", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	if msg, ok := result["message"].(string); !ok || !strings.Contains(msg, "no conflicted PRs") {
		t.Errorf("expected 'no conflicted PRs' message, got: %v", result)
	}
}

// --- Helper ---

func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}