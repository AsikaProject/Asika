package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) handleAddUser(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.requireAdmin(m.Author.ID) {
		return
	}
	if len(parts) < 4 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!adduser <username> <password> <role> [group1,group2,...]`\nRole: admin, operator, viewer")
		return
	}
	username := parts[1]
	password := parts[2]
	role := parts[3]
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[role] {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid role: %s. Must be admin, operator, or viewer.", role))
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
	b.doUserAPI(s, m, "POST", "/api/v1/users", body, "✅ User `"+username+"` created")
}

func (b *Bot) handleDelUser(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.requireAdmin(m.Author.ID) {
		return
	}
	if len(parts) < 2 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!deluser <username>`")
		return
	}
	b.doUserAPI(s, m, "DELETE", fmt.Sprintf("/api/v1/users/%s", parts[1]), nil, "✅ User `"+parts[1]+"` deleted")
}

func (b *Bot) handleListUsers(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.requireOperator(m.Author.ID) {
		return
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/users", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to fetch users: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var users []map[string]interface{}
	if json.Unmarshal(body, &users) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing users response")
		return
	}
	if len(users) == 0 {
		s.ChannelMessageSend(m.ChannelID, "No users found.")
		return
	}
	var sb strings.Builder
	sb.WriteString("**👥 Users**\n")
	for _, u := range users {
		username, _ := u["username"].(string)
		role, _ := u["role"].(string)
		sb.WriteString(fmt.Sprintf("• `%s` (%s)\n", username, role))
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) doUserAPI(s *discordgo.Session, m *discordgo.MessageCreate, method, path string, bodyData interface{}, successMsg string) {
	url := fmt.Sprintf("http://localhost%s%s", b.cfg.Server.Listen, path)
	var reqBody io.Reader
	if bodyData != nil {
		data, err := json.Marshal(bodyData)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", err))
			return
		}
		reqBody = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	if bodyData != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed: %v", err))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing response")
		return
	}
	if resp.StatusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			s.ChannelMessageSend(m.ChannelID, "Error: "+errMsg)
			return
		}
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Request failed (HTTP %d)", resp.StatusCode))
		return
	}
	s.ChannelMessageSend(m.ChannelID, successMsg)
}

func (b *Bot) handleAPIKeyCreate(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.requireAdmin(m.Author.ID) {
		return
	}
	if len(parts) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!apikey_create <name> <role>`\nRole: admin, operator, viewer")
		return
	}
	name := parts[1]
	role := parts[2]
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[role] {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Invalid role: %s", role))
		return
	}
	body := map[string]interface{}{"name": name, "role": role}
	b.doAPIKeyAPI(s, m, "POST", "/api/v1/apikeys", body, "API key created")
}

func (b *Bot) handleAPIKeyList(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.requireAdmin(m.Author.ID) {
		return
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/apikeys", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var keys []map[string]interface{}
	if json.Unmarshal(body, &keys) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing response")
		return
	}
	if len(keys) == 0 {
		s.ChannelMessageSend(m.ChannelID, "No API keys.")
		return
	}
	var sb strings.Builder
	sb.WriteString("**🔑 API Keys**\n")
	for _, k := range keys {
		name, _ := k["name"].(string)
		role, _ := k["role"].(string)
		id, _ := k["id"].(string)
		sb.WriteString(fmt.Sprintf("• `%s` (%s) `%s`\n", name, role, id))
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleAPIKeyRevoke(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.requireAdmin(m.Author.ID) {
		return
	}
	if len(parts) < 2 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!apikey_revoke <key_id>`")
		return
	}
	b.doAPIKeyAPI(s, m, "DELETE", fmt.Sprintf("/api/v1/apikeys/%s", parts[1]), nil, "✅ API key revoked")
}

func (b *Bot) doAPIKeyAPI(s *discordgo.Session, m *discordgo.MessageCreate, method, path string, bodyData interface{}, successMsg string) {
	url := fmt.Sprintf("http://localhost%s%s", b.cfg.Server.Listen, path)
	var reqBody io.Reader
	if bodyData != nil {
		data, err := json.Marshal(bodyData)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", err))
			return
		}
		reqBody = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	if bodyData != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed: %v", err))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing response")
		return
	}
	if resp.StatusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			s.ChannelMessageSend(m.ChannelID, "Error: "+errMsg)
			return
		}
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Request failed (HTTP %d)", resp.StatusCode))
		return
	}
	if method == "POST" {
		if key, ok := result["key"].(string); ok {
			s.ChannelMessageSend(m.ChannelID, successMsg+"\n\n`"+key+"`\n\n⚠️ Copy it now!")
			go func() {
				time.Sleep(2 * time.Minute)
				s.ChannelMessageDelete(m.ChannelID, m.ID)
			}()
			return
		}
	}
	s.ChannelMessageSend(m.ChannelID, successMsg)
}
