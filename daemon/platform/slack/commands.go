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
)

func (b *Bot) handleHelp(ev *slack.MessageEvent, client *socketmode.Client) {
	help := `*Asika Bot Commands*

*PR Management*
prs [repo_group] — List PRs
pr <repo_group> <number> — Show PR details
approve <repo_group> <pr_id> — Approve a PR
close <repo_group> <pr_id> [reason] — Close a PR
reopen <repo_group> <pr_id> — Reopen a PR (spam recovery)
spam <repo_group> <pr_id> — Mark PR as spam
revert <repo_group> <pr_id> — Revert a merged PR

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
