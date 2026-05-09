package syncer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/common/utils"
	"asika/testutil"
)

func setupSpamTest(t *testing.T) (*SpamDetector, *testutil.MockPlatformClient, func()) {
	t.Helper()

	testutil.NewTestDB(t)

	cfg := &models.Config{
		Spam: models.SpamConfig{
			Enabled:          true,
			TimeWindow:       "10m",
			Threshold:        3,
			TriggerOnAuthor:  true,
			TriggerOnTitleKw: []string{"spam", "buy now", "click here"},
		},
		Notify: []models.NotifyConfig{},
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "test-group",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "owner/repo",
				DefaultBranch:  "main",
			},
		},
	}

	config.Store(cfg)
	events.Init()

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	sd := NewSpamDetectorWithClients(cfg, clients)

	cleanup := func() {
		db.Close()
	}

	return sd, mock, cleanup
}

func TestDetectSpamByAuthor(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	prs := []*models.PRRecord{
		{ID: "a", Author: "bot1", Title: "fix typo", PRNumber: 1, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "bot1", Title: "update docs", PRNumber: 2, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "c", Author: "bot1", Title: "minor change", PRNumber: 3, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "d", Author: "real-user", Title: "great feature", PRNumber: 4, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)

	if len(spam) != 3 {
		t.Errorf("expected 3 spam PRs from bot1, got %d", len(spam))
	}

	for _, s := range spam {
		if s.Author == "real-user" {
			t.Errorf("real-user should not be marked as spam")
		}
	}
}

func TestDetectSpamByKeyword(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	prs := []*models.PRRecord{
		{ID: "a", Author: "user1", Title: "fix typo", PRNumber: 1, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "user2", Title: "Buy Now offer!!!", PRNumber: 2, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "c", Author: "user3", Title: "click here for deal", PRNumber: 3, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "d", Author: "user4", Title: "SPAM promotion", PRNumber: 4, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)

	if len(spam) != 3 {
		t.Errorf("expected 3 spam PRs by keyword, got %d", len(spam))
	}

	for _, s := range spam {
		if s.ID == "a" {
			t.Errorf("PR 'a' should not be spam by keyword")
		}
	}
}

func TestDetectSpamDisabled(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	sd.cfg.Spam.Enabled = false

	prs := []*models.PRRecord{
		{ID: "a", Author: "bot1", Title: "spam", PRNumber: 1, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "bot1", Title: "also spam", PRNumber: 2, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "c", Author: "bot1", Title: "even more spam", PRNumber: 3, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "d", Author: "bot1", Title: "final spam", PRNumber: 4, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)
	// Even thought disabled, detectSpam still works but Scan() checks cfg.Spam.Enabled first

	if len(spam) != 4 {
		t.Errorf("expected 4 spam PRs (detection ignores enabled flag, Scan checks it), got %d", len(spam))
	}
}

func TestHandleSpam(t *testing.T) {
	sd, mock, cleanup := setupSpamTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:        "test-id",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Buy Now cheap!",
		Author:    "spammer",
		State:     "open",
	}

	sd.HandleSpam(pr, "test-group")

	if !pr.SpamFlag {
		t.Errorf("expected SpamFlag to be true")
	}

	if pr.State != "spam" {
		t.Errorf("expected State to be 'spam', got %q", pr.State)
	}

	ctx := context.Background()
	prs, _ := mock.ListPRs(ctx, "owner", "repo", "")

	_ = prs

	key := "test-group#github#42"
	data, err := db.Get(db.BucketPRs, key)
	if err != nil {
		t.Fatalf("failed to get PR from DB: %v", err)
	}

	var stored models.PRRecord
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("failed to unmarshal stored PR: %v", err)
	}

	if !stored.SpamFlag {
		t.Errorf("stored PR should have SpamFlag=true")
	}

	if stored.State != "spam" {
		t.Errorf("stored PR should have state='spam', got %q", stored.State)
	}
}

func TestDetectSpamCombinedRules(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	prs := []*models.PRRecord{
		{ID: "a", Author: "bot1", Title: "fix typo", PRNumber: 1, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "bot1", Title: "update docs", PRNumber: 2, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "c", Author: "bot1", Title: "minor change", PRNumber: 3, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "d", Author: "user2", Title: "Buy Now offer!!!", PRNumber: 4, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)

	if len(spam) != 4 {
		t.Errorf("expected 4 spam PRs (3 by author + 1 by keyword), got %d", len(spam))
	}
}

func TestDetectSpamBelowThreshold(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	prs := []*models.PRRecord{
		{ID: "a", Author: "bot1", Title: "fix typo", PRNumber: 1, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "bot1", Title: "update docs", PRNumber: 2, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)

	if len(spam) != 0 {
		t.Errorf("expected 0 spam PRs (below threshold of 3), got %d", len(spam))
	}
}

func TestDetectSpamEmptyList(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	spam := sd.detectSpam([]*models.PRRecord{})

	if len(spam) != 0 {
		t.Errorf("expected 0 spam PRs for empty list, got %d", len(spam))
	}
}

func TestDetectSpamCaseInsensitiveKeyword(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	prs := []*models.PRRecord{
		{ID: "a", Author: "user1", Title: "BUY NOW cheap", PRNumber: 1, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "user2", Title: "Click Here for deals", PRNumber: 2, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "c", Author: "user3", Title: "this is SPAM", PRNumber: 3, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)

	if len(spam) != 3 {
		t.Errorf("expected 3 spam PRs (case insensitive), got %d", len(spam))
	}
}

func TestDetectSpamAuthorDisabled(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	sd.cfg.Spam.TriggerOnAuthor = false

	now := time.Now()
	prs := []*models.PRRecord{
		{ID: "a", Author: "bot1", Title: "fix typo", PRNumber: 1, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "b", Author: "bot1", Title: "update docs", PRNumber: 2, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
		{ID: "c", Author: "bot1", Title: "minor change", PRNumber: 3, CreatedAt: now, RepoGroup: "test-group", Platform: "github", State: "open"},
	}

	spam := sd.detectSpam(prs)

	if len(spam) != 0 {
		t.Errorf("expected 0 spam PRs (author detection disabled), got %d", len(spam))
	}
}

func TestScanDisabled(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	sd.cfg.Spam.Enabled = false

	// Scan should return immediately without error
	sd.Scan()
}

func TestScanWithNoPRs(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	// Scan with empty DB should not panic
	sd.Scan()
}

func TestScanWithSpamPRs(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	pr := &models.PRRecord{
		ID:        "scan-spam-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  50,
		Title:     "buy now cheap",
		Author:    "spammer",
		State:     "open",
		CreatedAt: now,
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#50", data)

	sd.Scan()

	// Verify PR was marked as spam
	stored, err := db.Get(db.BucketPRs, "test-group#github#50")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if !result.SpamFlag {
		t.Error("PR should have been marked as spam by Scan")
	}
}

func TestGetPRsAfter(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()
	oldTime := now.Add(-2 * time.Hour)

	// Store a recent PR
	recent := &models.PRRecord{
		ID:        "recent-pr",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  60,
		Title:     "Recent PR",
		Author:    "dev1",
		State:     "open",
		CreatedAt: now,
	}
	data, _ := json.Marshal(recent)
	db.Put(db.BucketPRs, "test-group#github#60", data)

	// Store an old PR
	old := &models.PRRecord{
		ID:        "old-pr",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  61,
		Title:     "Old PR",
		Author:    "dev2",
		State:     "open",
		CreatedAt: oldTime,
	}
	data, _ = json.Marshal(old)
	db.Put(db.BucketPRs, "test-group#github#61", data)

	prs := sd.getPRsAfter(now.Add(-1 * time.Hour))

	if len(prs) != 1 {
		t.Errorf("expected 1 recent PR, got %d", len(prs))
	}
	if len(prs) > 0 && prs[0].ID != "recent-pr" {
		t.Errorf("expected recent-pr, got %s", prs[0].ID)
	}
}

func TestGetPRsAfter_ExcludesSpam(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	now := time.Now()

	pr := &models.PRRecord{
		ID:        "already-spam",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  70,
		Title:     "Already spam",
		Author:    "spammer",
		State:     "spam",
		SpamFlag:  true,
		CreatedAt: now,
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#70", data)

	prs := sd.getPRsAfter(now.Add(-1 * time.Hour))

	if len(prs) != 0 {
		t.Errorf("expected 0 PRs (spam excluded), got %d", len(prs))
	}
}

func TestHandleSpam_NilClients(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	sd.clients = nil

	pr := &models.PRRecord{
		ID:        "nil-clients-pr",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  80,
		Title:     "Nil clients test",
		Author:    "spammer",
		State:     "open",
	}

	// Should not panic with nil clients
	sd.HandleSpam(pr, "test-group")

	if !pr.SpamFlag {
		t.Error("SpamFlag should be true even with nil clients")
	}
}

func TestHandleSpam_GroupNotFound(t *testing.T) {
	sd, _, cleanup := setupSpamTest(t)
	defer cleanup()

	pr := &models.PRRecord{
		ID:        "no-group-pr",
		RepoGroup: "nonexistent",
		Platform:  "github",
		PRNumber:  81,
		Title:     "No group test",
		Author:    "spammer",
		State:     "open",
	}

	// Should not panic with nonexistent group
	sd.HandleSpam(pr, "nonexistent")
}

func TestNewSpamDetector(t *testing.T) {
	cfg := &models.Config{}
	sd := NewSpamDetector(cfg)
	if sd == nil {
		t.Fatal("NewSpamDetector returned nil")
	}
	if sd.clients != nil {
		t.Error("clients should be nil initially")
	}
}

func TestSetClients(t *testing.T) {
	cfg := &models.Config{}
	sd := NewSpamDetector(cfg)

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}
	sd.SetClients(clients)

	if sd.clients == nil {
		t.Error("clients should be set")
	}
}

func TestStop(t *testing.T) {
	cfg := &models.Config{}
	sd := NewSpamDetector(cfg)

	// Stop should not panic
	sd.Stop()
}

func TestStopChan(t *testing.T) {
	cfg := &models.Config{}
	sd := NewSpamDetector(cfg)

	ch := sd.StopChan()
	if ch == nil {
		t.Fatal("StopChan should not return nil")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		defaultD time.Duration
		want     time.Duration
	}{
		{"10m", 5 * time.Minute, 10 * time.Minute},
		{"1h", 30 * time.Minute, 1 * time.Hour},
		{"", 15 * time.Minute, 15 * time.Minute},
		{"invalid", 20 * time.Minute, 20 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := utils.ParseDuration(tt.input, tt.defaultD)
			if got != tt.want {
				t.Errorf("ParseDuration(%q, %v) = %v, want %v", tt.input, tt.defaultD, got, tt.want)
			}
		})
	}
}
