package telegram

import (
	"encoding/json"
	"testing"

	"asika/common/db"
	"asika/common/models"
	commonutil "asika/common/platformutil"
)

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
