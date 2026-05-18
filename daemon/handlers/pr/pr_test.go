package pr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/testutil"
)

func setupTest(t *testing.T) (*testutil.MockPlatformClient, func()) {
	t.Helper()
	testutil.NewTestDB(t)

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:          "test-group",
				Mode:          "multi",
				GitHub:        "org/repo",
				DefaultBranch: "main",
			},
		},
	}
	config.Store(cfg)

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}
	InitClients(clients)

	cleanup := func() {
		InitClients(nil)
		InitQueueMgr(nil)
		db.Close()
	}
	return mock, cleanup
}

func TestGetClientForGroup(t *testing.T) {
	mock, cleanup := setupTest(t)
	defer cleanup()

	group := &models.RepoGroup{Name: "test-group"}

	client := GetClientForGroup(group, "github")
	if client == nil {
		t.Fatal("expected non-nil client for github")
	}
	if client != mock {
		t.Error("expected mock client")
	}

	client = GetClientForGroup(group, "unknown")
	if client != nil {
		t.Error("expected nil client for unknown platform")
	}

	client = GetClientForGroup(group, "")
	if client == nil {
		t.Fatal("expected non-nil client for empty platform (defaults to github)")
	}
}

func TestGetClientForGroup_NilClients(t *testing.T) {
	InitClients(nil)
	defer InitClients(nil)

	group := &models.RepoGroup{Name: "test-group"}
	client := GetClientForGroup(group, "github")
	if client != nil {
		t.Error("expected nil client when clients map is nil")
	}
}

func TestAddToQueue_NilManager(t *testing.T) {
	InitQueueMgr(nil)
	defer InitQueueMgr(nil)

	pr := &models.PRRecord{ID: "pr-1", RepoGroup: "test-group"}
	err := AddToQueue(pr)
	if err != nil {
		t.Errorf("expected nil error when queue manager is nil, got: %v", err)
	}
}

func TestAddToQueue_WithManager(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	mgr := queue.NewManager(&models.Config{}, nil)
	InitQueueMgr(mgr)
	defer InitQueueMgr(nil)

	pr := &models.PRRecord{
		ID:        "pr-1",
		RepoGroup: "test-group",
		Platform:  "github",
		State:     "open",
	}
	err := AddToQueue(pr)
	if err != nil {
		t.Fatalf("AddToQueue failed: %v", err)
	}

	items, err := mgr.GetQueueItems("test-group")
	if err != nil {
		t.Fatalf("GetQueueItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 queue item, got %d", len(items))
	}
	if items[0].PRID != "pr-1" {
		t.Errorf("PRID = %q, want %q", items[0].PRID, "pr-1")
	}
}

func TestRemoveFromQueue_NilManager(t *testing.T) {
	InitQueueMgr(nil)
	defer InitQueueMgr(nil)

	err := RemoveFromQueue("test-group", "pr-1")
	if err != nil {
		t.Errorf("expected nil error when queue manager is nil, got: %v", err)
	}
}

func TestClearQueue_NilManager(t *testing.T) {
	InitQueueMgr(nil)
	defer InitQueueMgr(nil)

	count, err := ClearQueue("test-group")
	if count != 0 || err != nil {
		t.Errorf("expected (0, nil), got (%d, %v)", count, err)
	}
}

func TestClosePR_RepoGroupNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/nonexistent/prs/1/close", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "nonexistent"}, {Key: "pr_id", Value: "1"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/close", ClosePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestClosePR_InvalidPRID(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/test-group/prs/abc/close", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}, {Key: "pr_id", Value: "abc"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/close", ClosePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestApprovePR_RepoGroupNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/nonexistent/prs/1/approve", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "nonexistent"}, {Key: "pr_id", Value: "1"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/approve", ApprovePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestApprovePR_InvalidPRID(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/test-group/prs/xyz/approve", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}, {Key: "pr_id", Value: "xyz"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/approve", ApprovePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestReopenPR_RepoGroupNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/nonexistent/prs/1/reopen", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "nonexistent"}, {Key: "pr_id", Value: "1"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/reopen", ReopenPR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestReopenPR_InvalidPRID(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/test-group/prs/abc/reopen", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}, {Key: "pr_id", Value: "abc"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/reopen", ReopenPR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMarkSpam_RepoGroupNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/nonexistent/prs/1/spam", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "nonexistent"}, {Key: "pr_id", Value: "1"}}

	r.POST("/api/v1/repos/:repo_group/prs/:pr_id/spam", MarkSpam)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestBatchLabelPR_RepoGroupNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/nonexistent/prs/batch/label",
		strings.NewReader(`{"pr_ids":["1"],"label":"bug"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "repo_group", Value: "nonexistent"}}

	r.POST("/api/v1/repos/:repo_group/prs/batch/label", BatchLabelPR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestBatchApprovePR_InvalidJSON(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/repos/test-group/prs/batch/approve",
		strings.NewReader(`{invalid`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}}

	r.POST("/api/v1/repos/:repo_group/prs/batch/approve", BatchApprovePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetSyncer_Nil(t *testing.T) {
	syncerRef = nil
	defer func() { syncerRef = nil }()

	s := GetSyncer()
	if s != nil {
		t.Error("expected nil syncer")
	}
}

func TestGetQueueMgr_Nil(t *testing.T) {
	queueMgr = nil
	defer func() { queueMgr = nil }()

	m := GetQueueMgr()
	if m != nil {
		t.Error("expected nil queue manager")
	}
}

func TestTriggerQueueCheck_NilManager(t *testing.T) {
	queueMgr = nil
	defer func() { queueMgr = nil }()

	TriggerQueueCheck()
}

func TestRecheckQueue_NilManager(t *testing.T) {
	queueMgr = nil
	defer func() { queueMgr = nil }()

	RecheckQueue()
}
