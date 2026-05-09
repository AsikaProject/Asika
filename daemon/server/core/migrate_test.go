package core

import (
	"encoding/json"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
)

func TestMigrateRepoGroupNames_NoGroups(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{RepoGroups: []models.RepoGroupConfig{}}
	MigrateRepoGroupNames(cfg)
}

func TestMigrateRepoGroupNames_NoChanges(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default"},
		},
	}

	pr := models.PRRecord{
		ID:        "pr-1",
		RepoGroup: "default",
		Platform:  "github",
		PRNumber:  1,
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	if err := db.Put(db.BucketPRs, "default#github#1", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	MigrateRepoGroupNames(cfg)

	stored, err := db.Get(db.BucketPRs, "default#github#1")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.RepoGroup != "default" {
		t.Errorf("repo group = %q, want 'default'", result.RepoGroup)
	}
}

func TestMigrateRepoGroupNames_RenamesOldGroup(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default"},
			{Name: "backend"},
		},
	}

	pr := models.PRRecord{
		ID:        "pr-old",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  42,
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	if err := db.Put(db.BucketPRs, "main#github#42", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	MigrateRepoGroupNames(cfg)

	stored, err := db.Get(db.BucketPRs, "default#pr-old")
	if err != nil {
		t.Fatalf("PR not found at new key: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.RepoGroup != "default" {
		t.Errorf("repo group = %q, want 'default'", result.RepoGroup)
	}
	if result.ID != "pr-old" {
		t.Errorf("ID = %q, want 'pr-old'", result.ID)
	}
}

func TestMigrateRepoGroupNames_QueueItems(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default"},
		},
	}

	qi := models.QueueItem{
		PRID:      "qi-1",
		RepoGroup: "old-group",
		Status:    "waiting",
	}
	data, _ := json.Marshal(qi)
	if err := db.Put(db.BucketQueueItems, "old-group#qi-1", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	MigrateRepoGroupNames(cfg)

	stored, err := db.Get(db.BucketQueueItems, "default#qi-1")
	if err != nil {
		t.Fatalf("queue item not found at new key: %v", err)
	}
	var result models.QueueItem
	json.Unmarshal(stored, &result)
	if result.RepoGroup != "default" {
		t.Errorf("queue repo group = %q, want 'default'", result.RepoGroup)
	}
	if result.PRID != "qi-1" {
		t.Errorf("PRID = %q, want 'qi-1'", result.PRID)
	}
}

func TestMigratePRStates_NoChanges(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}

	pr := models.PRRecord{
		ID:        "pr-open",
		RepoGroup: "rg",
		Platform:  "github",
		PRNumber:  1,
		State:     "open",
	}
	data, _ := json.Marshal(pr)
	if err := db.Put(db.BucketPRs, "rg#github#1", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	MigratePRStates(cfg)

	stored, err := db.Get(db.BucketPRs, "rg#github#1")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.State != "open" {
		t.Errorf("state = %q, want 'open'", result.State)
	}
}

func TestMigratePRStates_FixesClosedWithMergedAt(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}

	pr := models.PRRecord{
		ID:        "pr-merged",
		RepoGroup: "rg",
		Platform:  "github",
		PRNumber:  10,
		State:     "closed",
		MergedAt:  time.Now(),
	}
	data, _ := json.Marshal(pr)
	if err := db.Put(db.BucketPRs, "rg#github#10", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	MigratePRStates(cfg)

	stored, err := db.Get(db.BucketPRs, "rg#github#10")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.State != "merged" {
		t.Errorf("state = %q, want 'merged'", result.State)
	}
}

func TestMigratePRStates_SkipsAlreadyMerged(t *testing.T) {
	dir := t.TempDir()
	if err := db.Init(dir + "/test.db"); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &models.Config{}

	pr := models.PRRecord{
		ID:        "pr-already",
		RepoGroup: "rg",
		Platform:  "github",
		PRNumber:  20,
		State:     "merged",
		MergedAt:  time.Now(),
	}
	data, _ := json.Marshal(pr)
	if err := db.Put(db.BucketPRs, "rg#github#20", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	MigratePRStates(cfg)

	stored, err := db.Get(db.BucketPRs, "rg#github#20")
	if err != nil {
		t.Fatalf("PR not found: %v", err)
	}
	var result models.PRRecord
	json.Unmarshal(stored, &result)
	if result.State != "merged" {
		t.Errorf("state = %q, want 'merged'", result.State)
	}
}
