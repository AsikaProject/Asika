package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient(Config{APIKey: "test-key"})
	if c.config.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want %q", c.config.BaseURL, "https://api.openai.com/v1")
	}
	if c.config.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q, want %q", c.config.Model, "gpt-4o-mini")
	}
	if c.config.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", c.config.APIKey, "test-key")
	}
}

func TestNewClient_CustomURL(t *testing.T) {
	c := NewClient(Config{APIKey: "key", BaseURL: "http://localhost:11434/v1", Model: "deepseek-chat"})
	if c.config.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("BaseURL = %q", "http://localhost:11434/v1")
	}
	if c.config.Model != "deepseek-chat" {
		t.Errorf("Model = %q", "deepseek-chat")
	}
}

func TestChat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "gpt-4o-mini" {
			t.Errorf("Model = %q", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("len messages = %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Errorf("unexpected roles")
		}

		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "Generated summary"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL})
	result, err := client.Chat("You are a helpful assistant", "Summarize this PR")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if result != "Generated summary" {
		t.Errorf("result = %q, want %q", result, "Generated summary")
	}
}

func TestChat_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Error: &struct {
				Message string `json:"message"`
			}{Message: "Rate limit exceeded"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "key", BaseURL: server.URL})
	_, err := client.Chat("system", "user")
	if err == nil || err.Error() != "llm API error: Rate limit exceeded" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChat_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "key", BaseURL: server.URL})
	_, err := client.Chat("system", "user")
	if err == nil || err.Error() != "llm: no choices returned" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChat_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "key", BaseURL: server.URL})
	_, err := client.Chat("system", "user")
	if err == nil {
		t.Fatal("expected error for non-200")
	}
}

func TestChat_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "key", BaseURL: server.URL})
	_, err := client.Chat("system", "user")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
