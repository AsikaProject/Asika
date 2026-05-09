package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/telebot.v3"
)

func (b *Bot) handleAddUser(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /adduser <username> <password> <role> [group1,group2,...]\nRole: admin, operator, viewer")
	}
	username := args[1]
	password := args[2]
	role := args[3]
	if len(args) > 4 {
		// backward compat: args[3] might be role
	}
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[role] {
		return c.Send(fmt.Sprintf("Invalid role: %s. Must be admin, operator, or viewer.", role))
	}

	body := map[string]interface{}{
		"username": username,
		"password": password,
		"role":     role,
	}
	if len(args) > 4 {
		groups := strings.Split(args[4], ",")
		for i := range groups {
			groups[i] = strings.TrimSpace(groups[i])
		}
		body["allowed_repo_groups"] = groups
	}

	return b.doUserAPI(c, "POST", "/api/v1/users", body, "User created")
}

func (b *Bot) handleDelUser(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 2 {
		return c.Send("Usage: /deluser <username>")
	}
	return b.doUserAPI(c, "DELETE", fmt.Sprintf("/api/v1/users/%s", args[1]), nil, "User deleted")
}

func (b *Bot) handleListUsers(c telebot.Context) error {
	if !b.requireOperator(c) {
		return nil
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/users", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to fetch users: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var users []map[string]interface{}
	if json.Unmarshal(body, &users) != nil {
		return c.Send("Error parsing users response")
	}
	if len(users) == 0 {
		return c.Send("No users found.")
	}
	var sb strings.Builder
	sb.WriteString("<b>👥 Users</b>\n\n")
	for _, u := range users {
		username, _ := u["username"].(string)
		role, _ := u["role"].(string)
		sb.WriteString(fmt.Sprintf("• <b>%s</b> (%s)\n", username, role))
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) doUserAPI(c telebot.Context, method, path string, bodyData interface{}, successMsg string) error {
	url := fmt.Sprintf("http://localhost%s%s", b.cfg.Server.Listen, path)
	var reqBody io.Reader
	if bodyData != nil {
		data, err := json.Marshal(bodyData)
		if err != nil {
			return c.Send(fmt.Sprintf("Error: %v", err))
		}
		reqBody = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	if bodyData != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed: %v", err))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		return c.Send(fmt.Sprintf("Error parsing response"))
	}
	if resp.StatusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			return c.Send(fmt.Sprintf("Error: %s", errMsg))
		}
		return c.Send(fmt.Sprintf("Request failed (HTTP %d)", resp.StatusCode))
	}
	return c.Send(successMsg)
}
