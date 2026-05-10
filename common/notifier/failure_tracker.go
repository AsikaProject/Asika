package notifier

import (
	"fmt"
	"log/slog"
	"sync"
)

const defaultFailureThreshold = 3

type notifierFailureState struct {
	consecutiveFailures int
	lastError           string
	alerted             bool
}

// FailureTracker tracks consecutive send failures per notifier and triggers
// alerts when the threshold is reached.
type FailureTracker struct {
	mu        sync.Mutex
	states    map[string]*notifierFailureState
	threshold int
	alertFn   func(notifierType string, failures int, lastErr string)
}

// NewFailureTracker creates a new FailureTracker.
// alertFn is called when a notifier reaches the failure threshold.
func NewFailureTracker(alertFn func(notifierType string, failures int, lastErr string)) *FailureTracker {
	return &FailureTracker{
		states:    make(map[string]*notifierFailureState),
		threshold: defaultFailureThreshold,
		alertFn:   alertFn,
	}
}

// RecordSuccess clears the failure state for a notifier.
func (ft *FailureTracker) RecordSuccess(notifierType string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if state, exists := ft.states[notifierType]; exists {
		if state.consecutiveFailures > 0 || state.alerted {
			slog.Info("notifier recovered", "type", notifierType, "previous_failures", state.consecutiveFailures)
		}
		delete(ft.states, notifierType)
	}
}

// RecordFailure records a send failure for a notifier.
// Returns true if the failure threshold has been reached (alert should fire).
func (ft *FailureTracker) RecordFailure(notifierType string, err error) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	state, exists := ft.states[notifierType]
	if !exists {
		state = &notifierFailureState{}
		ft.states[notifierType] = state
	}

	state.consecutiveFailures++
	state.lastError = err.Error()

	if state.consecutiveFailures >= ft.threshold && !state.alerted {
		state.alerted = true
		slog.Error("notifier failure threshold reached", "type", notifierType, "failures", state.consecutiveFailures, "error", state.lastError)
		if ft.alertFn != nil {
			ft.alertFn(notifierType, state.consecutiveFailures, state.lastError)
		}
		return true
	}

	slog.Warn("notifier send failed", "type", notifierType, "consecutive_failures", state.consecutiveFailures, "error", state.lastError)
	return false
}

// Reset clears all failure states.
func (ft *FailureTracker) Reset() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.states = make(map[string]*notifierFailureState)
}

// Status returns the current failure state for all notifiers.
func (ft *FailureTracker) Status() map[string]FailureStatus {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	result := make(map[string]FailureStatus, len(ft.states))
	for t, s := range ft.states {
		result[t] = FailureStatus{
			ConsecutiveFailures: s.consecutiveFailures,
			LastError:           s.lastError,
			Alerted:             s.alerted,
		}
	}
	return result
}

// FailureStatus describes the failure state of a single notifier.
type FailureStatus struct {
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastError           string `json:"last_error"`
	Alerted             bool   `json:"alerted"`
}

// AlertMessage builds a fault alert title and body.
func AlertMessage(notifierType string, failures int, lastErr string) (string, string) {
	title := fmt.Sprintf("[Fault Alert] Notifier %s failed %d consecutive times", notifierType, failures)
	body := fmt.Sprintf("Notifier type: %s\nConsecutive failures: %d\nLast error: %s\n\nPlease check this notifier's configuration and connectivity.", notifierType, failures, lastErr)
	return title, body
}
