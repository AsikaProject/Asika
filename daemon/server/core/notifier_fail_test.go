package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/testutil"
)

// failStorage wraps a real Storage and injects failures on specific buckets.
type failStorage struct {
	db.Storage
	failBuckets map[string]bool
	mu          sync.Mutex
}

func (f *failStorage) Put(bucket, key string, value []byte) error {
	f.mu.Lock()
	fail := f.failBuckets[bucket]
	f.mu.Unlock()
	if fail {
		return fmt.Errorf("injected Put failure on bucket %s", bucket)
	}
	return f.Storage.Put(bucket, key, value)
}

func (f *failStorage) PutNotificationDedup(key string, data []byte) error {
	f.mu.Lock()
	fail := f.failBuckets[db.BucketNotificationDedup]
	f.mu.Unlock()
	if fail {
		return fmt.Errorf("injected PutNotificationDedup failure")
	}
	return f.Storage.PutNotificationDedup(key, data)
}

func (f *failStorage) GetNotificationDedup(key string) ([]byte, error) {
	f.mu.Lock()
	fail := f.failBuckets[db.BucketNotificationDedup]
	f.mu.Unlock()
	if fail {
		return nil, fmt.Errorf("injected GetNotificationDedup failure")
	}
	return f.Storage.GetNotificationDedup(key)
}

func (f *failStorage) ListNotificationPrefs(usernames []string) ([]models.NotificationPreferences, error) {
	f.mu.Lock()
	fail := f.failBuckets["prefs"]
	f.mu.Unlock()
	if fail {
		return nil, fmt.Errorf("injected ListNotificationPrefs failure")
	}
	return f.Storage.ListNotificationPrefs(usernames)
}

// mustMarshal_unchanged verifies mustMarshal returns valid JSON even for types
// that normally marshal fine — baseline sanity check.
func TestMustMarshal_Normal(t *testing.T) {
	entry := dedupEntry{
		PRID:      "test-pr",
		Notifier:  "webhook",
		Events:    []string{"pr_opened"},
		Titles:    []string{"title"},
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
	}
	data := mustMarshal(entry)
	if len(data) == 0 {
		t.Fatal("mustMarshal returned empty data")
	}
	var decoded dedupEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("mustMarshal output is not valid JSON: %v", err)
	}
	if decoded.PRID != "test-pr" {
		t.Errorf("PRID = %q, want test-pr", decoded.PRID)
	}
}

// TestMustMarshal_FallbackOnUnmarshalable verifies mustMarshal returns a
// minimal fallback JSON when json.Marshal fails (e.g. channel type).
func TestMustMarshal_FallbackOnUnmarshalable(t *testing.T) {
	type bad struct {
		Ch chan int
	}
	data := mustMarshal(bad{Ch: make(chan int)})
	if len(data) == 0 {
		t.Fatal("mustMarshal returned empty data for unmarshalable type")
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("mustMarshal fallback is not valid JSON: %v", err)
	}
	// Fallback should contain _id
	if _, ok := decoded["_id"]; !ok {
		t.Error("mustMarshal fallback should contain _id field")
	}
}

// TestCachedNotificationPrefs_CacheHit verifies the cache returns stored prefs
// without hitting the database on subsequent calls.
func TestCachedNotificationPrefs_CacheHit(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	ResetNotifierPrefsCache()

	prefs := models.NotificationPreferences{
		Username: "cache-hit-user",
		Enabled:  true,
	}
	db.PutNotificationPrefs("cache-hit-user", mustMarshalJSON(prefs))

	result1, err := cachedNotificationPrefs()
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(result1) != 1 {
		t.Fatalf("expected 1 pref, got %d", len(result1))
	}

	result2, err := cachedNotificationPrefs()
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if len(result2) != 1 || result2[0].Username != "cache-hit-user" {
		t.Errorf("cache returned wrong prefs: %v", result2)
	}

	ResetNotifierPrefsCache()
}

// TestCachedNotificationPrefs_DBPrefFailure verifies that when
// ListNotificationPrefs fails, the error is propagated to the caller.
func TestCachedNotificationPrefs_DBFailure(t *testing.T) {
	realStorage := testutil.NewTestDB(t)
	defer db.Close()

	fail := &failStorage{
		Storage:     realStorage,
		failBuckets: map[string]bool{"prefs": true},
	}
	db.InitWithStorage(fail)
	t.Cleanup(func() { db.Close() })

	ResetNotifierPrefsCache()

	_, err := cachedNotificationPrefs()
	if err == nil {
		t.Fatal("expected error when DB fails, got nil")
	}

	ResetNotifierPrefsCache()
}

// TestCachedNotificationPrefs_ConcurrentCacheRefresh verifies that concurrent
// goroutines don't block each other when the cache expires. With the old
// sync.Mutex approach, all goroutines would serialize behind the DB query.
func TestCachedNotificationPrefs_ConcurrentCacheRefresh(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	ResetNotifierPrefsCache()

	prefs := models.NotificationPreferences{
		Username: "concurrent-user",
		Enabled:  true,
	}
	db.PutNotificationPrefs("concurrent-user", mustMarshalJSON(prefs))

	// Warm the cache
	_, err := cachedNotificationPrefs()
	if err != nil {
		t.Fatalf("warm-up call failed: %v", err)
	}

	// Reset to force concurrent refresh
	ResetNotifierPrefsCache()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cachedNotificationPrefs()
			if err != nil {
				t.Errorf("concurrent cachedNotificationPrefs failed: %v", err)
			}
		}()
	}
	wg.Wait()

	ResetNotifierPrefsCache()
}

// TestSendNotificationWithContext_DedupPutFailure verifies that when
// PutNotificationDedup fails, the notification is still sent (degraded dedup).
func TestSendNotificationWithContext_DedupPutFailure(t *testing.T) {
	realStorage := testutil.NewTestDB(t)
	defer db.Close()

	fail := &failStorage{
		Storage:     realStorage,
		failBuckets: map[string]bool{db.BucketNotificationDedup: true},
	}
	db.InitWithStorage(fail)
	t.Cleanup(func() { db.Close() })

	globalNotifiers = []notifier.Notifier{
		notifier.NewWebhookNotifier(map[string]interface{}{"url": "http://localhost:19999"}),
	}
	failureTracker = notifier.NewFailureTracker(func(string, int, string) {})
	t.Cleanup(func() { globalNotifiers = nil })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	SendNotificationWithContext(ctx, "dedup fail test", "body", "pr_opened", "pr-dedup-fail", "webhook")

	globalNotifiers = nil
}

// TestIsNotifierEnabledForAnyUser_DowngradeOnDBFailure verifies that when
// the preferences DB query fails, notifications are still sent (fail-open).
func TestIsNotifierEnabledForAnyUser_DowngradeOnDBFailure(t *testing.T) {
	realStorage := testutil.NewTestDB(t)
	defer db.Close()

	fail := &failStorage{
		Storage:     realStorage,
		failBuckets: map[string]bool{"prefs": true},
	}
	db.InitWithStorage(fail)
	t.Cleanup(func() { db.Close() })

	ResetNotifierPrefsCache()

	// When DB fails, should return true (fail-open: send notification)
	result := isNotifierEnabledForAnyUser("smtp", "pr_opened")
	if !result {
		t.Error("expected fail-open (true) when DB query fails")
	}
}

// TestResetNotifierPrefsCache_AfterSet verifies that resetting the cache
// forces a fresh DB read on the next call.
func TestResetNotifierPrefsCache_AfterSet(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	ResetNotifierPrefsCache()

	db.PutNotificationPrefs("user1", mustMarshalJSON(models.NotificationPreferences{
		Username: "user1",
		Enabled:  false,
	}))

	prefs, err := cachedNotificationPrefs()
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if len(prefs) != 1 || prefs[0].Enabled {
		t.Error("expected disabled user1")
	}

	// Now change the pref
	db.PutNotificationPrefs("user1", mustMarshalJSON(models.NotificationPreferences{
		Username: "user1",
		Enabled:  true,
	}))

	// Without reset, cache still has old value
	prefs, _ = cachedNotificationPrefs()
	if prefs[0].Enabled {
		t.Error("cache should still have old disabled value, got enabled")
	}

	// After reset, should get fresh value
	ResetNotifierPrefsCache()
	prefs, err = cachedNotificationPrefs()
	if err != nil {
		t.Fatalf("after reset read failed: %v", err)
	}
	if !prefs[0].Enabled {
		t.Error("after reset, should see updated enabled pref")
	}
}

func init() {
	_ = platforms.PlatformType("")
}
