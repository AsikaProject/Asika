package events

import (
	"log/slog"
	"sync"
	"time"

	"asika/common/models"
)

// EventType defines event types
type EventType string

const (
	EventPROpened              EventType = "pr_opened"
	EventPRClosed              EventType = "pr_closed"
	EventPRMerged              EventType = "pr_merged"
	EventPRApproved            EventType = "pr_approved"
	EventPRLabeled             EventType = "pr_labeled"
	EventPRSynced              EventType = "pr_synced"
	EventPRReopened            EventType = "pr_reopened"
	EventPRComment             EventType = "pr_comment"
	EventSpamDetected          EventType = "spam_detected"
	EventBranchDeleted         EventType = "branch_deleted"
	EventSyncCompleted         EventType = "sync_completed"
	EventSyncFailed            EventType = "sync_failed"
	EventPRReverted            EventType = "pr_reverted"
	EventNotificationEscalated EventType = "notification_escalated"
	EventBranchPushed          EventType = "branch_pushed"
	EventTagPushed             EventType = "tag_pushed"
)

// Event is the internal unified event
type Event struct {
	Type      EventType
	RepoGroup string
	Platform  string
	PR        *models.PRRecord
	Timestamp time.Time
	Payload   interface{}
}

// Bus is the event bus
type Bus struct {
	subscribers []chan Event
	mu          sync.RWMutex
}

var globalBus *Bus

// Init initializes the global event bus
func Init() {
	globalBus = &Bus{
		subscribers: make([]chan Event, 0),
	}
}

// Subscribe subscribes to events, returns a channel to receive events
func Subscribe() <-chan Event {
	ch := make(chan Event, 100)
	globalBus.mu.Lock()
	globalBus.subscribers = append(globalBus.subscribers, ch)
	globalBus.mu.Unlock()
	return ch
}

// Publish publishes an event to all subscribers.
// Uses non-blocking send: if a subscriber's channel is full, the event is dropped
// for that subscriber and logged, preventing slow subscribers from blocking others.
func Publish(e Event) {
	globalBus.mu.RLock()
	defer globalBus.mu.RUnlock()
	for _, ch := range globalBus.subscribers {
		select {
		case ch <- e:
		default:
			slog.Warn("event dropped: subscriber channel full", "event_type", e.Type)
		}
	}
}

// PublishPR publishes a PR-related event
func PublishPR(eventType EventType, repoGroup, platform string, pr *models.PRRecord, payload interface{}) {
	Publish(Event{
		Type:      eventType,
		RepoGroup: repoGroup,
		Platform:  platform,
		PR:        pr,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}
