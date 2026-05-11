package feishu

import (
	"encoding/json"
	"testing"

	"asika/common/db"
	"asika/common/models"
	commonutil "asika/common/platformutil"
)

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
