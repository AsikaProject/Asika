package telegram

import (
	"encoding/json"
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
	"asika/daemon/queue"
	"asika/daemon/syncer"
	"asika/testutil"
)

func setupBotTest(t *testing.T) (*Bot, func()) {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	db.DB = tdb
	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "owner/repo"},
		},
		Telegram: models.TelegramConfig{
			Enabled: true, Token: "test-token", AdminIDs: []int64{12345, 67890},
		},
	}
	config.Store(cfg)
	qm := queue.NewManager(cfg, clients)
	s := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)
	bot := &Bot{
		bot: nil, cfg: cfg, clients: clients, queueMgr: qm, syncerRef: s, spamDetector: sd,
		adminIDs: map[int64]bool{12345: true, 67890: true}, stop: make(chan struct{}),
	}
	return bot, func() { db.Close() }
}

func TestBotCreation(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if len(bot.adminIDs) != 2 {
		t.Errorf("expected 2 admin IDs, got %d", len(bot.adminIDs))
	}
}

func TestIsAdmin_EmptyAdminIDs(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	bot.adminIDs = map[int64]bool{}
	if !bot.isAdmin(nil) {
		t.Error("with empty adminIDs, everyone should be admin")
	}
}

func TestGetPRByID(t *testing.T) {
	_, cleanup := setupBotTest(t)
	defer cleanup()
	pr := models.PRRecord{
		ID: "pr-abc-123", RepoGroup: "test-group", Platform: "github",
		PRNumber: 42, Title: "Fix critical bug", Author: "dev1", State: "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#42", data)

	t.Run("find by ID", func(t *testing.T) {
		found, err := commonutil.GetPRByID("test-group", "pr-abc-123")
		if err != nil {
			t.Fatalf("GetPRByID failed: %v", err)
		}
		if found.Title != "Fix critical bug" {
			t.Errorf("Title = %q, want %q", found.Title, "Fix critical bug")
		}
	})
	t.Run("find by number", func(t *testing.T) {
		found, err := commonutil.GetPRByID("test-group", "42")
		if err != nil {
			t.Fatalf("GetPRByID by number failed: %v", err)
		}
		if found.PRNumber != 42 {
			t.Errorf("PRNumber = %d, want 42", found.PRNumber)
		}
	})
	t.Run("not found", func(t *testing.T) {
		_, err := commonutil.GetPRByID("test-group", "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent PR")
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := commonutil.Truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestGetClientForPlatform(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	if bot.clients == nil {
		t.Fatal("clients should not be nil")
	}
	if _, ok := bot.clients[platforms.PlatformGitHub]; !ok {
		t.Error("expected github client")
	}
	if _, ok := bot.clients["unknown"]; ok {
		t.Error("expected no unknown platform client")
	}
}

func TestBotStop(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	bot.Stop()
}

func TestMarkSpamViaBotLogic(t *testing.T) {
	_, cleanup := setupBotTest(t)
	defer cleanup()
	pr := models.PRRecord{
		ID: "spam-telegram-pr", RepoGroup: "test-group", Platform: "github",
		PRNumber: 99, Title: "Buy cheap meds", Author: "spam_bot", State: "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#99", data)
	found, err := commonutil.GetPRByID("test-group", "99")
	if err != nil {
		t.Fatalf("GetPRByID failed: %v", err)
	}
	found.SpamFlag = true
	found.State = "spam"
	updated, _ := json.Marshal(found)
	db.Put(db.BucketPRs, "test-group#github#99", updated)
	found2, err := commonutil.GetPRByID("test-group", "spam-telegram-pr")
	if err != nil {
		t.Fatalf("GetPRByID after spam failed: %v", err)
	}
	if !found2.SpamFlag {
		t.Error("expected SpamFlag=true")
	}
}

func TestSpamReopenViaBotLogic(t *testing.T) {
	_, cleanup := setupBotTest(t)
	defer cleanup()
	pr := models.PRRecord{
		ID: "reopen-telegram-pr", RepoGroup: "test-group", Platform: "github",
		PRNumber: 88, Title: "Legit PR marked spam", Author: "honest_dev", State: "spam", SpamFlag: true,
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#88", data)
	pr.State = "open"
	pr.SpamFlag = false
	updated, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#88", updated)
	found, err := commonutil.GetPRByID("test-group", "88")
	if err != nil {
		t.Fatalf("GetPRByID after reopen failed: %v", err)
	}
	if found.State != "open" {
		t.Errorf("expected state=open after reopen, got %q", found.State)
	}
	if found.SpamFlag {
		t.Error("expected SpamFlag=false after reopen")
	}
}
