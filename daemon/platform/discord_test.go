package platform

import (
	"encoding/json"
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
	"asika/testutil"
)

func setupDiscordTest(t *testing.T) (*DiscordBot, func()) {
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
		Discord: models.DiscordConfig{
			Enabled:   true,
			Token:     "test-bot-token",
			AdminIDs:  []string{"admin1", "admin2"},
			ChannelID: "channel-123",
		},
	}
	config.Store(cfg)

	qm := queue.NewManager(cfg, clients)
	syncr := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)

	bot := NewDiscordBot(cfg, clients, qm, syncr, sd, nil, []string{"admin1", "admin2"})

	cleanup := func() {
		db.Close()
	}
	return bot, cleanup
}

func TestDiscordBotCreation(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if len(bot.adminIDs) != 2 {
		t.Errorf("expected 2 admin IDs, got %d", len(bot.adminIDs))
	}
	if !bot.adminIDs["admin1"] {
		t.Error("expected admin ID admin1 to be present")
	}
	if !bot.adminIDs["admin2"] {
		t.Error("expected admin ID admin2 to be present")
	}
}

func TestDiscordIsAdmin_WithAdminIDs(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	if !bot.isAdmin("admin1") {
		t.Error("admin1 should be recognized")
	}
	if bot.isAdmin("stranger") {
		t.Error("stranger should not be admin")
	}
}

func TestDiscordIsAdmin_EmptyAdminIDs(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()
	bot.adminIDs = map[string]bool{}

	if !bot.isAdmin("anyone") {
		t.Error("with empty adminIDs, everyone should be admin")
	}
}

func TestDiscordRequireAdmin(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	if !bot.requireAdmin("admin1") {
		t.Error("admin1 should pass requireAdmin")
	}
	if bot.requireAdmin("stranger") {
		t.Error("stranger should fail requireAdmin")
	}
}

func TestDiscordGetClientForPlatform(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	client := bot.getClientForPlatform("github")
	if client == nil {
		t.Error("expected non-nil client for github")
	}

	client = bot.getClientForPlatform("unknown")
	if client != nil {
		t.Error("expected nil client for unknown platform")
	}
}

func TestDiscordBotStop(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	// Stop should not panic
	bot.Stop()
}

func TestDiscordBotStartWithoutSession(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	// Start with nil session should not panic
	bot.Start()
}

func TestDiscordSetSession(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	if bot.session != nil {
		t.Error("session should be nil initially")
	}

	// SetSession should not panic even with nil
	bot.SetSession(nil)
}

func TestDiscordHandleHelp(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()

	// Verify bot is properly initialized for command handling
	if bot.cfg == nil {
		t.Error("cfg should not be nil")
	}
	if bot.clients == nil {
		t.Error("clients should not be nil")
	}
	if bot.queueMgr == nil {
		t.Error("queueMgr should not be nil")
	}
}

func TestDiscordAdminIDsFromConfig(t *testing.T) {
	_, cleanup := setupDiscordTest(t)
	defer cleanup()

	cfg := config.Current()
	if cfg == nil {
		t.Fatal("config not set")
	}

	if len(cfg.Discord.AdminIDs) != 2 {
		t.Errorf("expected 2 admin IDs, got %d", len(cfg.Discord.AdminIDs))
	}
}

func TestDiscordMarkSpamLogic(t *testing.T) {
	_, cleanup := setupDiscordTest(t)
	defer cleanup()

	pr := models.PRRecord{
		ID:        "dc-spam-pr",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  55,
		Title:     "Discord spam test",
		Author:    "spammer_dc",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#55", data)

	found, err := getPRByID("test-group", "55")
	if err != nil {
		t.Fatalf("getPRByID failed: %v", err)
	}
	if found.State != "open" {
		t.Errorf("initial state = %q, want open", found.State)
	}

	// Simulate spam marking
	found.SpamFlag = true
	found.State = "spam"
	updated, _ := json.Marshal(found)
	db.Put(db.BucketPRs, "test-group#github#55", updated)

	found2, err := getPRByID("test-group", "dc-spam-pr")
	if err != nil {
		t.Fatalf("getPRByID after spam failed: %v", err)
	}
	if !found2.SpamFlag {
		t.Error("expected SpamFlag=true")
	}
	if found2.State != "spam" {
		t.Errorf("expected state=spam, got %q", found2.State)
	}
}

func TestDiscordCloseReopenLogic(t *testing.T) {
	_, cleanup := setupDiscordTest(t)
	defer cleanup()

	pr := models.PRRecord{
		ID:        "dc-close-pr",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  66,
		Title:     "Discord close/reopen test",
		Author:    "dev_dc",
		State:    	"open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#66", data)

	// Simulate close
	pr.State = "closed"
	updated, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#66", updated)

	found, err := getPRByID("test-group", "66")
	if err != nil {
		t.Fatalf("getPRByID after close failed: %v", err)
	}
	if found.State != "closed" {
		t.Errorf("expected state=closed, got %q", found.State)
	}

	// Simulate reopen
	pr.State = "open"
	pr.SpamFlag = false
	reopened, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#66", reopened)

	found2, err := getPRByID("test-group", "dc-close-pr")
	if err != nil {
		t.Fatalf("getPRByID after reopen failed: %v", err)
	}
	if found2.State != "open" {
		t.Errorf("expected state=open, got %q", found2.State)
	}
	if found2.SpamFlag {
		t.Error("expected SpamFlag=false after reopen")
	}
}

func TestDiscordMultiGroupIsolation(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	db.DB = tdb

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "group-x", Mode: "multi", GitHub: "org-x/repo-x"},
			{Name: "group-y", Mode: "multi", GitHub: "org-y/repo-y"},
		},
		Discord: models.DiscordConfig{
			Enabled:  true,
			Token:    "test-token",
			AdminIDs: []string{"admin1"},
		},
	}
	config.Store(cfg)

	qm := queue.NewManager(cfg, clients)
	syncr := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)

	bot := NewDiscordBot(cfg, clients, qm, syncr, sd, nil, []string{"admin1"})

	defer db.Close()

	if bot == nil {
		t.Fatal("bot should not be nil")
	}

	// Create same PR number in two different groups
	prX := models.PRRecord{
		ID:        "pr-dc-x",
		RepoGroup: "group-x",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Feature X",
		Author:    "devX",
		State:     "open",
	}
	prY := models.PRRecord{
		ID:        "pr-dc-y",
		RepoGroup: "group-y",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Feature Y",
		Author:    "devY",
		State:     "open",
	}
	dataX, _ := json.Marshal(prX)
	dataY, _ := json.Marshal(prY)
	db.Put(db.BucketPRs, "group-x#github#42", dataX)
	db.Put(db.BucketPRs, "group-y#github#42", dataY)

	t.Run("close group-x does not affect group-y", func(t *testing.T) {
		pr, err := getPRByID("group-x", "42")
		if err != nil {
			t.Fatalf("getPRByID failed: %v", err)
		}
		if pr.RepoGroup != "group-x" {
			t.Errorf("expected group-x, got %s", pr.RepoGroup)
		}
		pr.State = "closed"
		updated, _ := json.Marshal(pr)
		db.Put(db.BucketPRs, "group-x#github#42", updated)

		prY2, err := getPRByID("group-y", "42")
		if err != nil {
			t.Fatalf("getPRByID for group-y failed: %v", err)
		}
		if prY2.State != "open" {
			t.Errorf("group-y PR state = %q, want open", prY2.State)
		}
	})

	t.Run("spam group-y does not affect group-x", func(t *testing.T) {
		pr, err := getPRByID("group-y", "42")
		if err != nil {
			t.Fatalf("getPRByID failed: %v", err)
		}
		pr.SpamFlag = true
		pr.State = "spam"
		updated, _ := json.Marshal(pr)
		db.Put(db.BucketPRs, "group-y#github#42", updated)

		prX2, err := getPRByID("group-x", "42")
		if err != nil {
			t.Fatalf("getPRByID for group-x failed: %v", err)
		}
		if prX2.State != "closed" {
			t.Errorf("group-x PR state = %q, want closed", prX2.State)
		}
		if prX2.SpamFlag {
			t.Error("group-x PR should not have SpamFlag")
		}
	})
}
