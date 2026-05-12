package db

import (
	"encoding/json"
	"testing"

	"asika/common/models"
	"go.etcd.io/bbolt"
)

func initTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	err := Init(dir + "/test.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { Close() })
}

func TestInitAndClose(t *testing.T) {
	initTestDB(t)

	if defaultStorage == nil {
		t.Fatal("defaultStorage should be initialized")
	}
}

func TestBucketsCreated(t *testing.T) {
	initTestDB(t)

	s, ok := defaultStorage.(*bboltStorage)
	if !ok {
		t.Skip("not bbolt storage")
	}
	err := s.db.View(func(tx *bbolt.Tx) error {
		buckets := []string{
			BucketConfig, BucketRepos, BucketPRs, BucketLogs,
			BucketQueueItems, BucketUsers, BucketSyncHistory,
		}
		for _, bucketName := range buckets {
			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				t.Errorf("bucket %q not found", bucketName)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
}

func TestPutAndGet(t *testing.T) {
	initTestDB(t)

	err := Put("prs", "pr-123", []byte(`{"id":"pr-123","state":"open"}`))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	val, err := Get("prs", "pr-123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(val) != `{"id":"pr-123","state":"open"}` {
		t.Errorf("Get() = %q, want %q", string(val), `{"id":"pr-123","state":"open"}`)
	}
}

func TestGet_NonExistent(t *testing.T) {
	initTestDB(t)

	val, err := Get("prs", "non-existent")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != nil {
		t.Errorf("Get() for non-existent key should return nil, got %q", string(val))
	}
}

func TestDelete(t *testing.T) {
	initTestDB(t)

	err := Put("prs", "pr-123", []byte("test-data"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	val, _ := Get("prs", "pr-123")
	if val == nil {
		t.Fatal("key should exist before delete")
	}

	err = Delete("prs", "pr-123")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	val, _ = Get("prs", "pr-123")
	if val != nil {
		t.Errorf("key should not exist after delete, got %q", string(val))
	}
}

func TestDelete_NonExistent(t *testing.T) {
	initTestDB(t)

	err := Delete("prs", "non-existent")
	if err != nil {
		t.Fatalf("Delete non-existent key should not error: %v", err)
	}
}

func TestForEach(t *testing.T) {
	initTestDB(t)

	testData := map[string]string{
		"pr-1": "data-1",
		"pr-2": "data-2",
		"pr-3": "data-3",
	}

	for k, v := range testData {
		err := Put("prs", k, []byte(v))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	count := 0
	found := make(map[string]bool)
	err := ForEach("prs", func(key, value []byte) error {
		count++
		found[string(key)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}

	if count != len(testData) {
		t.Errorf("ForEach visited %d items, want %d", count, len(testData))
	}

	for k := range testData {
		if !found[k] {
			t.Errorf("key %q not visited by ForEach", k)
		}
	}
}

func TestForEach_EmptyBucket(t *testing.T) {
	initTestDB(t)

	count := 0
	err := ForEach("prs", func(key, value []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if count != 0 {
		t.Errorf("ForEach on empty bucket should visit 0 items, got %d", count)
	}
}

func TestPing_Success(t *testing.T) {
	initTestDB(t)

	err := Ping()
	if err != nil {
		t.Errorf("Ping() = %v, want nil", err)
	}
}

func TestPing_NotInitialized(t *testing.T) {
	orig := defaultStorage
	defer func() {
		defaultStorage = orig
		if r := recover(); r == nil {
			t.Error("Ping() should panic when storage is nil")
		}
	}()

	defaultStorage = nil

	Ping()
}

func TestPing_AfterClose(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir + "/ping_test.db")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := Ping(); err != nil {
		t.Errorf("Ping() before close = %v, want nil", err)
	}

	Close()

	err = Ping()
	if err == nil {
		t.Error("Ping() should return error after Close()")
	}
}

func TestPut_InvalidBucket(t *testing.T) {
	initTestDB(t)

	err := Put("invalid-bucket", "key", []byte("value"))
	if err == nil {
		t.Error("Put to invalid bucket should return error")
	}
}

func TestGet_InvalidBucket(t *testing.T) {
	initTestDB(t)

	_, err := Get("invalid-bucket", "key")
	if err == nil {
		t.Error("Get from invalid bucket should return error")
	}
}

func TestDelete_InvalidBucket(t *testing.T) {
	initTestDB(t)

	err := Delete("invalid-bucket", "key")
	if err == nil {
		t.Error("Delete from invalid bucket should return error")
	}
}

func TestForEach_InvalidBucket(t *testing.T) {
	initTestDB(t)

	err := ForEach("invalid-bucket", func(key, value []byte) error {
		return nil
	})
	if err == nil {
		t.Error("ForEach on invalid bucket should return error")
	}
}

func TestListNotificationPrefs_Empty(t *testing.T) {
	initTestDB(t)

	prefs, err := ListNotificationPrefs(nil)
	if err != nil {
		t.Fatalf("ListNotificationPrefs failed: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("expected 0 prefs, got %d", len(prefs))
	}
}

func TestListNotificationPrefs_SingleUser(t *testing.T) {
	initTestDB(t)

	data, _ := json.Marshal(models.NotificationPreferences{
		Username:         "alice",
		Enabled:          true,
		EnabledNotifiers: []string{"smtp", "telegram"},
		EventPrefs:       map[string]bool{"pr_opened": true, "pr_closed": false},
		DigestMode:       "realtime",
	})
	PutNotificationPrefs("alice", data)

	prefs, err := ListNotificationPrefs(nil)
	if err != nil {
		t.Fatalf("ListNotificationPrefs failed: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("expected 1 pref, got %d", len(prefs))
	}
	if prefs[0].Username != "alice" {
		t.Errorf("expected alice, got %s", prefs[0].Username)
	}
	if !prefs[0].Enabled {
		t.Error("expected Enabled=true")
	}
	if len(prefs[0].EnabledNotifiers) != 2 {
		t.Errorf("expected 2 enabled notifiers, got %d", len(prefs[0].EnabledNotifiers))
	}
	if prefs[0].EventPrefs["pr_closed"] {
		t.Error("expected pr_closed=false")
	}
}

func TestListNotificationPrefs_MultipleUsers(t *testing.T) {
	initTestDB(t)

	for _, u := range []string{"alice", "bob", "charlie"} {
		data, _ := json.Marshal(models.NotificationPreferences{
			Username: u,
			Enabled:  true,
		})
		PutNotificationPrefs(u, data)
	}

	prefs, err := ListNotificationPrefs(nil)
	if err != nil {
		t.Fatalf("ListNotificationPrefs failed: %v", err)
	}
	if len(prefs) != 3 {
		t.Errorf("expected 3 prefs, got %d", len(prefs))
	}
}

func TestListNotificationPrefs_FilterByUsernames(t *testing.T) {
	initTestDB(t)

	for _, u := range []string{"alice", "bob", "charlie"} {
		data, _ := json.Marshal(models.NotificationPreferences{
			Username: u,
			Enabled:  true,
		})
		PutNotificationPrefs(u, data)
	}

	prefs, err := ListNotificationPrefs([]string{"alice", "charlie"})
	if err != nil {
		t.Fatalf("ListNotificationPrefs failed: %v", err)
	}
	if len(prefs) != 2 {
		t.Errorf("expected 2 prefs, got %d", len(prefs))
	}

	names := make(map[string]bool)
	for _, p := range prefs {
		names[p.Username] = true
	}
	if !names["alice"] || !names["charlie"] {
		t.Errorf("expected alice and charlie, got %v", names)
	}
	if names["bob"] {
		t.Error("bob should not be in filtered results")
	}
}

func TestListNotificationPrefs_InvalidJSON(t *testing.T) {
	initTestDB(t)

	PutNotificationPrefs("broken", []byte("not valid json"))
	data, _ := json.Marshal(models.NotificationPreferences{
		Username: "alice",
		Enabled:  true,
	})
	PutNotificationPrefs("alice", data)

	prefs, err := ListNotificationPrefs(nil)
	if err != nil {
		t.Fatalf("ListNotificationPrefs failed: %v", err)
	}
	if len(prefs) != 1 {
		t.Errorf("expected 1 valid pref (broken should be skipped), got %d", len(prefs))
	}
	if prefs[0].Username != "alice" {
		t.Errorf("expected alice, got %s", prefs[0].Username)
	}
}

func TestAuditLogIndex_WriteAndQuery(t *testing.T) {
	initTestDB(t)

	logKey := "1700000000000000000_abcdef12"
	entry := models.AuditLog{
		Timestamp: models.ParseTime("2024-01-01T00:00:00Z"),
		Level:     "info",
		Message:   "PR approved",
		Actor:     "alice",
		RepoGroup: "frontend",
		Action:    "approve",
		Category:  "pr",
	}
	data, _ := json.Marshal(entry)
	Put(BucketLogs, logKey, data)

	s := defaultStorage.(*bboltStorage)
	err := s.writeAuditLogIndex(logKey, entry)
	if err != nil {
		t.Fatalf("writeAuditLogIndex failed: %v", err)
	}

	var actorResults []models.AuditLog
	err = ForEachPrefix(BucketAuditLogIndex, BucketLogs, "actor:alice:", func(idxKey, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		actorResults = append(actorResults, log)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix failed: %v", err)
	}
	if len(actorResults) != 1 {
		t.Fatalf("expected 1 result for actor:alice, got %d", len(actorResults))
	}
	if actorResults[0].Message != "PR approved" {
		t.Errorf("expected 'PR approved', got %s", actorResults[0].Message)
	}

	var rgResults []models.AuditLog
	err = ForEachPrefix(BucketAuditLogIndex, BucketLogs, "repo_group:frontend:", func(idxKey, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		rgResults = append(rgResults, log)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix failed: %v", err)
	}
	if len(rgResults) != 1 {
		t.Errorf("expected 1 result for repo_group:frontend, got %d", len(rgResults))
	}

	var actionResults []models.AuditLog
	err = ForEachPrefix(BucketAuditLogIndex, BucketLogs, "action:approve:", func(idxKey, value []byte) error {
		var log models.AuditLog
		if err := json.Unmarshal(value, &log); err != nil {
			return nil
		}
		actionResults = append(actionResults, log)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix failed: %v", err)
	}
	if len(actionResults) != 1 {
		t.Errorf("expected 1 result for action:approve, got %d", len(actionResults))
	}

	var emptyResults []models.AuditLog
	err = ForEachPrefix(BucketAuditLogIndex, BucketLogs, "actor:nonexistent:", func(idxKey, value []byte) error {
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix failed: %v", err)
	}
	if len(emptyResults) != 0 {
		t.Errorf("expected 0 results for nonexistent actor, got %d", len(emptyResults))
	}
}

func TestAuditLogIndex_MinimalEntry(t *testing.T) {
	initTestDB(t)

	logKey := "1700000000000000001_11111111"
	entry := models.AuditLog{
		Timestamp: models.ParseTime("2024-01-01T00:00:00Z"),
		Level:     "info",
		Message:   "simple log",
	}
	data, _ := json.Marshal(entry)
	Put(BucketLogs, logKey, data)

	s := defaultStorage.(*bboltStorage)
	err := s.writeAuditLogIndex(logKey, entry)
	if err != nil {
		t.Fatalf("writeAuditLogIndex failed: %v", err)
	}

	var count int
	err = ForEach(BucketAuditLogIndex, func(key, value []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 index entries for minimal log, got %d", count)
	}
}

func TestAuditLogIndex_AllFields(t *testing.T) {
	initTestDB(t)

	logKey := "1700000000000000002_22222222"
	entry := models.AuditLog{
		Timestamp: models.ParseTime("2024-01-01T00:00:00Z"),
		Level:     "warn",
		Message:   "full log",
		Actor:     "bob",
		RepoGroup: "backend",
		Action:    "close",
		Category:  "pr",
	}
	data, _ := json.Marshal(entry)
	Put(BucketLogs, logKey, data)

	s := defaultStorage.(*bboltStorage)
	err := s.writeAuditLogIndex(logKey, entry)
	if err != nil {
		t.Fatalf("writeAuditLogIndex failed: %v", err)
	}

	var count int
	err = ForEach(BucketAuditLogIndex, func(key, value []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 index entries (actor+repo_group+action+category), got %d", count)
	}
}
