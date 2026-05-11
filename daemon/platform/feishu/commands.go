package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
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
			return "Usage: close <repo_group> <pr_id> [reason]"
		}
		return b.doClose(senderID, parts[1], parts[2], parts[3:])
	case strings.HasPrefix(lower, "reopen ") || strings.HasPrefix(lower, "/reopen "):
		if len(parts) < 3 {
			return "Usage: reopen <repo_group> <pr_id>"
		}
		return b.doReopen(senderID, parts[1], parts[2])
	case strings.HasPrefix(lower, "revert ") || strings.HasPrefix(lower, "/revert "):
		if len(parts) < 3 {
			return "Usage: revert <repo_group> <pr_id>"
		}
		return b.doRevert(senderID, parts[1], parts[2])
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
   revert <group> <id>   - Revert a merged PR
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
