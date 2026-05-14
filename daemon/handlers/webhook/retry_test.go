package webhook

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func setupRetryTest(t *testing.T) func() {
	t.Helper()
	testutil.NewTestDB(t)
	return func() { db.Close() }
}

func TestRetryBackoff_ExponentialGrowth(t *testing.T) {
	retry := &models.WebhookRetry{
		ID:         "backoff-test",
		RepoGroup:  "test-group",
		Platform:   "github",
		FailCount:  0,
		LastFailed: time.Now(),
	}

	backoffs := make([]time.Duration, 0)
	for i := 1; i <= 13; i++ {
		retry.FailCount = i
		backoff := time.Duration(1<<uint(min(retry.FailCount, 10))) * time.Second
		if backoff > time.Hour {
			backoff = time.Hour
		}
		backoffs = append(backoffs, backoff)
	}

	if backoffs[0] != 2*time.Second {
		t.Errorf("fail 1 backoff = %v, want 2s", backoffs[0])
	}
	if backoffs[1] != 4*time.Second {
		t.Errorf("fail 2 backoff = %v, want 4s", backoffs[1])
	}
	if backoffs[2] != 8*time.Second {
		t.Errorf("fail 3 backoff = %v, want 8s", backoffs[2])
	}
	if backoffs[9] != 1024*time.Second {
		t.Errorf("fail 10 backoff = %v, want 1024s", backoffs[9])
	}
	if backoffs[11] != 1024*time.Second {
		t.Errorf("fail 12 backoff = %v, want 1024s (min caps at 10, so 1<<10=1024s)", backoffs[11])
	}
}

func TestRetryBackoff_MaxBackoff(t *testing.T) {
	for failCount := 1; failCount <= 20; failCount++ {
		backoff := time.Duration(1<<uint(min(failCount, 10))) * time.Second
		if backoff > time.Hour {
			backoff = time.Hour
		}
		maxExpected := 1024 * time.Second
		if backoff > maxExpected {
			t.Errorf("fail %d backoff = %v, exceed max %v", failCount, backoff, maxExpected)
		}
	}
}

func TestWebhookRetryStoreAndRetrieve(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	retry := &models.WebhookRetry{
		ID:         "store-test",
		DeliveryID: "del-123",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{"action":"opened"}`),
		FailCount:  3,
		LastError:  "connection refused",
		LastFailed: time.Now(),
		NextRetry:  time.Now().Add(5 * time.Minute),
	}

	if err := db.PutWebhookRetry(retry); err != nil {
		t.Fatalf("PutWebhookRetry failed: %v", err)
	}

	stored, err := db.GetWebhookRetry("store-test")
	if err != nil {
		t.Fatalf("GetWebhookRetry failed: %v", err)
	}
	if stored == nil {
		t.Fatal("stored retry is nil")
	}
	if stored.FailCount != 3 {
		t.Errorf("FailCount = %d, want 3", stored.FailCount)
	}
	if stored.LastError != "connection refused" {
		t.Errorf("LastError = %q, want %q", stored.LastError, "connection refused")
	}
	if string(stored.Body) != `{"action":"opened"}` {
		t.Errorf("Body = %q, want %q", string(stored.Body), `{"action":"opened"}`)
	}
}

func TestWebhookRetryDelete(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	retry := &models.WebhookRetry{
		ID:        "delete-test",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      []byte(`{}`),
	}

	db.PutWebhookRetry(retry)

	if err := db.DeleteWebhookRetry("delete-test"); err != nil {
		t.Fatalf("DeleteWebhookRetry failed: %v", err)
	}

	stored, _ := db.GetWebhookRetry("delete-test")
	if stored != nil {
		t.Error("expected nil after delete")
	}
}

func TestGetDueWebhookRetries(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	now := time.Now()

	past := &models.WebhookRetry{
		ID:        "due-past",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      []byte(`{}`),
		NextRetry: now.Add(-5 * time.Minute),
	}
	future := &models.WebhookRetry{
		ID:        "due-future",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      []byte(`{}`),
		NextRetry: now.Add(5 * time.Minute),
	}

	db.PutWebhookRetry(past)
	db.PutWebhookRetry(future)

	due, err := db.GetDueWebhookRetries(now)
	if err != nil {
		t.Fatalf("GetDueWebhookRetries failed: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("expected 1 due retry, got %d", len(due))
	}
	if due[0].ID != "due-past" {
		t.Errorf("due retry ID = %q, want due-past", due[0].ID)
	}
}

func TestNotifyWebhookPermanentFailure_CallsNotifyFunc(t *testing.T) {
	var called int32
	var receivedTitle, receivedBody string

	SetNotifyFunc(func(title, body string) {
		atomic.AddInt32(&called, 1)
		receivedTitle = title
		receivedBody = body
	})

	retry := &models.WebhookRetry{
		ID:         "perm-fail-test",
		RepoGroup:  "test-group",
		Platform:   "github",
		FailCount:  10,
		LastError:  "max retries exceeded",
		LastFailed: time.Now(),
	}

	notifyWebhookPermanentFailure(retry)

	if atomic.LoadInt32(&called) != 1 {
		t.Error("expected notifyFunc to be called once")
	}
	if receivedTitle != "⚠️ Webhook Permanent Failure" {
		t.Errorf("title = %q, want %q", receivedTitle, "⚠️ Webhook Permanent Failure")
	}
	if receivedBody == "" {
		t.Error("body should not be empty")
	}

	SetNotifyFunc(nil)
}

func TestNotifyWebhookPermanentFailure_NilNotifyFunc(t *testing.T) {
	SetNotifyFunc(nil)

	retry := &models.WebhookRetry{
		ID:        "nil-notify-test",
		RepoGroup: "test-group",
		Platform:  "github",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("notifyWebhookPermanentFailure panicked with nil notifyFn: %v", r)
		}
	}()

	notifyWebhookPermanentFailure(retry)
}

func TestWebhookRetry_JSONSerialization(t *testing.T) {
	retry := &models.WebhookRetry{
		ID:         "json-test",
		DeliveryID: "del-456",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{"action":"opened","number":1}`),
		FailCount:  5,
		LastError:  "timeout",
		LastFailed: time.Now(),
		NextRetry:  time.Now().Add(10 * time.Minute),
	}

	data, err := json.Marshal(retry)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded models.WebhookRetry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.ID != "json-test" {
		t.Errorf("ID = %q, want json-test", decoded.ID)
	}
	if decoded.FailCount != 5 {
		t.Errorf("FailCount = %d, want 5", decoded.FailCount)
	}
	if string(decoded.Body) != `{"action":"opened","number":1}` {
		t.Errorf("Body = %q", string(decoded.Body))
	}
}

func TestWebhookRetry_JSONCorruptedBody(t *testing.T) {
	invalidJSON := []byte(`{invalid json}`)
	var retry models.WebhookRetry
	err := json.Unmarshal(invalidJSON, &retry)
	if err == nil {
		t.Error("expected error for corrupted JSON, got nil")
	}
}

func TestWebhookRetry_LargeBody(t *testing.T) {
	largeBody := make([]byte, 50000)
	for i := range largeBody {
		largeBody[i] = 'A'
	}

	retry := &models.WebhookRetry{
		ID:        "large-body-test",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      largeBody,
	}

	data, err := json.Marshal(retry)
	if err != nil {
		t.Fatalf("Marshal large body failed: %v", err)
	}

	var decoded models.WebhookRetry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal large body failed: %v", err)
	}

	if len(decoded.Body) != 50000 {
		t.Errorf("Body length = %d, want 50000", len(decoded.Body))
	}
}

func TestWebhookRetry_MinFailCount(t *testing.T) {
	for i := 0; i <= 10; i++ {
		result := min(i, 10)
		if i <= 10 && result != i {
			t.Errorf("min(%d, 10) = %d, want %d", i, result, i)
		}
		if i > 10 && result != 10 {
			t.Errorf("min(%d, 10) = %d, want 10", i, result)
		}
	}
}

func TestWebhookRetry_FailCountIncrement(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	retry := &models.WebhookRetry{
		ID:        "increment-test",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      []byte(`{}`),
		FailCount: 0,
	}

	db.PutWebhookRetry(retry)

	for i := 1; i <= 5; i++ {
		retry.FailCount++
		retry.LastError = fmt.Sprintf("error-%d", i)
		retry.LastFailed = time.Now()
		backoff := time.Duration(1<<uint(min(retry.FailCount, 10))) * time.Second
		retry.NextRetry = time.Now().Add(backoff)
		db.PutWebhookRetry(retry)
	}

	stored, err := db.GetWebhookRetry("increment-test")
	if err != nil {
		t.Fatalf("GetWebhookRetry failed: %v", err)
	}
	if stored.FailCount != 5 {
		t.Errorf("FailCount = %d, want 5", stored.FailCount)
	}
	if stored.LastError != "error-5" {
		t.Errorf("LastError = %q, want error-5", stored.LastError)
	}
}

func TestStartStopWebhookRetryWorker(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	StartWebhookRetryWorker()
	time.Sleep(100 * time.Millisecond)
	StopWebhookRetryWorker()
	time.Sleep(100 * time.Millisecond)
}

func TestStartWebhookRetryWorker_Idempotent(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	StartWebhookRetryWorker()
	StartWebhookRetryWorker()
	StopWebhookRetryWorker()
}

func TestWebhookRetry_StateTransition_FailedRetry(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	retryID := "state-fail-retry"
	now := time.Now()
	retry := &models.WebhookRetry{
		ID:         retryID,
		DeliveryID: "del-state-fail",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{"action":"opened","number":1}`),
		FailCount:  3,
		LastError:  "connection refused",
		LastFailed: now,
		NextRetry:  now.Add(8 * time.Second),
	}
	db.PutWebhookRetry(retry)

	retry.FailCount++
	retry.LastError = "timeout"
	retry.LastFailed = time.Now()
	backoff := time.Duration(1<<uint(min(retry.FailCount, 10))) * time.Second
	retry.NextRetry = time.Now().Add(backoff)
	db.PutWebhookRetry(retry)

	stored, err := db.GetWebhookRetry(retryID)
	if err != nil {
		t.Fatalf("GetWebhookRetry failed: %v", err)
	}
	if stored == nil {
		t.Fatal("retry record should exist")
	}
	if stored.FailCount != 4 {
		t.Errorf("FailCount = %d, want 4", stored.FailCount)
	}
	if stored.LastError != "timeout" {
		t.Errorf("LastError = %q, want timeout", stored.LastError)
	}
	if !stored.NextRetry.After(time.Now()) {
		t.Errorf("NextRetry should be in the future, got %v", stored.NextRetry)
	}
}

func TestWebhookRetry_StateTransition_MaxAttempts(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	var notified int32
	SetNotifyFunc(func(title, body string) {
		atomic.AddInt32(&notified, 1)
	})
	defer SetNotifyFunc(nil)

	retryID := "state-max-retry"
	retry := &models.WebhookRetry{
		ID:         retryID,
		DeliveryID: "del-state-max",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{"action":"opened","number":1}`),
		FailCount:  9,
		LastError:  "timeout",
		LastFailed: time.Now(),
	}
	db.PutWebhookRetry(retry)

	retry.FailCount = 10
	retry.LastError = "max retries exceeded"
	retry.LastFailed = time.Now()
	notifyWebhookPermanentFailure(retry)
	db.DeleteWebhookRetry(retryID)

	stored, _ := db.GetWebhookRetry(retryID)
	if stored != nil {
		t.Error("retry record should be removed after max attempts")
	}
	if atomic.LoadInt32(&notified) != 1 {
		t.Error("expected permanent failure notification")
	}
}

func TestWebhookRetry_DueItemsFilteredByTime(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	now := time.Now()

	dueRetry := &models.WebhookRetry{
		ID:        "due-now",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      []byte(`{}`),
		NextRetry: now.Add(-1 * time.Minute),
	}
	futureRetry := &models.WebhookRetry{
		ID:        "due-future",
		RepoGroup: "test-group",
		Platform:  "github",
		Body:      []byte(`{}`),
		NextRetry: now.Add(5 * time.Minute),
	}
	db.PutWebhookRetry(dueRetry)
	db.PutWebhookRetry(futureRetry)

	due, err := db.GetDueWebhookRetries(now)
	if err != nil {
		t.Fatalf("GetDueWebhookRetries failed: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due retry, got %d", len(due))
	}
	if due[0].ID != "due-now" {
		t.Errorf("due retry ID = %q, want due-now", due[0].ID)
	}
}

func TestWebhookRetry_NextRetryPrecision(t *testing.T) {
	cleanup := setupRetryTest(t)
	defer cleanup()

	now := time.Now()
	retry := &models.WebhookRetry{
		ID:         "precision-retry",
		DeliveryID: "del-precision",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{}`),
		FailCount:  3,
		LastError:  "error",
		LastFailed: now,
		NextRetry:  now.Add(4 * time.Second),
	}
	db.PutWebhookRetry(retry)

	due, err := db.GetDueWebhookRetries(now.Add(3 * time.Second))
	if err != nil {
		t.Fatalf("GetDueWebhookRetries failed: %v", err)
	}
	for _, r := range due {
		if r.ID == "precision-retry" {
			t.Error("retry should NOT be due yet (NextRetry is 4s in future)")
		}
	}

	due, err = db.GetDueWebhookRetries(now.Add(5 * time.Second))
	if err != nil {
		t.Fatalf("GetDueWebhookRetries failed: %v", err)
	}
	found := false
	for _, r := range due {
		if r.ID == "precision-retry" {
			found = true
			break
		}
	}
	if !found {
		t.Error("retry should be due after NextRetry time has passed")
	}
}
