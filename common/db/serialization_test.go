package db

import (
	"encoding/json"
	"testing"
	"time"

	"asika/common/models"
)

func TestPutPRWithIndex_CorruptedData(t *testing.T) {
	dir := t.TempDir()
	Init(dir + "/test.db")
	t.Cleanup(func() { Close() })

	validPR := &models.PRRecord{
		ID:        "corrupt-test-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}
	data, _ := json.Marshal(validPR)
	PutPRWithIndex("test-group#github#1", data, "corrupt-test-1", "test-group", 1)

	corruptedData := []byte(`{invalid json}`)
	Put("test-group#github#1", "corrupted", corruptedData)

	var result models.PRRecord
	err := json.Unmarshal(corruptedData, &result)
	if err == nil {
		t.Error("expected error unmarshaling corrupted data")
	}
}

func TestGetPRByIndex_NonexistentKey(t *testing.T) {
	dir := t.TempDir()
	Init(dir + "/test.db")
	t.Cleanup(func() { Close() })

	data, err := GetPRByIndex("nonexistent-id", "", 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for nonexistent key, got %d bytes", len(data))
	}
}

func TestGetPRByIndex_EmptyID(t *testing.T) {
	dir := t.TempDir()
	Init(dir + "/test.db")
	t.Cleanup(func() { Close() })

	data, err := GetPRByIndex("", "test-group", 1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for empty ID, got %d bytes", len(data))
	}
}

func TestPRRecord_JSONRoundTrip(t *testing.T) {
	original := &models.PRRecord{
		ID:        "roundtrip-1",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  42,
		Title:     "Test PR",
		Body:      "This is the PR body\nwith multiple lines",
		Author:    "dev1",
		State:     "open",
		SpamFlag:  false,
		Labels:    []string{"bug", "urgent"},
		Events: []models.PREvent{
			{
				Timestamp: time.Now(),
				Action:    "opened",
				Actor:     "dev1",
			},
			{
				Timestamp: time.Now(),
				Action:    "labeled",
				Actor:     "bot",
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded models.PRRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Title != original.Title {
		t.Errorf("Title = %q, want %q", decoded.Title, original.Title)
	}
	if decoded.Body != original.Body {
		t.Errorf("Body = %q, want %q", decoded.Body, original.Body)
	}
	if len(decoded.Labels) != len(original.Labels) {
		t.Errorf("Labels length = %d, want %d", len(decoded.Labels), len(original.Labels))
	}
	if len(decoded.Events) != len(original.Events) {
		t.Errorf("Events length = %d, want %d", len(decoded.Events), len(original.Events))
	}
}

func TestPRRecord_JSONEmptyFields(t *testing.T) {
	minimal := &models.PRRecord{
		ID: "minimal-1",
	}

	data, err := json.Marshal(minimal)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded models.PRRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != "minimal-1" {
		t.Errorf("ID = %q, want minimal-1", decoded.ID)
	}
	if decoded.Labels != nil {
		t.Errorf("expected nil Labels, got %v", decoded.Labels)
	}
	if decoded.Events != nil {
		t.Errorf("expected nil Events, got %v", decoded.Events)
	}
}

func TestPRRecord_JSONSpecialCharacters(t *testing.T) {
	special := &models.PRRecord{
		ID:    "special-1",
		Title: "PR with \"quotes\" and <html> & special chars: 日本語 🎉",
		Body:  "Line1\nLine2\r\nLine3\tTabbed",
	}

	data, err := json.Marshal(special)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded models.PRRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Title != special.Title {
		t.Errorf("Title = %q, want %q", decoded.Title, special.Title)
	}
	if decoded.Body != special.Body {
		t.Errorf("Body = %q, want %q", decoded.Body, special.Body)
	}
}

func TestPRRecord_JSONLargePayload(t *testing.T) {
	largeBody := make([]byte, 100000)
	for i := range largeBody {
		largeBody[i] = byte('A' + (i % 26))
	}

	pr := &models.PRRecord{
		ID:     "large-1",
		Title:  "Large PR",
		Body:   string(largeBody),
		Labels: make([]string, 100),
	}
	for i := range pr.Labels {
		pr.Labels[i] = "label-" + string(rune('A'+i%26))
	}

	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("Marshal large payload failed: %v", err)
	}

	var decoded models.PRRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal large payload failed: %v", err)
	}

	if len(decoded.Body) != 100000 {
		t.Errorf("Body length = %d, want 100000", len(decoded.Body))
	}
	if len(decoded.Labels) != 100 {
		t.Errorf("Labels count = %d, want 100", len(decoded.Labels))
	}
}

func TestPRRecord_JSONNullFields(t *testing.T) {
	data := []byte(`{"id":"null-test","title":null,"body":null,"labels":null,"events":null}`)

	var pr models.PRRecord
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("Unmarshal null fields failed: %v", err)
	}

	if pr.ID != "null-test" {
		t.Errorf("ID = %q, want null-test", pr.ID)
	}
}

func TestPRRecord_JSONUnknownFields(t *testing.T) {
	data := []byte(`{"id":"unknown-test","unknown_field":"value","another":123}`)

	var pr models.PRRecord
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("Unmarshal with unknown fields failed: %v", err)
	}

	if pr.ID != "unknown-test" {
		t.Errorf("ID = %q, want unknown-test", pr.ID)
	}
}

func TestWebhookRetry_JSONRoundTrip(t *testing.T) {
	original := &models.WebhookRetry{
		ID:         "retry-roundtrip",
		DeliveryID: "del-789",
		RepoGroup:  "test-group",
		Platform:   "github",
		Body:       []byte(`{"action":"opened","number":1}`),
		FailCount:  3,
		LastError:  "timeout",
		LastFailed: time.Now(),
		NextRetry:  time.Now().Add(5 * time.Minute),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded models.WebhookRetry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.FailCount != original.FailCount {
		t.Errorf("FailCount = %d, want %d", decoded.FailCount, original.FailCount)
	}
	if string(decoded.Body) != string(original.Body) {
		t.Errorf("Body mismatch")
	}
}

func TestQueueItem_JSONRoundTrip(t *testing.T) {
	original := &models.QueueItem{
		PRID:        "queue-roundtrip",
		RepoGroup:   "test-group",
		Status:      "waiting",
		AddedAt:     time.Now(),
		LastChecked: time.Now(),
		Criteria: models.MergeCriteria{
			RequiredApprovals: 2,
			CIStatus:          "pending",
		},
		ScheduleAt: time.Now().Add(1 * time.Hour),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded models.QueueItem
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.PRID != original.PRID {
		t.Errorf("PRID = %q, want %q", decoded.PRID, original.PRID)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, original.Status)
	}
	if decoded.Criteria.RequiredApprovals != original.Criteria.RequiredApprovals {
		t.Errorf("RequiredApprovals = %d, want %d", decoded.Criteria.RequiredApprovals, original.Criteria.RequiredApprovals)
	}
}

func TestGetPRByIndex_CorruptedEntry(t *testing.T) {
	dir := t.TempDir()
	Init(dir + "/test.db")
	t.Cleanup(func() { Close() })

	validPR := &models.PRRecord{
		ID:        "valid-after-corrupt",
		RepoGroup: "test-group",
		Platform:  "github",
		PRNumber:  2,
		Title:     "Valid PR",
		State:     "open",
	}
	data, _ := json.Marshal(validPR)
	PutPRWithIndex("test-group#github#2", data, "valid-after-corrupt", "test-group", 2)

	Put(BucketPRs, "test-group#github#99", []byte(`{corrupted`))

	result, err := GetPRByIndex("valid-after-corrupt", "", 0)
	if err != nil {
		t.Fatalf("GetPRByIndex failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected to find valid PR despite corrupted entry")
	}
	var found models.PRRecord
	json.Unmarshal(result, &found)
	if found.Title != "Valid PR" {
		t.Errorf("Title = %q, want Valid PR", found.Title)
	}
}

func TestGetPRByIndex_AllCorrupted(t *testing.T) {
	dir := t.TempDir()
	Init(dir + "/test.db")
	t.Cleanup(func() { Close() })

	Put(BucketPRs, "test-group#github#1", []byte(`{corrupted1`))
	Put(BucketPRs, "test-group#github#2", []byte(`{corrupted2`))

	result, err := GetPRByIndex("nonexistent", "", 0)
	if err != nil {
		t.Fatalf("GetPRByIndex failed: %v", err)
	}
	if result != nil {
		t.Error("expected nil for nonexistent PR")
	}
}

func TestPutAndGet_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	Init(dir + "/test.db")
	t.Cleanup(func() { Close() })

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(n int) {
			pr := &models.PRRecord{
				ID:        "concurrent-" + string(rune('A'+n%26)),
				RepoGroup: "test-group",
				Platform:  "github",
				PRNumber:  n + 1,
				Title:     "Concurrent PR",
				State:     "open",
			}
			data, _ := json.Marshal(pr)
			key := "test-group#github#" + string(rune('1'+n%26))
			Put(BucketPRs, key, data)
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}
