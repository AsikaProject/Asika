package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultRequestTimeout = 60 * time.Second
	maxResponseSize       = 1 << 20
)

type Config struct {
	APIKey  string `toml:"api_key" json:"api_key"`
	BaseURL string `toml:"base_url" json:"base_url"`
	Model   string `toml:"model" json:"model"`
}

type Client struct {
	config Config
	http   *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewClient(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Client{
		config: Config{
			APIKey:  cfg.APIKey,
			BaseURL: baseURL,
			Model:   model,
		},
		http: &http.Client{Timeout: defaultRequestTimeout},
	}
}

func (c *Client) Chat(system, user string) (string, error) {
	reqBody := chatRequest{
		Model: c.config.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("llm marshal: %w", err)
	}

	url := c.config.BaseURL + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return "", fmt.Errorf("llm read: %w", err)
	}

	var result chatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("llm parse: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("llm API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("llm: no choices returned")
	}

	return result.Choices[0].Message.Content, nil
}
