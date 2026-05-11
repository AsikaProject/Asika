package discord

import (
	"encoding/json"
	"testing"

	"asika/common/db"
	"asika/common/models"
	commonutil "asika/common/platformutil"
)

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
