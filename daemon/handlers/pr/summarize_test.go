package pr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

func TestSummarizePR_RepoGroupNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/repos/nonexistent/prs/1/summary", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "nonexistent"}, {Key: "pr_id", Value: "1"}}

	r.GET("/api/v1/repos/:repo_group/prs/:pr_id/summary", SummarizePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSummarizePR_PRNotFound(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/repos/test-group/prs/nonexistent/summary", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}, {Key: "pr_id", Value: "nonexistent"}}

	r.GET("/api/v1/repos/:repo_group/prs/:pr_id/summary", SummarizePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSummarizePR_ExistingBotSummary(t *testing.T) {
	mock, cleanup := setupTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:        "test-pr-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  42,
		State:     "open",
		Title:     "Test PR",
	}
	data, _ := json.Marshal(pr)
	if err := db.PutPRWithIndex("test-group#github#42", data, pr.ID, pr.RepoGroup, pr.PRNumber); err != nil {
		t.Fatalf("PutPRWithIndex failed: %v", err)
	}

	mock.PRComments = []models.PRComment{
		{
			ID:        "1",
			Author:    "some-user",
			Body:      "Looks good to me!",
			CreatedAt: time.Now(),
			IsBot:     false,
		},
		{
			ID:        "2",
			Author:    "asika-bot",
			Body:      "## AI Summary\nThis PR adds a new feature.\n- Added foo\n- Updated bar",
			CreatedAt: time.Now(),
			IsBot:     true,
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/repos/test-group/prs/test-pr-1/summary", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}, {Key: "pr_id", Value: "test-pr-1"}}

	r.GET("/api/v1/repos/:repo_group/prs/:pr_id/summary", SummarizePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp summaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Generated {
		t.Error("expected Generated=false for existing bot summary")
	}
	if resp.Source != "asika-bot" {
		t.Errorf("Source = %q, want %q", resp.Source, "asika-bot")
	}
}

func TestSummarizePR_NoAISummaryConfig(t *testing.T) {
	mock, cleanup := setupTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:        "test-pr-2",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  43,
		State:     "open",
		Title:     "Test PR",
	}
	data, _ := json.Marshal(pr)
	if err := db.PutPRWithIndex("test-group#github#43", data, pr.ID, pr.RepoGroup, pr.PRNumber); err != nil {
		t.Fatalf("PutPRWithIndex failed: %v", err)
	}

	mock.PRComments = []models.PRComment{
		{
			ID:        "1",
			Author:    "developer",
			Body:      "Nice work!",
			CreatedAt: time.Now(),
			IsBot:     false,
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/repos/test-group/prs/test-pr-2/summary", nil)
	c.Params = gin.Params{{Key: "repo_group", Value: "test-group"}, {Key: "pr_id", Value: "test-pr-2"}}

	r.GET("/api/v1/repos/:repo_group/prs/:pr_id/summary", SummarizePR)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp summaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Generated {
		t.Error("expected Generated=false when AI summary disabled")
	}
	if resp.Summary != "" {
		t.Errorf("Summary = %q, want empty", resp.Summary)
	}
}

func TestGetSummaryConfig_NilConfig(t *testing.T) {
	// Clear config
	config.Store(nil)
	defer config.Store(nil)

	cfg := getSummaryConfig()
	if cfg != nil {
		t.Errorf("expected nil config, got %+v", cfg)
	}
}

func TestIsBotSummary_ByPrefix(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantBot bool
	}{
		{"## AI Summary prefix", "## AI Summary\nThis PR adds...", true},
		{"## Summary prefix", "## Summary\n- Added foo", true},
		{"### AI-Generated Summary", "### AI-Generated Summary\nsome content", true},
		{"**AI Summary**", "**AI Summary**\ntext", true},
		{"No known prefix", "This is a normal comment", false},
		{"Short prefix match", "#", false},
		{"## AI Summary alone", "## AI Summary", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := &models.PRComment{Body: tt.body, Author: "user"}
			if got := isBotSummary(comment, nil); got != tt.wantBot {
				t.Errorf("isBotSummary = %v, want %v", got, tt.wantBot)
			}
		})
	}
}

func TestIsBotSummary_ByBotUser(t *testing.T) {
	tests := []struct {
		name    string
		author  string
		body    string
		cfg     *models.AISummaryConfig
		wantBot bool
	}{
		{"asika-bot with markdown body", "asika-bot", "# Changes\nSome significant changes that make this longer than fifty characters threshold", nil, true},
		{"asika-bot short body", "asika-bot", "Short", nil, false},
		{"asika-bot no markdown", "asika-bot", "Just a plain sentence that is longer than 50 characters but has no markdown", nil, false},
		{"github-actions[bot] with bullets", "github-actions[bot]", "# Summary\n- Item 1\n- Item 2\n- Item 3 that makes this body longer than fifty characters total", nil, true},
		{"custom bot user", "my-bot", "## Changes\n- Significant change item that pushes past the fifty character boundary easily", &models.AISummaryConfig{BotUsers: []string{"my-bot"}}, true},
		{"normal user", "developer", "## AI Summary\ncontent", nil, true},
		{"unknown user no match", "stranger", "normal comment that is long enough but user is not a bot", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := &models.PRComment{Body: tt.body, Author: tt.author}
			if got := isBotSummary(comment, tt.cfg); got != tt.wantBot {
				t.Errorf("isBotSummary = %v, want %v", got, tt.wantBot)
			}
		})
	}
}

func TestIsBotSummary_ByIsBotFlag(t *testing.T) {
	tests := []struct {
		name    string
		isBot   bool
		body    string
		wantBot bool
	}{
		{"bot flag long body", true, "This is a long body that exceeds the fifty character threshold and should be detected as a summary", true},
		{"bot flag short body", true, "Short body", false},
		{"not bot", false, "Long body here that exceeds fifty characters but is bot flagged false", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := &models.PRComment{Body: tt.body, Author: "somebot", IsBot: tt.isBot}
			if got := isBotSummary(comment, nil); got != tt.wantBot {
				t.Errorf("isBotSummary = %v, want %v", got, tt.wantBot)
			}
		})
	}
}

func TestGenerateSummary_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"Generated summary text"}}]}`))
	}))
	defer server.Close()

	cfg := &models.AISummaryConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	}
	pr := &models.PRRecord{Title: "Test PR"}
	diffFiles := []models.DiffFile{
		{Filename: "main.go", Status: "modified", Additions: 10, Deletions: 2, Patch: "diff --git a/main.go b/main.go\nindex abc..def\n--- a/main.go\n+++ b/main.go\n@@ -1,5 +1,10 @@\n+new line\n"},
	}
	commits := []string{"abc123", "def456"}
	body := "This PR adds a feature"

	result, err := generateSummary(cfg, pr, diffFiles, commits, body)
	if err != nil {
		t.Fatalf("generateSummary failed: %v", err)
	}
	if result != "Generated summary text" {
		t.Errorf("result = %q, want %q", result, "Generated summary text")
	}
}

func TestGenerateSummary_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":{"message":"API error"}}`))
	}))
	defer server.Close()

	cfg := &models.AISummaryConfig{
		APIKey:  "key",
		BaseURL: server.URL,
		Model:   "m",
	}
	_, err := generateSummary(cfg, &models.PRRecord{}, nil, nil, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateSummary_MaxDiffLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"short"}}]}`))
	}))
	defer server.Close()

	cfg := &models.AISummaryConfig{
		APIKey:        "key",
		BaseURL:       server.URL,
		Model:         "m",
		MaxDiffLength: 50,
	}
	largeDiff := models.DiffFile{
		Filename: "large.go",
		Status:   "modified",
		Patch:    string(make([]byte, 1000)),
	}
	_, err := generateSummary(cfg, &models.PRRecord{}, []models.DiffFile{largeDiff}, nil, "")
	if err != nil {
		t.Fatalf("generateSummary with small max diff: %v", err)
	}
}
