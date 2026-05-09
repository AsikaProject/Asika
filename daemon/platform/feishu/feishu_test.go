package feishu

import (
	"context"
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

func setupFeishuTest(t *testing.T) (*Bot, func()) {
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
		Feishu: models.FeishuConfig{
			Enabled: true, AppID: "test-app-id", AppSecret: "test-secret", AdminIDs: []string{"ou_admin1", "ou_admin2"},
		},
	}
	config.Store(cfg)
	qm := queue.NewManager(cfg, clients)
	s := syncer.NewSyncer(cfg, clients)
	sd := syncer.NewSpamDetectorWithClients(cfg, clients)
	b := NewBot(cfg, clients, qm, s, sd, nil)
	return b, func() { db.Close() }
}

func TestFeishuBotCreation(t *testing.T) {
	bot, cleanup := setupFeishuTest(t)
	defer cleanup()
	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if len(bot.adminIDs) != 2 {
		t.Errorf("expected 2 admin IDs, got %d", len(bot.adminIDs))
	}
}

func TestFeishuIsAdmin(t *testing.T) {
	bot, cleanup := setupFeishuTest(t)
	defer cleanup()
	if !bot.isAdmin("ou_admin1") {
		t.Error("ou_admin1 should be admin")
	}
	if bot.isAdmin("random_user") {
		t.Error("random_user should not be admin")
	}
}

func TestFeishuHandleEvent_URLVerification(t *testing.T) {
	bot, cleanup := setupFeishuTest(t)
	defer cleanup()
	body := `{"header":{"event_type":"url_verification","token":"test"},"event":{"challenge":"abc123","token":"test","type":"url_verification"}}`
	resp, err := bot.HandleEvent(context.Background(), []byte(body))
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}
	m, ok := resp.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", resp)
	}
	if m["challenge"] != "abc123" {
		t.Errorf("challenge = %q, want %q", m["challenge"], "abc123")
	}
}

func TestFeishuGetPRRecord(t *testing.T) {
	_, cleanup := setupFeishuTest(t)
	defer cleanup()
	pr := models.PRRecord{
		ID: "feishu-pr-1", RepoGroup: "test-group", Platform: "github",
		PRNumber: 77, Title: "Test PR", Author: "dev", State: "open",
	}
	data, _ := json.Marshal(pr)
	db.Put(db.BucketPRs, "test-group#github#77", data)

	found, _ := commonutil.GetPRByID("test-group", "77")
	if found == nil || found.Title != "Test PR" {
		t.Error("expected to find PR")
	}
	_, err := commonutil.GetPRByID("test-group", "99999")
	if err == nil {
		t.Error("expected error for nonexistent PR")
	}
}

func TestFeishuProcessCommand_Help(t *testing.T) {
	bot, cleanup := setupFeishuTest(t)
	defer cleanup()
	result := bot.processCommand("ou_admin1", "help")
	if result == "" {
		t.Error("expected non-empty help text")
	}
}

func TestFeishuProcessCommand_Unknown(t *testing.T) {
	bot, cleanup := setupFeishuTest(t)
	defer cleanup()
	result := bot.processCommand("ou_admin1", "nonexistentcmd")
	if result == "" {
		t.Error("expected error message for unknown command")
	}
}

func TestFeishuProcessCommand_Unauthorized(t *testing.T) {
	bot, cleanup := setupFeishuTest(t)
	defer cleanup()
	result := bot.processCommand("random_user", "help")
	if result != "Access denied. Operator or Admin only." {
		t.Errorf("expected access denied, got %q", result)
	}
}
