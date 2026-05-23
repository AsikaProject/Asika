package db_test

import (
	"sync"
	"testing"
	"time"

	"asika/common/db"
	"asika/common/models"
	"asika/testutil"
)

func TestConcurrentWebhookDedup_SingleDelivery(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	deliveryID := "del-concurrent-1"
	ts, _ := time.Now().MarshalBinary()

	var wg sync.WaitGroup
	errors := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errors[idx] = db.PutWebhookDedup(deliveryID, ts)
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("goroutine %d: PutWebhookDedup failed: %v", i, err)
		}
	}

	got, err := db.GetWebhookDedup(deliveryID)
	if err != nil {
		t.Fatalf("GetWebhookDedup failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected dedup record to exist")
	}
}

func TestConcurrentWebhookDedup_MultipleDeliveries(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	var wg sync.WaitGroup
	count := 50

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			deliveryID := "del-multi-" + string(rune('A'+idx%26))
			ts, _ := time.Now().MarshalBinary()
			db.PutWebhookDedup(deliveryID, ts)
		}(i)
	}
	wg.Wait()

	dedup, err := db.ListWebhookDedup()
	if err != nil {
		t.Fatalf("ListWebhookDedup failed: %v", err)
	}
	if len(dedup) == 0 {
		t.Error("expected some dedup records after concurrent writes")
	}
}

func TestConcurrentWebhookRetry_ReadWrite(t *testing.T) {
	testutil.NewTestDB(t)
	defer db.Close()

	retry := &models.WebhookRetry{
		ID:         "retry-concurrent",
		DeliveryID: "del-rw",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{"action":"opened"}`),
		FailCount:  1,
		LastError:  "timeout",
		LastFailed: time.Now(),
		NextRetry:  time.Now().Add(2 * time.Second),
	}
	db.PutWebhookRetry(retry)

	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db.GetWebhookRetry("retry-concurrent")
		}()
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r, _ := db.GetWebhookRetry("retry-concurrent")
			if r != nil {
				r.FailCount++
				r.LastError = "updated"
				db.PutWebhookRetry(r)
			}
		}(i)
	}

	wg.Wait()
}
