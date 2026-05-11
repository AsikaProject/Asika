package telegram

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

func setupBotTest(t *testing.T) (*Bot, func()) {
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
