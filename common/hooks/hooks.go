package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"asika/common/events"
	"asika/common/models"
)

const (
	hookRequestTimeout  = 10 * time.Second
	hookSignatureHeader = "X-Asika-Hook-Signature"
)

var (
	blockInternalURLs = true
	blockedNetworks   = []*net.IPNet{
		mustParseCIDR("127.0.0.0/8"),
		mustParseCIDR("10.0.0.0/8"),
		mustParseCIDR("172.16.0.0/12"),
		mustParseCIDR("192.168.0.0/16"),
		mustParseCIDR("169.254.0.0/16"),
		mustParseCIDR("::1/128"),
		mustParseCIDR("fc00::/7"),
	}
)

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func isBlockedURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := u.Hostname()
	if host == "" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return true
	}
	for _, addr := range addrs {
		for _, n := range blockedNetworks {
			if n.Contains(addr.IP) {
				return true
			}
		}
	}
	return false
}

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

func (r *RetryConfig) backoffDuration(attempt int) time.Duration {
	if attempt > 30 {
		attempt = 30
	}
	base := time.Second
	if r != nil && r.Backoff != "" {
		if d, err := time.ParseDuration(r.Backoff); err == nil {
			base = d
		}
	}
	return base << uint(attempt)
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
	if blockInternalURLs && isBlockedURL(h.URL) {
		slog.Warn("hooks: blocked internal URL", "url", h.URL)
		return
	}

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

	maxAttempts := 1
	if h.Retry != nil && h.Retry.MaxAttempts > 0 {
		maxAttempts = h.Retry.MaxAttempts
	}

	var resp *http.Response
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			dur := h.Retry.backoffDuration(attempt - 1)
			slog.Info("hooks: retrying", "url", h.URL, "attempt", attempt+1, "backoff", dur)
			time.Sleep(dur)
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

		resp, err = client.Do(req)
		if err != nil {
			slog.Warn("hooks: request failed", "url", h.URL, "error", err, "attempt", attempt+1)
			continue
		}

		if resp.StatusCode < 400 {
			resp.Body.Close()
			slog.Debug("hooks: dispatched", "url", h.URL, "event", event.Type)
			return
		}

		slog.Warn("hooks: non-2xx response", "url", h.URL, "status", resp.StatusCode, "attempt", attempt+1)
		resp.Body.Close()
	}

	slog.Error("hooks: all attempts failed", "url", h.URL, "max_attempts", maxAttempts)
}

type Dispatcher struct {
	hooks   []Hook
	ch      <-chan events.Event
	client  *http.Client
	stopCh  chan struct{}
	mu      sync.Mutex
	started bool
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
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return
	}
	d.started = true
	d.mu.Unlock()

	d.ch = events.Subscribe()
	go d.runLoop()
	slog.Info("hooks dispatcher started", "count", len(d.hooks))
}

func (d *Dispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.started {
		return
	}
	d.started = false
	close(d.stopCh)
	if d.ch != nil {
		events.Unsubscribe(d.ch)
	}
}

func (d *Dispatcher) Reload(hooks []Hook) {
	d.mu.Lock()
	d.hooks = hooks
	if d.started {
		close(d.stopCh)
		if d.ch != nil {
			events.Unsubscribe(d.ch)
		}
		d.stopCh = make(chan struct{})
		d.ch = events.Subscribe()
		d.mu.Unlock()
		go d.runLoop()
	} else {
		d.stopCh = make(chan struct{})
		d.started = true
		d.mu.Unlock()
		d.ch = events.Subscribe()
		go d.runLoop()
	}
}

func (d *Dispatcher) runLoop() {
	for {
		select {
		case event, ok := <-d.ch:
			if !ok {
				return
			}
			d.mu.Lock()
			hooks := d.hooks
			d.mu.Unlock()
			for i := range hooks {
				hook := &hooks[i]
				if hook.matches(event) {
					go hook.fire(d.client, event)
				}
			}
		case <-d.stopCh:
			return
		}
	}
}

func (d *Dispatcher) FireForTest(event events.Event) error {
	d.mu.Lock()
	hooks := d.hooks
	d.mu.Unlock()

	for i := range hooks {
		hook := &hooks[i]
		if hook.matches(event) {
			if blockInternalURLs && isBlockedURL(hook.URL) {
				return fmt.Errorf("blocked internal URL: %s", hook.URL)
			}
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
