package hooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"asika/common/events"
	"asika/common/models"
)

const (
	hookRequestTimeout  = 10 * time.Second
	hookSignatureHeader = "X-Asika-Hook-Signature"
)

type Hook struct {
	Events []string      `toml:"events" json:"events"`
	URL    string        `toml:"url" json:"url"`
	Secret string        `toml:"secret" json:"secret"`
	Retry  *RetryConfig  `toml:"retry" json:"retry,omitempty"`
	Filter *FilterConfig `toml:"filter" json:"filter,omitempty"`
}

type RetryConfig struct {
	MaxAttempts int    `toml:"max_attempts" json:"max_attempts"`
	Backoff     string `toml:"backoff" json:"backoff"`
}

type FilterConfig struct {
	RepoGroups []string `toml:"repo_groups" json:"repo_groups,omitempty"`
	Platforms  []string `toml:"platforms" json:"platforms,omitempty"`
}

type HookPayload struct {
	Event     events.EventType `json:"event"`
	RepoGroup string           `json:"repo_group"`
	Platform  string           `json:"platform"`
	Timestamp time.Time        `json:"timestamp"`
	PR        *PRSummary       `json:"pr,omitempty"`
	Payload   interface{}      `json:"payload,omitempty"`
}

type PRSummary struct {
	ID          string   `json:"id"`
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	State       string   `json:"state"`
	Labels      []string `json:"labels"`
	URL         string   `json:"url"`
	MergeCommit string   `json:"merge_commit,omitempty"`
	IsDraft     bool     `json:"is_draft,omitempty"`
	IsConflict  bool     `json:"has_conflict,omitempty"`
}

func hooksPRSummary(pr *models.PRRecord) *PRSummary {
	if pr == nil {
		return nil
	}
	return &PRSummary{
		ID:          pr.ID,
		Number:      pr.PRNumber,
		Title:       pr.Title,
		Author:      pr.Author,
		State:       pr.State,
		Labels:      pr.Labels,
		URL:         pr.HTMLURL,
		MergeCommit: pr.MergeCommitSHA,
		IsDraft:     pr.IsDraft,
		IsConflict:  pr.HasConflict,
	}
}

func (h *Hook) matches(event events.Event) bool {
	if h.Filter != nil {
		if len(h.Filter.RepoGroups) > 0 {
			found := false
			for _, rg := range h.Filter.RepoGroups {
				if rg == event.RepoGroup {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		if len(h.Filter.Platforms) > 0 {
			found := false
			for _, p := range h.Filter.Platforms {
				if p == event.Platform {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	if len(h.Events) == 0 {
		return true
	}
	for _, e := range h.Events {
		if e == string(event.Type) {
			return true
		}
	}
	return false
}

func (h *Hook) fire(client *http.Client, event events.Event) {
	payload := HookPayload{
		Event:     event.Type,
		RepoGroup: event.RepoGroup,
		Platform:  event.Platform,
		Timestamp: event.Timestamp,
		PR:        hooksPRSummary(event.PR),
		Payload:   event.Payload,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("hooks: failed to marshal payload", "url", h.URL, "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(data))
	if err != nil {
		slog.Error("hooks: failed to create request", "url", h.URL, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	if h.Secret != "" {
		sign := hmac.New(sha256.New, []byte(h.Secret))
		sign.Write(data)
		req.Header.Set(hookSignatureHeader, "sha256="+hex.EncodeToString(sign.Sum(nil)))
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("hooks: request failed", "url", h.URL, "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("hooks: non-2xx response", "url", h.URL, "status", resp.StatusCode)
		return
	}

	slog.Debug("hooks: dispatched", "url", h.URL, "event", event.Type)
}

type Dispatcher struct {
	hooks  []Hook
	ch     <-chan events.Event
	client *http.Client
	stopCh chan struct{}
}

func NewDispatcher(hooks []Hook) *Dispatcher {
	return &Dispatcher{
		hooks: hooks,
		client: &http.Client{
			Timeout: hookRequestTimeout,
		},
		stopCh: make(chan struct{}),
	}
}

func (d *Dispatcher) Start() {
	d.ch = events.Subscribe()
	go func() {
		for {
			select {
			case event, ok := <-d.ch:
				if !ok {
					return
				}
				for i := range d.hooks {
					hook := &d.hooks[i]
					if hook.matches(event) {
						go hook.fire(d.client, event)
					}
				}
			case <-d.stopCh:
				return
			}
		}
	}()
	slog.Info("hooks dispatcher started", "count", len(d.hooks))
}

func (d *Dispatcher) Stop() {
	close(d.stopCh)
	if d.ch != nil {
		events.Unsubscribe(d.ch)
	}
}

func (d *Dispatcher) Reload(hooks []Hook) {
	d.Stop()
	d.hooks = hooks
	d.stopCh = make(chan struct{})
	d.Start()
}

func (d *Dispatcher) FireForTest(event events.Event) error {
	for i := range d.hooks {
		hook := &d.hooks[i]
		if hook.matches(event) {
			payload := HookPayload{
				Event:     event.Type,
				RepoGroup: event.RepoGroup,
				Platform:  event.Platform,
				Timestamp: event.Timestamp,
				PR:        hooksPRSummary(event.PR),
				Payload:   event.Payload,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("failed to marshal: %w", err)
			}
			req, err := http.NewRequest(http.MethodPost, hook.URL, bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if hook.Secret != "" {
				sign := hmac.New(sha256.New, []byte(hook.Secret))
				sign.Write(data)
				req.Header.Set(hookSignatureHeader, "sha256="+hex.EncodeToString(sign.Sum(nil)))
			}
			resp, err := d.client.Do(req)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("non-2xx response: %d", resp.StatusCode)
			}
		}
	}
	return nil
}
