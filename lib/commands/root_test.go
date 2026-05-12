package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestIsAPIKey(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"ak_1234567890abcdef", true},
		{"ak_", true},
		{"Bearer token123", false},
		{"jwt.token.here", false},
		{"", false},
		{"AK_123", false},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got := isAPIKey(tt.token)
			if got != tt.want {
				t.Errorf("isAPIKey(%q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
}

func TestSetAuthHeader_APIKey(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	setAuthHeader(req, "ak_testkey123")

	if got := req.Header.Get("X-API-Key"); got != "ak_testkey123" {
		t.Errorf("X-API-Key = %q, want ak_testkey123", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization should be empty for API key, got %q", got)
	}
}

func TestSetAuthHeader_JWT(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	setAuthHeader(req, "my.jwt.token")

	if got := req.Header.Get("Authorization"); got != "Bearer my.jwt.token" {
		t.Errorf("Authorization = %q, want Bearer my.jwt.token", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "" {
		t.Errorf("X-API-Key should be empty for JWT, got %q", got)
	}
}

func TestSetAuthHeader_EmptyToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	setAuthHeader(req, "")

	// Empty token is not an API key (no "ak_" prefix), so it goes to JWT branch
	if got := req.Header.Get("Authorization"); got != "Bearer " {
		t.Errorf("Authorization = %q, want Bearer ", got)
	}
}

func TestHandleResponse_Array(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       http.NoBody,
	}
	// Empty body will fail JSON unmarshal, should print emptyMsg
	result := handleResponse(resp, "no data")
	if result != nil {
		t.Errorf("handleResponse = %v, want nil", result)
	}
}

func TestHandleResponse_ObjectWithError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error": "something went wrong"}`))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	result := handleResponse(resp, "no data")
	if result != nil {
		t.Errorf("handleResponse = %v, want nil for error response", result)
	}
}

func TestHandleResponse_ObjectWithMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message": "success"}`))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	result := handleResponse(resp, "no data")
	if result != nil {
		t.Errorf("handleResponse = %v, want nil for message response", result)
	}
}

func TestHandleResponse_DataArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data": [{"id": 1}, {"id": 2}]}`))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	result := handleResponse(resp, "no data")
	if len(result) != 2 {
		t.Errorf("handleResponse len = %d, want 2", len(result))
	}
}

func TestHandleWriteResponse_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message": "created"}`))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	// Should not panic
	handleWriteResponse(resp, "default success")
}

func TestHandleWriteResponse_PlainText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	handleWriteResponse(resp, "default success")
}

func TestHandleObjectResponse_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"key": "value"}`))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	// Should not panic
	handleObjectResponse(resp, "no data")
}

func TestHandleObjectResponse_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error": "not found"}`))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	handleObjectResponse(resp, "no data")
}

func TestConfigPath(t *testing.T) {
	path := configPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "asika", "config.json")
	if path != expected {
		t.Errorf("configPath() = %q, want %q", path, expected)
	}
}

func TestLoadSaveCLIConfig(t *testing.T) {
	// Override config path for testing
	origDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", origDir)
	defer t.Setenv("HOME", origHome)

	cfg := cliConfig{
		Token:  "test-token",
		Server: "http://test:8080",
	}
	saveCLIConfig(cfg)

	loaded := loadCLIConfig()
	if loaded.Token != "test-token" {
		t.Errorf("Token = %q, want test-token", loaded.Token)
	}
	if loaded.Server != "http://test:8080" {
		t.Errorf("Server = %q, want http://test:8080", loaded.Server)
	}
}

func TestLoadCLIConfig_NotExist(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "/nonexistent-path-for-test")
	defer t.Setenv("HOME", origHome)

	cfg := loadCLIConfig()
	if cfg.Token != "" || cfg.Server != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func TestNewBuffer(t *testing.T) {
	data := []byte("hello")
	buf := newBuffer(data)
	if buf.String() != "hello" {
		t.Errorf("newBuffer = %q, want hello", buf.String())
	}
}

func TestWatchStream_SSEConnected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Error("expected Accept: text/event-stream header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: connected\ndata: {\"status\":\"connected\"}\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer ts.Close()

	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		cmd := &cobra.Command{}
		cmd.Flags().String("server", ts.URL, "")
		cmd.Flags().String("token", "test-token", "")
		watchStream(cmd, ts.URL, "test-token")
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	w.Close()
	os.Stderr = origStderr
	r.Close()

	<-done
	_ = buf
}

func TestWatchStream_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd := &cobra.Command{}
	watchStream(cmd, ts.URL, "test-token")

	w.Close()
	os.Stderr = origStderr
	r.Close()
}

func TestWatchStream_ConnectionError(t *testing.T) {
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd := &cobra.Command{}
	watchStream(cmd, "http://localhost:1", "test-token")

	w.Close()
	os.Stderr = origStderr
	r.Close()
}

func TestWatchStream_InvalidURL(t *testing.T) {
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd := &cobra.Command{}
	watchStream(cmd, "http://invalid-host-that-does-not-exist.example:12345", "test-token")

	w.Close()
	os.Stderr = origStderr
	r.Close()
}

func TestWatchStreamCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range watchCmd.Commands() {
		if cmd.Use == "stream" {
			found = true
			break
		}
	}
	if !found {
		t.Error("watch stream subcommand not registered")
	}
}
