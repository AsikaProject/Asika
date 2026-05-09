package discord

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

func setupDiscordTest(t *testing.T) (*Bot, func()) {
	t.Helper()
	testutil.NewTestDB(t)
	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", Mode: "multi", GitHub: "owner/repo"},
		},
		Discord: models.DiscordConfig{
			Enabled: true, Token: "test-bot-token", AdminIDs: []string{"admin1", "admin2"}, ChannelID: "channel-123",
		},
	}
	config.Store(cfg)
	qm := queue.NewManager(cfg, clients)
	s := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)
	bot := NewBot(cfg, clients, qm, s, sd, nil, []string{"admin1", "admin2"}, nil, nil)
	return bot, func() { db.Close() }
}

func TestDiscordBotCreation(t *testing.T) {
	bot, cleanup := setupDiscordTest(t)
	defer cleanup()
	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if !bot.isAdmin("admin1") {
		t.Error("admin1 should be admin")
	}
	if bot.isAdmin("random") {
		t.Error("random should not be admin")
	}
}

func TestDiscordGetPRByID(t *testing.T) {
	_, cleanup := setupDiscordTest(t)
	defer cleanup()
	pr := models.PRRecord{
		ID: "dc-pr-1", RepoGroup: "test-group", Platform: "github",
		PRNumber: 55, Title: "Discord PR", Author: "dev", State: "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#55", data)

	found, err := commonutil.GetPRByID("test-group", "55")
	if err != nil {
		t.Fatalf("GetPRByID failed: %v", err)
	}
	if found.Title != "Discord PR" {
		t.Errorf("Title = %q, want %q", found.Title, "Discord PR")
	}
}

func TestDiscordMarkSpamReopen(t *testing.T) {
	_, cleanup := setupDiscordTest(t)
	defer cleanup()
	pr := models.PRRecord{
		ID: "dc-spam-pr", RepoGroup: "test-group", Platform: "github",
		PRNumber: 66, Title: "Spam PR", Author: "spammer", State: "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#66", data)

	found, err := commonutil.GetPRByID("test-group", "dc-spam-pr")
	if err != nil {
		t.Fatalf("GetPRByID failed: %v", err)
	}
	found.SpamFlag = true
	found.State = "spam"
	updated, _ := json.Marshal(found)
	db.Put(db.BucketPRs, "test-group#github#66", updated)

	found2, err := commonutil.GetPRByID("test-group", "66")
	if err != nil {
		t.Fatalf("GetPRByID after spam failed: %v", err)
	}
	if !found2.SpamFlag {
		t.Error("expected SpamFlag=true")
	}

	found2.State = "open"
	found2.SpamFlag = false
	reopened, _ := json.Marshal(found2)
	db.Put(db.BucketPRs, "test-group#github#66", reopened)

	found3, err := commonutil.GetPRByID("test-group", "66")
	if err != nil {
		t.Fatalf("GetPRByID after reopen failed: %v", err)
	}
	if found3.State != "open" {
		t.Errorf("state = %q, want open", found3.State)
	}
	if found3.SpamFlag {
		t.Error("SpamFlag should be false after reopen")
	}
}
