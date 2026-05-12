package discord

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	commonutil "asika/common/platformutil"
)

func (b *Bot) handleHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	help := `**Asika Bot Commands**

**PR Management**
!prs [repo_group] — List PRs
!pr <repo_group> <number> — Show PR details
!approve <repo_group> <pr_id> — Approve a PR
!close <repo_group> <pr_id> — Close a PR
!reopen <repo_group> <pr_id> — Reopen a PR (spam recovery)
!spam <repo_group> <pr_id> — Mark PR as spam
!revert <repo_group> <pr_id> — Revert a merged PR

**Queue**
!queue [repo_group] — Show merge queue
!recheck [repo_group] — Trigger queue recheck

**Config**
!config — Show current config (masked)

**Rebase / Cherry-pick**
!rebase repo_group pr_number — Rebase a PR onto its base branch
!cherry-pick repo_group pr_number target_branch — Cherry-pick a merged PR

**Info**
!version — Show version info`
	s.ChannelMessageSend(m.ChannelID, help)
}

func (b *Bot) handleListPRs(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) == 0 {
			s.ChannelMessageSend(m.ChannelID, "No repo groups configured.")
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
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("No PRs found for repo group **%s**.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**PRs in %s**\n\n", repoGroup))
	for _, pr := range prs {
		statusEmoji := "🔵"
		switch pr.State {
		case "merged":
			statusEmoji = "🟣"
		case "closed":
			statusEmoji = "🔴"
		case "spam":
			statusEmoji = "⚠️"
		}
		sb.WriteString(fmt.Sprintf("%s **#%d** %s — by %s (%s/%s)\n",
			statusEmoji, pr.PRNumber, commonutil.Truncate(pr.Title, 40), pr.Author, pr.Platform, pr.State))
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleShowPR(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: `!pr <repo_group> <pr_number>`")
		return
	}
	repoGroup := args[1]
	prNumber, err := strconv.Atoi(args[2])
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "Invalid PR number.")
		return
	}
	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && pr.PRNumber == prNumber {
			found = &pr
		}
		return nil
	})
	if found == nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("PR #%d not found in repo group **%s**.", prNumber, repoGroup))
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
            sb.WriteString(fmt.Sprintf("  • %s by %s at %s\n", ev.Action, ev.Actor, ev.Timestamp.Format("2006-01-02 15:04")))
        }
        events = sb.String()
    }
    msg := fmt.Sprintf(
        "**PR #%d** — %s\n\n  Author: %s\n  State: %s\n  Platform: %s\n  Repo Group: %s\n  Labels: %s\n  Created: %s\n",
        found.PRNumber, found.Title, found.Author, found.State, found.Platform,
        found.RepoGroup, strings.Join(found.Labels, ", "), found.CreatedAt.Format(time.RFC3339),
    )
    if desc != "" {
        msg += "\n**Description:**\n" + desc + "\n"
    }
    if events != "" {
        msg += "\n**Events:**\n" + events
    }
	switch found.State {
	case "open":
		msg += "\nAvailable actions: `!approve` / `!close [reason]` / `!spam`"
	case "closed", "spam":
		msg += "\nAvailable actions: `!reopen`"
	case "merged":
		msg += "\nAvailable actions: `!revert`"
	}
	s.ChannelMessageSend(m.ChannelID, msg)
}
