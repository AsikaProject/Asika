package db

import (
	"testing"
	"time"

	"asika/common/models"
)

func TestPutWebhookRetry_And_GetWebhookRetry(t *testing.T) {
	initTestDB(t)

	retry := &models.WebhookRetry{
		ID:         "retry-1",
		DeliveryID: "delivery-123",
		RepoGroup:  "frontend",
		Platform:   "github",
		Body:       []byte(`{"action":"opened"}`),
		FailCount:  2,
		LastError:  "timeout",
		LastFailed: time.Now(),
		NextRetry:  time.Now().Add(5 * time.Minute),
	}

	err := PutWebhookRetry(retry)
	if err != nil {
		t.Fatalf("PutWebhookRetry failed: %v", err)
	}

	got, err := GetWebhookRetry("retry-1")
	if err != nil {
		t.Fatalf("GetWebhookRetry failed: %v", err)
	}
	if got.DeliveryID != retry.DeliveryID {
		t.Errorf("DeliveryID mismatch: got %q, want %q", got.DeliveryID, retry.DeliveryID)
	}
	if got.FailCount != retry.FailCount {
		t.Errorf("FailCount mismatch: got %d, want %d", got.FailCount, retry.FailCount)
	}
}

func TestGetWebhookRetry_NotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetWebhookRetry("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteWebhookRetry(t *testing.T) {
	initTestDB(t)

	retry := &models.WebhookRetry{ID: "to-delete", DeliveryID: "d1"}
	PutWebhookRetry(retry)

	err := DeleteWebhookRetry("to-delete")
	if err != nil {
		t.Fatalf("DeleteWebhookRetry failed: %v", err)
	}

	_, err = GetWebhookRetry("to-delete")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestGetDueWebhookRetries(t *testing.T) {
	initTestDB(t)

	now := time.Now()

	PutWebhookRetry(&models.WebhookRetry{
		ID:        "due-1",
		NextRetry: now.Add(-10 * time.Minute),
	})
	PutWebhookRetry(&models.WebhookRetry{
		ID:        "due-2",
		NextRetry: now.Add(-5 * time.Minute),
	})
	PutWebhookRetry(&models.WebhookRetry{
		ID:        "not-due",
		NextRetry: now.Add(10 * time.Minute),
	})
	PutWebhookRetry(&models.WebhookRetry{
		ID:        "zero-time",
		NextRetry: time.Time{},
	})

	due, err := GetDueWebhookRetries(now)
	if err != nil {
		t.Fatalf("GetDueWebhookRetries failed: %v", err)
	}
	if len(due) != 2 {
		t.Errorf("expected 2 due retries (past only, zero-time is skipped), got %d", len(due))
	}
}

func TestGetDueWebhookRetries_Empty(t *testing.T) {
	initTestDB(t)

	due, err := GetDueWebhookRetries(time.Now())
	if err != nil {
		t.Fatalf("GetDueWebhookRetries failed: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("expected 0 due retries, got %d", len(due))
	}
}

func TestListWebhookHealth(t *testing.T) {
	initTestDB(t)

	PutWebhookHealth("frontend", "github", time.Now())
	PutWebhookHealth("backend", "gitlab", time.Now())

	health, err := ListWebhookHealth()
	if err != nil {
		t.Fatalf("ListWebhookHealth failed: %v", err)
	}
	if len(health) != 2 {
		t.Errorf("expected 2 health entries, got %d", len(health))
	}
}
