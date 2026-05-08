package slack

import (
	"testing"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
	"asika/testutil"
)

func setupSlackTest(t *testing.T) (*Bot, func()) {
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
		Slack: models.SlackConfig{
			Enabled: true, Token: "test-token", AppToken: "test-app-token", AdminIDs: []string{"U_ADMIN1"},
		},
	}
	config.Store(cfg)
	qm := queue.NewManager(cfg, clients)
	s := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)
	bot := NewBot(cfg, clients, qm, s, sd, nil, []string{"U_ADMIN1"})
	return bot, func() { db.Close() }
}

func TestSlackBotCreation(t *testing.T) {
	bot, cleanup := setupSlackTest(t)
	defer cleanup()
	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if !bot.isAdmin("U_ADMIN1") {
		t.Error("U_ADMIN1 should be admin")
	}
	if bot.isAdmin("U_RANDOM") {
		t.Error("U_RANDOM should not be admin")
	}
}

func TestSlackIsAdmin_EmptyAdminIDs(t *testing.T) {
	bot, cleanup := setupSlackTest(t)
	defer cleanup()
	bot.adminIDs = map[string]bool{}
	if !bot.isAdmin("any_user") {
		t.Error("with empty adminIDs, everyone should be admin")
	}
}

func TestSlackGetClientForPlatform(t *testing.T) {
	bot, cleanup := setupSlackTest(t)
	defer cleanup()
	if bot.getClientForPlatform("github") == nil {
		t.Error("expected non-nil client for github")
	}
	if bot.getClientForPlatform("unknown") != nil {
		t.Error("expected nil client for unknown")
	}
}
