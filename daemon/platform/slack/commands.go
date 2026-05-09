package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) handleHelp(ev *slack.MessageEvent, client *socketmode.Client) {
	help := `*Asika Bot Commands*

*PR Management*
prs [repo_group] — List PRs
pr <repo_group> <number> — Show PR details
approve <repo_group> <pr_id> — Approve a PR
close <repo_group> <pr_id> — Close a PR
reopen <repo_group> <pr_id> — Reopen a PR (spam recovery)
spam <repo_group> <pr_id> — Mark PR as spam

*Queue*
queue [repo_group] — Show merge queue
recheck [repo_group] — Trigger queue recheck

*Config*
config — Show current config (masked)

*Rebase / Cherry-pick*
rebase repo_group pr_number — Rebase a PR onto its base branch
cherry-pick repo_group pr_number target_branch — Cherry-pick a merged PR

*Info*
version — Show version info`
	b.postMessage(client, ev.Channel, help)
}

func (b *Bot) handleListPRs(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			b.postMessage(client, ev.Channel, "No repo groups configured.")
			return
		}
		repoGroup = groups[0].Name
	}
	var prs []models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup || repoGroup == "" {
			prs = append(prs, pr)
		}
		return nil
	})
	if len(prs) == 0 {
		b.postMessage(client, ev.Channel, fmt.Sprintf("No PRs found for repo group *%s*.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*PRs in %s*\n\n", repoGroup))
	for _, pr := range prs {
		stateEmoji := map[string]string{"merged": "🟣", "closed": "🔴", "spam": "⚠️"}
		emoji := "🔵"
		if e, ok := stateEmoji[pr.State]; ok {
			emoji = e
		}
		sb.WriteString(fmt.Sprintf("%s *#%d* %s — by %s (%s/%s)\n",
			emoji, pr.PRNumber, utils.TruncateString(pr.Title, 40), pr.Author, pr.Platform, pr.State))
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleShowPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: pr <repo_group> <number>")
		return
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Invalid PR number: %s", args[2]))
		return
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Repo group *%s* not found.", repoGroup))
		return
	}
	platform := config.GetPlatformForGroup(group)
	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Cannot resolve repo for platform: %s", platform))
		return
	}
	pClient := b.getClientForPlatform(platform)
	if pClient == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Platform client not available: %s", platform))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pr, err := pClient.GetPR(ctx, owner, repo, prNumber)
	if err != nil || pr == nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d not found in %s.", prNumber, repoGroup))
		return
	}
	text := fmt.Sprintf("*PR #%d — %s*\nState: %s\nAuthor: %s\nPlatform: %s\nURL: %s",
		pr.PRNumber, pr.Title, pr.State, pr.Author, pr.Platform, pr.HTMLURL)
	b.postMessage(client, ev.Channel, text)
}

func (b *Bot) handleApprovePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: approve <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Approve via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleClosePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: close <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Close via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleReopenPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: reopen <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Reopen via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleMarkSpam(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: spam <repo_group> <pr_id>")
		return
	}
	b.postMessage(client, ev.Channel, "Spam marking via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleShowQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	}
	items, err := b.queueMgr.GetQueueItems(repoGroup)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Error fetching queue: %v", err))
		return
	}
	if len(items) == 0 {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Queue is empty for repo group *%s*.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*Merge Queue for %s*\n\n", repoGroup))
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. %s [%s]\n", i+1, item.PRID, item.Status))
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleRecheckQueue(ev *slack.MessageEvent, client *socketmode.Client) {
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	go b.queueMgr.CheckQueue()
	b.postMessage(client, ev.Channel, "Queue recheck triggered.")
}

func (b *Bot) handleClearQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) > 0 {
			repoGroup = groups[0].Name
		}
	}
	if repoGroup == "" {
		b.postMessage(client, ev.Channel, "No repo group configured.")
		return
	}
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to clear queue: %v", err))
		return
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("Queue cleared for *%s*. %d items removed.", repoGroup, count))
}

func (b *Bot) handleRemoveFromQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: queue_remove <repo_group> <pr_id>")
		return
	}
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to remove: %v", err))
		return
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("Removed *%s* from queue.", args[2]))
}

func (b *Bot) handleShowConfig(ev *slack.MessageEvent, client *socketmode.Client) {
	cfg := b.cfg
	text := fmt.Sprintf("*Asika Config*\nListen: %s\nMode: %s\nRepo Groups: %d",
		cfg.Server.Listen, cfg.Server.Mode, len(cfg.RepoGroups))
	b.postMessage(client, ev.Channel, text)
}

func (b *Bot) handleRebasePR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: rebase <repo_group> <pr_number>")
		return
	}
	b.postMessage(client, ev.Channel, "Rebase via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleCherryPickPR(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 4 {
		b.postMessage(client, ev.Channel, "Usage: cherry-pick <repo_group> <pr_number> <target_branch>")
		return
	}
	b.postMessage(client, ev.Channel, "Cherry-pick via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleStats(ev *slack.MessageEvent, client *socketmode.Client) {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch stats: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		b.postMessage(client, ev.Channel, "Error parsing stats response")
		return
	}
	var sb strings.Builder
	sb.WriteString("*📊 DORA Metrics*\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: *%.2f*\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: *%s*\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: *%.1f%%*\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: *%s*\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n*Overview*\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: *%v*\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: *%v*\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: *%v*\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: *%v*\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n*By Repo Group*\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: *%v*\n", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n*By Platform*\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: *%v*\n", k, v))
		}
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleUsage(ev *slack.MessageEvent, client *socketmode.Client) {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch usage: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		b.postMessage(client, ev.Channel, "Error parsing usage response")
		return
	}
	var sb strings.Builder
	sb.WriteString("*💻 System Usage*\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: *%.1f%%*\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: *%v*\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: *%v*\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: *%v*\n", v))
	}
	sb.WriteString("\n*Memory*\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: *%s*\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: *%.1f%%*\n", utils.ToFloat64(pct)))
			}
		}
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) handleVersion(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, fmt.Sprintf("*Asika*\nVersion: `%s`", version.Version))
}

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
