package slack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
		b.postMessage(client, ev.Channel, fmt.Sprintf("PR #%d not found in %s.", prNumber, repoGroup))
		return
	}
	var desc string
	if found.Body != "" {
		lines := strings.Split(found.Body, "\n")
		if len(lines) > 5 {
			desc = strings.Join(lines[:5], "\n") + "\n..."
		} else {
			desc = found.Body
		}
	}
	var events string
	if len(found.Events) > 0 {
		var sb strings.Builder
		for _, ev := range found.Events {
			sb.WriteString(fmt.Sprintf("  • %s by %s at %s\n", ev.Action, ev.Actor, ev.Timestamp.Format("01-02 15:04")))
		}
		events = sb.String()
	}
	text := fmt.Sprintf("*PR #%d — %s*\nState: %s\nAuthor: %s\nPlatform: %s\nLabels: %s",
		found.PRNumber, found.Title, found.State, found.Author, found.Platform,
		strings.Join(found.Labels, ", "))
	if found.MergeCommitSHA != "" {
		text += fmt.Sprintf("\nMerge Commit: `%s`", found.MergeCommitSHA[:8])
	}
	if found.HTMLURL != "" {
		text += fmt.Sprintf("\nURL: %s", found.HTMLURL)
	}
	if desc != "" {
		text += "\n\n*Description:*\n" + desc
	}
	if events != "" {
		text += "\n\n*Events:*\n" + events
	}
	switch found.State {
	case "open":
		text += "\n\nActions: `approve` / `close [reason]` / `spam` / `rebase`"
	case "closed", "spam":
		text += "\n\nActions: `reopen`"
	case "merged":
		text += "\n\nActions: `revert` / `cherry-pick <target_branch>`"
	}
	b.postMessage(client, ev.Channel, text)
}
