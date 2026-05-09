package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) processCommand(senderID, text string) string {
	if !b.isOperator(senderID) {
		return "Access denied. Operator or Admin only."
	}
	lower := strings.ToLower(text)
	parts := strings.Fields(text)
	switch {
	case lower == "help" || lower == "/help":
		return b.helpText()
	case lower == "prs" || lower == "/prs":
		return b.listPRsText("")
	case strings.HasPrefix(lower, "prs ") || strings.HasPrefix(lower, "/prs "):
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		return b.listPRsText(groupName)
	case strings.HasPrefix(lower, "pr ") || strings.HasPrefix(lower, "/pr "):
		if len(parts) < 3 {
			return "Usage: pr <repo_group> <pr_number>"
		}
		return b.showPRText(parts[1], parts[2])
	case strings.HasPrefix(lower, "approve ") || strings.HasPrefix(lower, "/approve "):
		if len(parts) < 3 {
			return "Usage: approve <repo_group> <pr_id>"
		}
		return b.doApprove(senderID, parts[1], parts[2])
	case strings.HasPrefix(lower, "close ") || strings.HasPrefix(lower, "/close "):
		if len(parts) < 3 {
			return "Usage: close <repo_group> <pr_id>"
		}
		return b.doClose(senderID, parts[1], parts[2])
	case strings.HasPrefix(lower, "reopen ") || strings.HasPrefix(lower, "/reopen "):
		if len(parts) < 3 {
			return "Usage: reopen <repo_group> <pr_id>"
		}
		return b.doReopen(senderID, parts[1], parts[2])
	case strings.HasPrefix(lower, "spam ") || strings.HasPrefix(lower, "/spam "):
		if len(parts) < 3 {
			return "Usage: spam <repo_group> <pr_id>"
		}
		return b.doMarkSpam(senderID, parts[1], parts[2])
	case lower == "queue" || lower == "/queue":
		return b.showQueueText("")
	case strings.HasPrefix(lower, "queue ") || strings.HasPrefix(lower, "/queue "):
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		return b.showQueueText(groupName)
	case lower == "recheck" || lower == "/recheck":
		if b.queueMgr != nil {
			go b.queueMgr.CheckQueue()
			return "Queue recheck triggered."
		}
		return "Queue manager not initialized."
	case lower == "queue_clear" || lower == "/queue_clear":
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		if groupName == "" {
			groups := config.GetRepoGroups(b.cfg)
			if len(groups) > 0 {
				groupName = groups[0].Name
			}
		}
		if groupName == "" {
			return "No repo group configured."
		}
		if b.queueMgr == nil {
			return "Queue manager not initialized."
		}
		count, err := b.queueMgr.ClearQueue(groupName)
		if err != nil {
			return fmt.Sprintf("Failed to clear queue: %v", err)
		}
		return fmt.Sprintf("Queue cleared for %s. %d items removed.", groupName, count)
	case strings.HasPrefix(lower, "queue_remove ") || strings.HasPrefix(lower, "/queue_remove "):
		if len(parts) < 3 {
			return "Usage: queue_remove <repo_group> <pr_id>"
		}
		if b.queueMgr == nil {
			return "Queue manager not initialized."
		}
		if err := b.queueMgr.RemoveFromQueue(parts[1], parts[2]); err != nil {
			return fmt.Sprintf("Failed to remove: %v", err)
		}
		return fmt.Sprintf("Removed %s from queue.", parts[2])
	case lower == "config" || lower == "/config":
		return b.showConfigText()
	case strings.HasPrefix(lower, "stalecheck") || strings.HasPrefix(lower, "/stalecheck") || strings.HasPrefix(lower, "stale-check") || strings.HasPrefix(lower, "/stale-check"):
		groupName := ""
		if len(parts) > 1 {
			groupName = parts[1]
		}
		return b.doStaleCheckText(groupName)
	case strings.HasPrefix(lower, "unstale ") || strings.HasPrefix(lower, "/unstale "):
		if len(parts) < 3 {
			return "Usage: unstale <repo_group> <pr_number>"
		}
		return b.doUnstale(senderID, parts[1], parts[2])
	case strings.HasPrefix(lower, "rebase ") || strings.HasPrefix(lower, "/rebase "):
		if len(parts) < 3 {
			return "Usage: rebase <repo_group> <pr_number>"
		}
		return b.doRebase(senderID, parts[1], parts[2])
	case lower == "stats" || lower == "/stats":
		return b.showStatsText()
	case lower == "usage" || lower == "/usage":
		return b.showUsageText()
	case lower == "adduser" || lower == "/adduser":
		return b.handleAddUser(senderID, parts)
	case lower == "deluser" || lower == "/deluser":
		return b.handleDelUser(senderID, parts)
	case lower == "listusers" || lower == "/listusers":
		return b.handleListUsers(senderID)
	case strings.HasPrefix(lower, "apikey ") || strings.HasPrefix(lower, "/apikey "):
		return b.handleAPIKey(senderID, parts)
	case lower == "apikey" || lower == "/apikey":
		return "Usage:\napikey new <name> <role>\napikey list\napikey revoke <key_id>"
	case lower == "version" || lower == "/version":
		return b.showVersionText()
	case strings.HasPrefix(lower, "cherry-pick ") || strings.HasPrefix(lower, "/cherry-pick "):
		if len(parts) < 4 {
			return "Usage: cherry-pick <repo_group> <pr_number> <target_branch>"
		}
		return b.doCherryPick(senderID, parts[1], parts[2], parts[3])
	default:
		return fmt.Sprintf("Unknown command: %s\nTry 'help' for available commands.", text)
	}
}

func (b *Bot) helpText() string {
	return `Asika Feishu Bot Commands:
  help          - Show this help
  prs [group]   - List PRs
  pr <group> <num> - Show PR details
  approve <group> <id> - Approve PR
  close <group> <id>   - Close PR
  reopen <group> <id>  - Reopen PR
  spam <group> <id>    - Mark as spam
  queue [group] - Show merge queue
  recheck       - Trigger queue recheck
  config        - Show config summary
  stalecheck [group] - Check for stale PRs
  unstale <group> <id> - Remove stale label
  rebase <group> <num> - Rebase a PR
  cherry-pick <group> <num> <branch> - Cherry-pick a merged PR
  stats         - Show DORA metrics
  usage         - Show CPU & memory usage
  adduser <user> <pass> <role> [groups] - Add user (admin)
  deluser <username> - Delete user (admin)
  listusers     - List all users
  apikey new <name> <role> - Create API key (admin)
  apikey list   - List API keys (admin)
  apikey revoke <key_id> - Revoke API key (admin)
  version       - Show version info`
}

func (b *Bot) listPRsText(repoGroup string) string {
	if repoGroup == "" {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			return "No repo groups configured."
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
		return fmt.Sprintf("No PRs in %s", repoGroup)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("PRs in %s:\n", repoGroup))
	for _, pr := range prs {
		emoji := "O"
		switch pr.State {
		case "merged":
			emoji = "M"
		case "closed":
			emoji = "X"
		case "spam":
			emoji = "!"
		}
		sb.WriteString(fmt.Sprintf("  %s #%d %s - %s (%s)\n",
			emoji, pr.PRNumber, commonutil.Truncate(pr.Title, 35), pr.Author, pr.State))
	}
	return sb.String()
}

func (b *Bot) showPRText(repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return fmt.Sprintf("PR %s not found in %s", prID, repoGroup)
	}
	return fmt.Sprintf(
		"PR #%d - %s\n  Author: %s | State: %s\n  Platform: %s | Spam: %v\n  Labels: %s",
		pr.PRNumber, pr.Title, pr.Author, pr.State,
		pr.Platform, pr.SpamFlag, strings.Join(pr.Labels, ", "),
	)
}

func (b *Bot) doApprove(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if err := client.ApprovePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR approve failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	pr.IsApproved = true
	pr.Events = append(pr.Events, models.PREvent{Timestamp: time.Now(), Action: "approved", Actor: senderID})
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	addedToQueue := false
	if b.queueMgr != nil {
		if pr.State != "" && pr.State != "open" {
			slog.Info("feishu bot: skipping queue add for non-open PR", "pr_number", pr.PRNumber, "state", pr.State)
		} else {
			if err := b.queueMgr.AddToQueue(pr); err != nil {
				slog.Warn("feishu bot: failed to add PR to queue", "error", err, "pr_number", pr.PRNumber)
			} else {
				addedToQueue = true
				go b.queueMgr.CheckQueue()
			}
		}
	}
	db.AppendAuditLog("info", "PR approved", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "added_to_queue": addedToQueue,
	})
	if addedToQueue {
		return fmt.Sprintf("PR #%d approved and added to merge queue.", pr.PRNumber)
	}
	return fmt.Sprintf("PR #%d approved.", pr.PRNumber)
}

func (b *Bot) doClose(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR close failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	pr.State = "closed"
	prData, _ := json.Marshal(pr)
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	db.PutPRWithIndex(key, prData, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR closed", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu",
	})
	return fmt.Sprintf("PR #%d closed.", pr.PRNumber)
}

func (b *Bot) doReopen(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group == nil {
		return "Repo group not found."
	}
	client := b.getClient(pr.Platform)
	if client == nil {
		return "No client for platform."
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if err := client.ReopenPR(context.Background(), owner, repo, pr.PRNumber); err != nil {
		db.AppendAuditLog("error", "PR reopen failed", map[string]interface{}{
			"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
		})
		return fmt.Sprintf("Failed: %v", err)
	}
	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber), data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("info", "PR reopened", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu",
	})
	return fmt.Sprintf("PR #%d reopened.", pr.PRNumber)
}

func (b *Bot) doMarkSpam(senderID, repoGroup, prID string) string {
	pr, _ := commonutil.GetPRByID(repoGroup, prID)
	if pr == nil {
		return "PR not found."
	}
	pr.SpamFlag = true
	pr.State = "spam"
	pr.UpdatedAt = time.Now()
	key := fmt.Sprintf("%s#%s#%d", pr.RepoGroup, pr.Platform, pr.PRNumber)
	data, _ := json.Marshal(pr)
	db.PutPRWithIndex(key, data, pr.ID, pr.RepoGroup, pr.PRNumber)
	db.AppendAuditLog("warn", "PR marked as spam", map[string]interface{}{
		"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu",
	})
	group := config.GetRepoGroupByName(b.cfg, repoGroup)
	if group != nil {
		client := b.getClient(pr.Platform)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
			if err := client.ClosePR(context.Background(), owner, repo, pr.PRNumber); err != nil {
				db.AppendAuditLog("error", "PR spam close failed", map[string]interface{}{
					"pr_number": pr.PRNumber, "repo_group": pr.RepoGroup, "platform": pr.Platform, "actor": "feishu", "error": err.Error(),
				})
			}
		}
	}
	if b.notifier != nil {
		title := fmt.Sprintf("[Spam Alert] PR #%d", pr.PRNumber)
		body := fmt.Sprintf("PR #%d \"%s\" by %s marked as spam via Feishu.", pr.PRNumber, pr.Title, pr.Author)
		b.notifier.Send(context.Background(), title, body)
	}
	return fmt.Sprintf("PR #%d marked as spam.", pr.PRNumber)
}

func (b *Bot) showQueueText(repoGroup string) string {
	if repoGroup == "" {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) > 0 {
			repoGroup = groups[0].Name
		}
	}
	var items []models.QueueItem
	db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if strings.HasPrefix(string(key), repoGroup+"#") {
			items = append(items, item)
		}
		return nil
	})
	if len(items) == 0 {
		return fmt.Sprintf("Queue empty for %s", repoGroup)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Queue - %s:\n", repoGroup))
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("  %s (%s)\n", item.PRID, item.Status))
	}
	return sb.String()
}

func (b *Bot) showConfigText() string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	groups := config.GetRepoGroups(cfg)
	return fmt.Sprintf(
		"Asika Config:\n  Server: %s (%s)\n  DB: %s\n  Events: %s\n  Spam: %v\n  Repo Groups: %d\n  Notify Channels: %d",
		cfg.Server.Listen, cfg.Server.Mode, cfg.Database.Path,
		cfg.Events.Mode, cfg.Spam.Enabled, len(groups), len(cfg.Notify),
	)
}

func (b *Bot) doStaleCheckText(repoGroup string) string {
	cfg := config.Current()
	if cfg == nil || !cfg.Stale.Enabled {
		return "Stale PR management is not enabled in config."
	}
	if repoGroup == "" {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			return "No repo groups configured."
		}
		repoGroup = groups[0].Name
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}
	var lines []string
	lines = append(lines, "Stale PR Check for "+repoGroup+":")
	for _, pt := range platforms.GroupPlatforms(group) {
		client, ok := b.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		prs, err := client.ListPRs(ctx, owner, repo, "open")
		cancel()
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: error listing PRs", pt))
			continue
		}
		for _, pr := range prs {
			if cfg.Stale.SkipDraftPRs && pr.IsDraft {
				continue
			}
			isExempt := false
			for _, exempt := range cfg.Stale.ExemptLabels {
				if commonutil.HasLabelStr(pr.Labels, exempt, "") {
					isExempt = true
					break
				}
			}
			if isExempt {
				continue
			}
			days := commonutil.InactivityDays(pr.UpdatedAt)
			hasStale := commonutil.HasLabelStr(pr.Labels, cfg.Stale.StaleLabel, "stale")
			if hasStale && cfg.Stale.DaysUntilClose > 0 && days >= cfg.Stale.DaysUntilStale+cfg.Stale.DaysUntilClose {
				lines = append(lines, fmt.Sprintf("- [CLOSE] #%d %s", pr.PRNumber, commonutil.Truncate(pr.Title, 40)))
			} else if !hasStale && days >= cfg.Stale.DaysUntilStale {
				lines = append(lines, fmt.Sprintf("- [MARK] #%d %s", pr.PRNumber, commonutil.Truncate(pr.Title, 40)))
			}
		}
	}
	if len(lines) == 1 {
		return "No stale PRs found in " + repoGroup
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) doUnstale(senderID, repoGroup, prNumberStr string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}
	label := cfg.Stale.StaleLabel
	if label == "" {
		label = "stale"
	}
	removed := false
	for _, pt := range platforms.GroupPlatforms(group) {
		client, ok := b.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := client.RemoveLabel(ctx, owner, repo, commonutil.ParseInt(prNumberStr), label)
		cancel()
		if err != nil {
			continue
		}
		removed = true
	}
	if !removed {
		return "Failed to remove stale label."
	}
	return "Stale label removed from PR #" + prNumberStr + " in " + repoGroup
}

func (b *Bot) doRebase(senderID, repoGroup, prNumberStr string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}
	prNumber := commonutil.ParseInt(prNumberStr)
	if prNumber == 0 {
		return "Invalid PR number: " + prNumberStr
	}
	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && pr.PRNumber == prNumber {
			found = &pr
		}
		return nil
	})
	if found == nil {
		return fmt.Sprintf("PR #%d not found in %s", prNumber, repoGroup)
	}
	if found.State != "open" {
		return fmt.Sprintf("PR #%d is not open (state: %s)", prNumber, found.State)
	}
	platform := found.Platform
	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}
	client, ok := b.clients[platforms.PlatformType(platform)]
	if !ok {
		return "Platform client not available: " + platform
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return "Cannot resolve repo for platform: " + platform
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	branchInfo, err := client.GetPRBranchInfo(ctx, owner, repo, prNumber)
	if err != nil {
		return fmt.Sprintf("Failed to get branch info: %v", err)
	}
	if !branchInfo.MaintainerCanModify {
		return "Rebase not allowed: PR author has not enabled 'allow edits from maintainers'. Please ask the author to enable it on the PR page."
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/rebase", cfg.Server.Listen, repoGroup, prNumber)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Rebase request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Rebase request submitted (async)."
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		return msg
	}
	if errMsg, ok := result["error"].(string); ok {
		return "Rebase failed: " + errMsg
	}
	return "Rebase request submitted."
}

func (b *Bot) doCherryPick(senderID, repoGroup, prNumberStr, targetBranch string) string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return "Repo group not found: " + repoGroup
	}
	prNumber := commonutil.ParseInt(prNumberStr)
	if prNumber == 0 {
		return "Invalid PR number: " + prNumberStr
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/repos/%s/prs/%d/cherry-pick", cfg.Server.Listen, repoGroup, prNumber)
	body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Cherry-pick request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) != nil {
		return "Cherry-pick request submitted (async)."
	}
	if success, ok := result["success"].(bool); ok && success {
		msg, _ := result["message"].(string)
		return msg
	}
	if errMsg, ok := result["error"].(string); ok {
		return "Cherry-pick failed: " + errMsg
	}
	return "Cherry-pick request submitted."
}

func (b *Bot) showUsageText() string {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch usage: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Error parsing usage response"
	}
	var lines []string
	lines = append(lines, "System Usage", "─────────────")
	if v, ok := result["cpu_percent"]; ok {
		lines = append(lines, fmt.Sprintf("CPU: %.1f%%", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		lines = append(lines, fmt.Sprintf("Cores: %v", v))
	}
	if v, ok := result["goroutines"]; ok {
		lines = append(lines, fmt.Sprintf("Goroutines: %v", v))
	}
	if v, ok := result["pid"]; ok {
		lines = append(lines, fmt.Sprintf("PID: %v", v))
	}
	lines = append(lines, "", "Memory", "─────────────")
	if v, ok := result["mem_alloc_mb"]; ok {
		lines = append(lines, fmt.Sprintf("Alloc: %s", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		lines = append(lines, fmt.Sprintf("Total: %s", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		lines = append(lines, fmt.Sprintf("Sys: %s", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			lines = append(lines, fmt.Sprintf("GOMEMLIMIT: %s", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				lines = append(lines, fmt.Sprintf("Usage: %.1f%%", utils.ToFloat64(pct)))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) showStatsText() string {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch stats: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Error parsing stats response"
	}
	var lines []string
	lines = append(lines, "DORA Metrics", "─────────────")
	if v, ok := result["deployment_frequency"]; ok {
		lines = append(lines, fmt.Sprintf("Deployments/Day: %.2f", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		lines = append(lines, fmt.Sprintf("Lead Time: %s", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		lines = append(lines, fmt.Sprintf("Failure Rate: %.1f%%", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		lines = append(lines, fmt.Sprintf("MTTR: %s", utils.FormatHours(utils.ToFloat64(v))))
	}
	lines = append(lines, "", "Overview", "─────────────")
	if v, ok := result["total_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Total PRs: %v", v))
	}
	if v, ok := result["open_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Open: %v", v))
	}
	if v, ok := result["merged_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Merged: %v", v))
	}
	if v, ok := result["queue_items"]; ok {
		lines = append(lines, fmt.Sprintf("Queue: %v", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		lines = append(lines, "", "By Repo Group", "─────────────")
		for k, v := range byGroup {
			lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		lines = append(lines, "", "By Platform", "─────────────")
		for k, v := range byPlat {
			lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
		}
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) showVersionText() string {
	return fmt.Sprintf("Asika\nVersion: %s", version.Version)
}
