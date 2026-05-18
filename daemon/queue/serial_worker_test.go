package queue

import (
	"encoding/json"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/testutil"
)

func setupSerialTest(t *testing.T) (*SerialWorker, *testutil.MockPlatformClient, func()) {
	testDB := testutil.NewTestDB(t)
	_ = testDB

	mock := testutil.NewMockPlatformClient()
	clients := map[platforms.PlatformType]platforms.PlatformClient{
		platforms.PlatformGitHub: mock,
	}

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "test-group", GitHub: "org/repo", DefaultBranch: "main"},
		},
		Git: models.GitConfig{
			WorkDir: t.TempDir(),
		},
	}

	w := NewSerialWorker(cfg, clients)
	return w, mock, func() {}
}

func TestSerialWorker_Enqueue(t *testing.T) {
	w, _, cleanup := setupSerialTest(t)
	defer cleanup()

	item := &models.QueueItem{
		PRID:      "pr-1",
		RepoGroup: "test-group",
		Status:    "waiting",
	}

	err := w.Enqueue(item)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	data, err := db.Get(db.BucketSerialQueue, "test-group#pr-1")
	if err != nil {
		t.Fatalf("Failed to read from bucket: %v", err)
	}

	var stored models.QueueItem
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if stored.ValidationStatus != "validating" {
		t.Errorf("ValidationStatus = %q, want %q", stored.ValidationStatus, "validating")
	}
	if stored.PRID != "pr-1" {
		t.Errorf("PRID = %q, want %q", stored.PRID, "pr-1")
	}
}

func TestSerialWorker_FindPRByID(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	_ = testDB

	pr := &models.PRRecord{
		ID:        "test-pr-1",
		RepoGroup: "default",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex("default#github#42", data, "test-pr-1", "default", 42)

	found, err := FindPRByID("test-pr-1")
	if err != nil {
		t.Fatalf("FindPRByID failed: %v", err)
	}
	if found.ID != "test-pr-1" {
		t.Errorf("ID = %q, want %q", found.ID, "test-pr-1")
	}

	_, err = FindPRByID("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent PR")
	}
}

func TestSerialWorker_Fail(t *testing.T) {
	w, _, cleanup := setupSerialTest(t)
	defer cleanup()

	item := &models.QueueItem{
		PRID:      "pr-fail",
		RepoGroup: "test-group",
		Status:    "waiting",
	}
	w.Enqueue(item)

	key := "test-group#pr-fail"
	w.fail(item, key, "test failure reason")

	data, _ := db.Get(db.BucketSerialQueue, key)
	var stored models.QueueItem
	json.Unmarshal(data, &stored)

	if stored.ValidationStatus != "validation_failed" {
		t.Errorf("ValidationStatus = %q, want %q", stored.ValidationStatus, "validation_failed")
	}
	if stored.FailureReason != "test failure reason" {
		t.Errorf("FailureReason = %q, want %q", stored.FailureReason, "test failure reason")
	}
}

func TestSerialWorker_CheckRebaseTimeout(t *testing.T) {
	w, _, cleanup := setupSerialTest(t)
	defer cleanup()

	item := &models.QueueItem{
		PRID:              "pr-timeout",
		RepoGroup:         "test-group",
		ValidationStatus:  "rebasing",
		ValidationStarted: time.Now().Add(-11 * time.Minute),
	}
	key := "test-group#pr-timeout"
	data, _ := json.Marshal(item)
	db.Put(db.BucketSerialQueue, key, data)

	w.checkRebaseStatus(item, key)

	if item.ValidationStatus != "validation_failed" {
		t.Errorf("ValidationStatus = %q, want %q", item.ValidationStatus, "validation_failed")
	}
}

func TestSerialWorker_StopIdempotent(t *testing.T) {
	w, _, cleanup := setupSerialTest(t)
	defer cleanup()

	w.Stop()
	w.Stop()
	w.Stop()
}

func TestSerialWorker_StopConcurrent(t *testing.T) {
	w, _, cleanup := setupSerialTest(t)
	defer cleanup()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			w.Stop()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent Stop deadlocked")
		}
	}
}
