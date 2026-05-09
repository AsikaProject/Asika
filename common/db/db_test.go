package db

import (
	"testing"

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
