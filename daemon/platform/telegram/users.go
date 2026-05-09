package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

func (b *Bot) handleAPIKey(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 2 {
		return c.Send("Usage:\n/apikey new <name> <role>\n/apikey list\n/apikey revoke <key_id>")
	}
	switch args[1] {
	case "new":
		if len(args) < 4 {
			return c.Send("Usage: /apikey new <name> <role>\nRole: admin, operator, viewer")
		}
		name := args[2]
		role := args[3]
		validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
		if !validRoles[role] {
			return c.Send(fmt.Sprintf("Invalid role: %s", role))
		}
		body := map[string]interface{}{"name": name, "role": role}
		return b.doAPIKeyAPI(c, "POST", "/api/v1/apikeys", body, "API key created")
	case "list":
		return b.handleAPIKeyList(c)
	case "revoke":
		if len(args) < 3 {
			return c.Send("Usage: /apikey revoke <key_id>")
		}
		return b.doAPIKeyAPI(c, "DELETE", fmt.Sprintf("/api/v1/apikeys/%s", args[2]), nil, "API key revoked")
	default:
		return c.Send("Unknown subcommand. Use: new, list, revoke")
	}
}

func (b *Bot) handleAPIKeyList(c telebot.Context) error {
	url := fmt.Sprintf("http://localhost%s/api/v1/apikeys", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var keys []map[string]interface{}
	if json.Unmarshal(body, &keys) != nil {
		return c.Send("Error parsing response")
	}
	if len(keys) == 0 {
		return c.Send("No API keys.")
	}
	var sb strings.Builder
	sb.WriteString("<b>🔑 API Keys</b>\n\n")
	for _, k := range keys {
		name, _ := k["name"].(string)
		role, _ := k["role"].(string)
		id, _ := k["id"].(string)
		sb.WriteString(fmt.Sprintf("• <b>%s</b> (%s) <code>%s</code>\n", name, role, id))
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) doAPIKeyAPI(c telebot.Context, method, path string, bodyData interface{}, successMsg string) error {
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
		return c.Send("Error parsing response")
	}
	if resp.StatusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			return c.Send("Error: " + errMsg)
		}
		return c.Send(fmt.Sprintf("Request failed (HTTP %d)", resp.StatusCode))
	}
	if method == "POST" {
		if key, ok := result["key"].(string); ok {
			err := c.Send(successMsg+"\n\n<code>"+key+"</code>\n\n⚠️ Copy it now, it won't be shown again!",
				&telebot.SendOptions{ParseMode: telebot.ModeHTML})
			go func() {
				time.Sleep(2 * time.Minute)
				b.bot.Delete(c.Message())
			}()
			return err
		}
	}
	return c.Send(successMsg)
}
