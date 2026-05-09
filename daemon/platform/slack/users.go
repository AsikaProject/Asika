package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func (b *Bot) handleAddUser(ev *slack.MessageEvent, client *socketmode.Client, parts []string) {
	if !b.isAdmin(ev.User) {
		return
	}
	if len(parts) < 4 {
		b.postMessage(client, ev.Channel, "Usage: `adduser <username> <password> <role> [group1,group2,...]`\nRole: admin, operator, viewer")
		return
	}
	username := parts[1]
	password := parts[2]
	role := parts[3]
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[role] {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Invalid role: %s. Must be admin, operator, or viewer.", role))
		return
	}
	body := map[string]interface{}{
		"username": username,
		"password": password,
		"role":     role,
	}
	if len(parts) > 4 {
		groups := strings.Split(parts[4], ",")
		for i := range groups {
			groups[i] = strings.TrimSpace(groups[i])
		}
		body["allowed_repo_groups"] = groups
	}
	b.doUserAPI(client, ev.Channel, "POST", "/api/v1/users", body, "✅ User `"+username+"` created")
}

func (b *Bot) handleDelUser(ev *slack.MessageEvent, client *socketmode.Client, parts []string) {
	if !b.isAdmin(ev.User) {
		return
	}
	if len(parts) < 2 {
		b.postMessage(client, ev.Channel, "Usage: `deluser <username>`")
		return
	}
	b.doUserAPI(client, ev.Channel, "DELETE", fmt.Sprintf("/api/v1/users/%s", parts[1]), nil, "✅ User `"+parts[1]+"` deleted")
}

func (b *Bot) handleListUsers(ev *slack.MessageEvent, client *socketmode.Client) {
	if !b.isOperator(ev.User) {
		return
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/users", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch users: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var users []map[string]interface{}
	if json.Unmarshal(body, &users) != nil {
		b.postMessage(client, ev.Channel, "Error parsing users response")
		return
	}
	if len(users) == 0 {
		b.postMessage(client, ev.Channel, "No users found.")
		return
	}
	var sb strings.Builder
	sb.WriteString("*👥 Users*\n")
	for _, u := range users {
		username, _ := u["username"].(string)
		role, _ := u["role"].(string)
		sb.WriteString(fmt.Sprintf("• `%s` (%s)\n", username, role))
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) doUserAPI(client *socketmode.Client, channel, method, path string, bodyData interface{}, successMsg string) {
	url := fmt.Sprintf("http://localhost%s%s", b.cfg.Server.Listen, path)
	var reqBody io.Reader
	if bodyData != nil {
		data, err := json.Marshal(bodyData)
		if err != nil {
			b.postMessage(client, channel, fmt.Sprintf("Error: %v", err))
			return
		}
		reqBody = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		b.postMessage(client, channel, fmt.Sprintf("Error: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	if bodyData != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, channel, fmt.Sprintf("Failed: %v", err))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		b.postMessage(client, channel, "Error parsing response")
		return
	}
	if resp.StatusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			b.postMessage(client, channel, "Error: "+errMsg)
			return
		}
		b.postMessage(client, channel, fmt.Sprintf("Request failed (HTTP %d)", resp.StatusCode))
		return
	}
	b.postMessage(client, channel, successMsg)
}
