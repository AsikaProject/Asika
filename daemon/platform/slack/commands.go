package slack

import (
	"context"
	"encoding/json"
	"fmt"
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
	b.postMessage(client, ev.Channel, "Stats via Slack bot is not yet implemented. Use the API or WebUI.")
}

func (b *Bot) handleVersion(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, fmt.Sprintf("*Asika*\nVersion: `%s`", version.Version))
}
