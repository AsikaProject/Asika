package hooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"asika/common/events"
	"asika/common/models"
)

func TestHookMatches(t *testing.T) {
	event := events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "team-a",
		Platform:  "github",
		Timestamp: time.Now(),
	}

	tests := []struct {
		name string
		hook Hook
		want bool
	}{
		{
			name: "matches by event",
			hook: Hook{Events: []string{"pr_opened"}},
			want: true,
		},
		{
			name: "no match different event",
			hook: Hook{Events: []string{"pr_closed"}},
			want: false,
		},
		{
			name: "no events means all match",
			hook: Hook{Events: nil},
			want: true,
		},
		{
			name: "matches by repo group",
			hook: Hook{Events: []string{"pr_opened"}, Filter: &FilterConfig{RepoGroups: []string{"team-a"}}},
			want: true,
		},
		{
			name: "no match wrong repo group",
			hook: Hook{Events: []string{"pr_opened"}, Filter: &FilterConfig{RepoGroups: []string{"team-b"}}},
			want: false,
		},
		{
			name: "matches by platform",
			hook: Hook{Events: []string{"pr_opened"}, Filter: &FilterConfig{Platforms: []string{"github"}}},
			want: true,
		},
		{
			name: "no match wrong platform",
			hook: Hook{Events: []string{"pr_opened"}, Filter: &FilterConfig{Platforms: []string{"gitlab"}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hook.matches(event); got != tt.want {
				t.Errorf("matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHookFire(t *testing.T) {
	var receivedBody []byte
	var receivedSig string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedSig = r.Header.Get(hookSignatureHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pr := &models.PRRecord{
		ID:       "12345",
		PRNumber: 42,
		Title:    "Test PR",
		Author:   "testuser",
		State:    "open",
		Labels:   []string{"bug"},
		HTMLURL:  "https://github.com/org/repo/pull/42",
	}

	event := events.Event{
		Type:      events.EventPROpened,
		RepoGroup: "team-a",
		Platform:  "github",
		PR:        pr,
		Timestamp: time.Now(),
		Payload:   map[string]string{"key": "val"},
	}

	hook := Hook{
		Events: []string{"pr_opened"},
		URL:    server.URL,
		Secret: "test-secret",
	}

	client := &http.Client{Timeout: 10 * time.Second}
	hook.fire(client, event)

	var payload HookPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Event != events.EventPROpened {
		t.Errorf("event = %v, want pr_opened", payload.Event)
	}
	if payload.RepoGroup != "team-a" {
		t.Errorf("repo_group = %v, want team-a", payload.RepoGroup)
	}
	if payload.Platform != "github" {
		t.Errorf("platform = %v, want github", payload.Platform)
	}
	if payload.PR == nil {
		t.Fatal("PR is nil")
	}
	if payload.PR.Number != 42 {
		t.Errorf("pr number = %v, want 42", payload.PR.Number)
	}
	if payload.PR.Title != "Test PR" {
		t.Errorf("pr title = %v, want Test PR", payload.PR.Title)
	}

	sigPrefix := "sha256="
	if len(receivedSig) <= len(sigPrefix) {
		t.Fatal("missing signature")
	}
	sigHash := receivedSig[len(sigPrefix):]

	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(receivedBody)
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if sigHash != expectedSig {
		t.Errorf("signature mismatch: got %v, want %v", sigHash, expectedSig)
	}
}

func TestDispatcherStartStop(t *testing.T) {
	d := NewDispatcher(nil)
	d.Start()
	time.Sleep(50 * time.Millisecond)
	d.Stop()
}

func TestHookFireNoSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	event := events.Event{
		Type:      events.EventPRClosed,
		RepoGroup: "team-b",
		Platform:  "gitlab",
		Timestamp: time.Now(),
	}

	hook := Hook{
		Events: []string{"pr_closed"},
		URL:    server.URL,
	}
	client := &http.Client{Timeout: 10 * time.Second}
	hook.fire(client, event)
}
