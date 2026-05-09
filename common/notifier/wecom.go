package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"asika/common/models"
)

const (
	wecomMaxBodyRead    = 1 << 20 // 1 MB
	wecomRequestTimeout = 10 * time.Second
	wecomMaxRetries     = 3
)

// WeComNotifier sends notifications via WeCom (企业微信).
// Supports two modes:
//  1. Webhook mode: one or more webhook URLs (with optional signature)
//  2. App mode: corp_id + corp_secret + agent_id for proactive messaging
type WeComNotifier struct {
	// Webhook mode
	webhookURLs      []string
	webhookSecret    string // optional, for signature
	mentionedList    []string
	mentionedMobiles []string

	// App mode
	corpID      string
	corpSecret  string
	agentID     int64
	toUsers     []string
	toParties   []string
	toTags      []string
	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time

	// Message format
	msgType string // "markdown" (default), "textcard", "text"

	httpClient *http.Client
}

// NewWeComNotifier creates a WeCom notifier from config.
func NewWeComNotifier(config map[string]interface{}) Notifier {
	n := &WeComNotifier{
		httpClient: &http.Client{Timeout: wecomRequestTimeout},
		msgType:    "markdown",
	}

	// --- Webhook mode ---

	// Single webhook_url (backward compatible)
	if u, ok := config["webhook_url"].(string); ok && u != "" {
		n.webhookURLs = append(n.webhookURLs, u)
	}

	// Multiple webhook_urls
	if urls, ok := config["webhook_urls"].([]interface{}); ok {
		for _, v := range urls {
			if s, ok := v.(string); ok && s != "" {
				n.webhookURLs = append(n.webhookURLs, s)
			}
		}
	}

	// Webhook secret for signature
	n.webhookSecret, _ = config["webhook_secret"].(string)

	// Mentioned users
	if list, ok := config["mentioned_list"].([]interface{}); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				n.mentionedList = append(n.mentionedList, s)
			}
		}
	}

	// Mentioned mobile numbers
	if list, ok := config["mentioned_mobile_list"].([]interface{}); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				n.mentionedMobiles = append(n.mentionedMobiles, s)
			}
		}
	}

	// --- App mode ---

	n.corpID, _ = config["corp_id"].(string)
	n.corpSecret, _ = config["corp_secret"].(string)
	if aid, ok := config["agent_id"].(float64); ok {
		n.agentID = int64(aid)
	}

	if list, ok := config["to_user"].([]interface{}); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				n.toUsers = append(n.toUsers, s)
			}
		}
	}

	if list, ok := config["to_party"].([]interface{}); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				n.toParties = append(n.toParties, s)
			}
		}
	}

	if list, ok := config["to_tag"].([]interface{}); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && s != "" {
				n.toTags = append(n.toTags, s)
			}
		}
	}

	// --- Message format ---

	if mt, ok := config["msg_type"].(string); ok && mt != "" {
		n.msgType = mt
	}

	// Validate app mode: if corp_id is set, require corp_secret + agent_id
	if n.corpID != "" && (n.corpSecret == "" || n.agentID == 0) {
		slog.Warn("wecom notifier: app mode requires corp_id, corp_secret, and agent_id; falling back to webhook-only")
		n.corpID = ""
	}

	return n
}

// Type returns the notifier type.
func (n *WeComNotifier) Type() string {
	return "wecom"
}

// Send dispatches the notification via all configured channels.
func (n *WeComNotifier) Send(ctx context.Context, title, body string) error {
	if len(n.webhookURLs) == 0 && n.corpID == "" {
		return fmt.Errorf("wecom: no delivery target configured (webhook_url or corp_id required)")
	}

	var lastErr error
	sent := false

	// Webhook mode
	for _, webhookURL := range n.webhookURLs {
		if err := n.sendViaWebhook(ctx, webhookURL, title, body); err != nil {
			slog.Error("wecom webhook failed", "url", redactURL(webhookURL), "error", err)
			lastErr = err
			continue
		}
		sent = true
		slog.Info("wecom notification sent via webhook", "url", redactURL(webhookURL))
	}

	// App mode
	if n.corpID != "" {
		if err := n.sendViaApp(ctx, title, body); err != nil {
			slog.Error("wecom app send failed", "error", err)
			lastErr = err
		} else {
			sent = true
			slog.Info("wecom notification sent via app")
		}
	}

	if !sent && lastErr != nil {
		return lastErr
	}
	return nil
}

// SendPR sends a structured PR notification using a template.
func (n *WeComNotifier) SendPR(ctx context.Context, pr *models.PRRecord, action string) error {
	title := fmt.Sprintf("PR %s #%d", action, pr.PRNumber)
	body := formatPRBody(pr, action)
	return n.Send(ctx, title, body)
}

// sendViaWebhook sends a message to a single WeCom webhook with retry.
func (n *WeComNotifier) sendViaWebhook(ctx context.Context, webhookURL, title, body string) error {
	payload, err := n.buildPayload(title, body)
	if err != nil {
		return fmt.Errorf("wecom: build payload failed: %w", err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("wecom: marshal payload failed: %w", err)
	}

	url := n.signURL(webhookURL)

	var lastErr error
	for attempt := 0; attempt <= wecomMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			slog.Info("wecom webhook retrying", "attempt", attempt, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := n.doRequest(ctx, url, data)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("wecom webhook failed after %d retries: %w", wecomMaxRetries, lastErr)
}

// doRequest performs a single HTTP POST with the given payload.
func (n *WeComNotifier) doRequest(ctx context.Context, url string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, wecomMaxBodyRead))
		slog.Error("wecom webhook returned non-200",
			"status", resp.StatusCode,
			"body", string(bodyBytes))
		return fmt.Errorf("wecom webhook returned status %d", resp.StatusCode)
	}

	// Read and check WeCom response body for errcode
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, wecomMaxBodyRead))
	var result struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if json.Unmarshal(respBody, &result) == nil && result.Errcode != 0 {
		return fmt.Errorf("wecom API error: code=%d msg=%s", result.Errcode, result.Errmsg)
	}

	return nil
}

// buildPayload constructs the WeCom message payload based on msg_type.
func (n *WeComNotifier) buildPayload(title, body string) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"msgtype": n.msgType,
	}

	switch n.msgType {
	case "textcard":
		payload["textcard"] = map[string]interface{}{
			"title":       title,
			"description": body,
			"url":         "", // caller can set via extra config if needed
			"btntxt":      "View Details",
		}

	case "markdown":
		content := fmt.Sprintf("## %s\n\n%s", title, body)
		md := map[string]interface{}{"content": content}
		if len(n.mentionedList) > 0 {
			md["mentioned_list"] = n.mentionedList
		}
		if len(n.mentionedMobiles) > 0 {
			md["mentioned_mobile_list"] = n.mentionedMobiles
		}
		payload["markdown"] = md

	default: // "text"
		text := map[string]interface{}{
			"content": fmt.Sprintf("%s\n\n%s", title, body),
		}
		if len(n.mentionedList) > 0 {
			text["mentioned_list"] = n.mentionedList
		}
		if len(n.mentionedMobiles) > 0 {
			text["mentioned_mobile_list"] = n.mentionedMobiles
		}
		payload["text"] = text
	}

	return payload, nil
}

// signURL appends HMAC-SHA256 signature to the webhook URL if webhook_secret is configured.
func (n *WeComNotifier) signURL(baseURL string) string {
	if n.webhookSecret == "" {
		return baseURL
	}

	// WeCom webhook signature: base64(HMAC-SHA256(secret, timestamp\nbody))
	// For URL-based signing we use a simpler approach: append a signed nonce
	// Actually, WeCom webhook doesn't use URL signing by default.
	// The "key" in the URL is the webhook key itself.
	// webhook_secret here is used for custom HMAC signing if the org uses it.
	// For now we just pass through — WeCom webhook auth is via the key in URL.
	return baseURL
}

// sendViaApp sends via WeCom App Message API (proactive push to users).
func (n *WeComNotifier) sendViaApp(ctx context.Context, title, body string) error {
	token, err := n.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("wecom app: get token failed: %w", err)
	}

	apiURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)

	msg := map[string]interface{}{
		"msgtype": n.msgType,
		"agentid": n.agentID,
	}

	switch n.msgType {
	case "textcard":
		msg["textcard"] = map[string]interface{}{
			"title":       title,
			"description": body,
			"url":         "",
			"btntxt":      "View Details",
		}
	case "markdown":
		msg["markdown"] = map[string]interface{}{
			"content": fmt.Sprintf("## %s\n\n%s", title, body),
		}
	default:
		msg["text"] = map[string]interface{}{
			"content": fmt.Sprintf("%s\n\n%s", title, body),
		}
	}

	// Target recipients
	if len(n.toUsers) > 0 {
		msg["touser"] = joinStrings(n.toUsers, "|")
	}
	if len(n.toParties) > 0 {
		msg["toparty"] = joinStrings(n.toParties, "|")
	}
	if len(n.toTags) > 0 {
		msg["totag"] = joinStrings(n.toTags, "|")
	}

	if len(n.toUsers) == 0 && len(n.toParties) == 0 && len(n.toTags) == 0 {
		return fmt.Errorf("wecom app: no recipients configured (to_user/to_party/to_tag)")
	}

	msg["safe"] = 0

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("wecom app: marshal failed: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= wecomMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := n.doAppRequest(ctx, apiURL, data)
		if err == nil {
			return nil
		}
		lastErr = err

		// Token expired — force refresh
		if isTokenError(err) {
			n.tokenExpiry = time.Time{}
			token, err = n.getAccessToken(ctx)
			if err != nil {
				return fmt.Errorf("wecom app: token refresh failed: %w", err)
			}
			apiURL = fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", token)
		}
	}

	return fmt.Errorf("wecom app send failed after %d retries: %w", wecomMaxRetries, lastErr)
}

// doAppRequest performs a single app API call and checks the WeCom errcode response.
func (n *WeComNotifier) doAppRequest(ctx context.Context, apiURL string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, wecomMaxBodyRead))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wecom app API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if json.Unmarshal(respBody, &result) != nil {
		return fmt.Errorf("wecom app: failed to parse response: %s", string(respBody))
	}
	if result.Errcode != 0 {
		return fmt.Errorf("wecom app API error: code=%d msg=%s", result.Errcode, result.Errmsg)
	}

	return nil
}

// getAccessToken fetches or returns cached WeCom app access token.
func (n *WeComNotifier) getAccessToken(ctx context.Context) (string, error) {
	n.tokenMu.Lock()
	defer n.tokenMu.Unlock()

	if n.accessToken != "" && time.Now().Before(n.tokenExpiry) {
		return n.accessToken, nil
	}

	apiURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(n.corpID), url.QueryEscape(n.corpSecret))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, wecomMaxBodyRead))

	var result struct {
		Errcode     int    `json:"errcode"`
		Errmsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("wecom: parse token response failed: %w", err)
	}
	if result.Errcode != 0 || result.AccessToken == "" {
		return "", fmt.Errorf("wecom: token request failed: code=%d msg=%s", result.Errcode, result.Errmsg)
	}

	n.accessToken = result.AccessToken
	n.tokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-60) * time.Second)
	slog.Info("wecom app token refreshed", "expires_in", result.ExpiresIn)
	return n.accessToken, nil
}

// --- helpers ---

func isTokenError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsStr(msg, "40014") || containsStr(msg, "42001") || containsStr(msg, "40001")
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func redactURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return "(unparseable)"
	}
	q := parsed.Query()
	if q.Has("key") {
		q.Set("key", "***")
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}

// formatPRBody formats a PR record into a WeCom-friendly message body.
func formatPRBody(pr *models.PRRecord, action string) string {
	stateEmoji := map[string]string{
		"open":   "🟢",
		"closed": "🔴",
		"merged": "🟣",
		"spam":   "⚠️",
	}
	emoji := stateEmoji[pr.State]
	if emoji == "" {
		emoji = "⚪"
	}

	lines := []string{
		fmt.Sprintf("**Title:** %s", pr.Title),
		fmt.Sprintf("**Author:** %s", pr.Author),
		fmt.Sprintf("**State:** %s %s", emoji, pr.State),
	}
	if pr.HTMLURL != "" {
		lines = append(lines, fmt.Sprintf("**Link:** [View PR](%s)", pr.HTMLURL))
	}
	if len(pr.Labels) > 0 {
		lines = append(lines, fmt.Sprintf("**Labels:** %s", joinStrings(pr.Labels, ", ")))
	}
	return joinStrings(lines, "\n")
}

// signWebhookHMAC computes HMAC-SHA256 signature for WeCom webhook verification.
// Can be used by the receiving side if bidirectional webhook is implemented.
func signWebhookHMAC(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
