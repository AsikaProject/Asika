package feishu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (b *Bot) handleAddUser(senderID string, parts []string) string {
	if !b.isAdmin(senderID) {
		return "Access denied. Admin only."
	}
	if len(parts) < 4 {
		return "Usage: adduser <username> <password> <role> [group1,group2,...]\nRole: admin, operator, viewer"
	}
	username := parts[1]
	password := parts[2]
	role := parts[3]
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[role] {
		return fmt.Sprintf("Invalid role: %s. Must be admin, operator, or viewer.", role)
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
	return b.doUserAPI("POST", "/api/v1/users", body, "User created")
}

func (b *Bot) handleDelUser(senderID string, parts []string) string {
	if !b.isAdmin(senderID) {
		return "Access denied. Admin only."
	}
	if len(parts) < 2 {
		return "Usage: deluser <username>"
	}
	return b.doUserAPI("DELETE", fmt.Sprintf("/api/v1/users/%s", parts[1]), nil, "User deleted")
}

func (b *Bot) handleListUsers(senderID string) string {
	if !b.isOperator(senderID) {
		return "Access denied. Operator or Admin only."
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/users", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch users: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var users []map[string]interface{}
	if json.Unmarshal(body, &users) != nil {
		return "Error parsing users response"
	}
	if len(users) == 0 {
		return "No users found."
	}
	var lines []string
	lines = append(lines, "Users:")
	for _, u := range users {
		username, _ := u["username"].(string)
		role, _ := u["role"].(string)
		lines = append(lines, fmt.Sprintf("  %s (%s)", username, role))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) doUserAPI(method, path string, bodyData interface{}, successMsg string) string {
	url := fmt.Sprintf("http://localhost%s%s", b.cfg.Server.Listen, path)
	var reqBody io.Reader
	if bodyData != nil {
		data, err := json.Marshal(bodyData)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		reqBody = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	if bodyData != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		return "Error parsing response"
	}
	if resp.StatusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			return "Error: " + errMsg
		}
		return fmt.Sprintf("Request failed (HTTP %d)", resp.StatusCode)
	}
	return successMsg
}
