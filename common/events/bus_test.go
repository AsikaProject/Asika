package events

import (
	"testing"
	"time"

	"asika/common/models"
)

func TestInit(t *testing.T) {
	Init()
	if globalBus == nil {
		t.Error("globalBus should be initialized after Init()")
	}
	if len(globalBus.subscribers) != 0 {
		t.Errorf("expected 0 subscribers, got %d", len(globalBus.subscribers))
	}
}

func TestSubscribe(t *testing.T) {
	Init()

	ch := Subscribe()
	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	if cap(ch) != 100 {
		t.Errorf("channel capacity = %d, want 100", cap(ch))
	}
}

func TestPublishAndSubscribe(t *testing.T) {
	Init()

	ch := Subscribe()

	// Publish event
	pr := &models.PRRecord{
		ID:        "pr-123",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  1,
		Title:     "Test PR",
		State:     "open",
	}

	Publish(Event{
		Type:      EventPROpened,
		RepoGroup: "main",
		Platform:  "github",
		PR:        pr,
		Timestamp: time.Now(),
	})

	// Receive event (with timeout)
	select {
	case event := <-ch:
		if event.Type != EventPROpened {
			t.Errorf("event.Type = %v, want %v", event.Type, EventPROpened)
		}
		if event.RepoGroup != "main" {
			t.Errorf("event.RepoGroup = %q, want main", event.RepoGroup)
		}
		if event.PR == nil {
			t.Error("event.PR should not be nil")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestPublishPR(t *testing.T) {
	Init()

	ch := Subscribe()

	pr := &models.PRRecord{
		ID:        "pr-456",
		RepoGroup: "main",
		Platform:  "gitlab",
		PRNumber:  2,
		Title:     "Test PR 2",
		State:     "open",
	}

	PublishPR(EventPRMerged, "main", "gitlab", pr, nil)

	select {
	case event := <-ch:
		if event.Type != EventPRMerged {
			t.Errorf("event.Type = %v, want %v", event.Type, EventPRMerged)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	Init()

	ch1 := Subscribe()
	ch2 := Subscribe()

	pr := &models.PRRecord{
		ID:        "pr-789",
		RepoGroup: "main",
		Platform:  "gitea",
		PRNumber:  3,
		Title:     "Test PR 3",
		State:     "open",
	}

	PublishPR(EventPRLabeled, "main", "gitea", pr, nil)

	// Both subscribers should receive the event
	select {
	case event := <-ch1:
		if event.Type != EventPRLabeled {
			t.Errorf("ch1: event.Type = %v, want %v", event.Type, EventPRLabeled)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event on ch1")
	}

	select {
	case event := <-ch2:
		if event.Type != EventPRLabeled {
			t.Errorf("ch2: event.Type = %v, want %v", event.Type, EventPRLabeled)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event on ch2")
	}
}

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name  string
		value EventType
	}{
		{"EventPROpened", EventPROpened},
		{"EventPRClosed", EventPRClosed},
		{"EventPRMerged", EventPRMerged},
		{"EventPRApproved", EventPRApproved},
		{"EventPRLabeled", EventPRLabeled},
		{"EventPRSynced", EventPRSynced},
		{"EventPRReopened", EventPRReopened},
		{"EventSpamDetected", EventSpamDetected},
		{"EventBranchDeleted", EventBranchDeleted},
		{"EventSyncCompleted", EventSyncCompleted},
		{"EventSyncFailed", EventSyncFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				t.Error("EventType should not be empty")
			}
		})
	}
}

func TestPublishBackpressure(t *testing.T) {
	Init()

	// Create a channel but don't read from it to fill the buffer
	ch := Subscribe()
	// Fill the channel buffer
	for i := 0; i < 100; i++ {
		pr := &models.PRRecord{
			ID:        "pr-full",
			RepoGroup: "main",
			Platform:  "github",
			PRNumber:  i,
			State:     "open",
		}
		PublishPR(EventPROpened, "main", "github", pr, nil)
	}

	// Publish the 101st event should block until consumer reads.
	// Use a goroutine with a timeout to verify backpressure works.
	pr := &models.PRRecord{
		ID:        "pr-overflow",
		RepoGroup: "main",
		Platform:  "github",
		PRNumber:  101,
		State:     "open",
	}

	done := make(chan struct{})
	go func() {
		PublishPR(EventPROpened, "main", "github", pr, nil)
		close(done)
	}()

	// Give the goroutine time to attempt the publish
	time.Sleep(50 * time.Millisecond)

	// The publish should still be blocked (channel full)
	select {
	case <-done:
		t.Fatal("Publish should have been blocked on full channel")
	default:
		// Expected: still blocked
	}

	// Drain one slot to unblock the publisher
	<-ch

	// Now the publish should complete
	select {
	case <-done:
		// Expected: unblocked after drain
	case <-time.After(time.Second):
		t.Fatal("Publish should have completed after draining one slot")
	}

	_ = ch
}
