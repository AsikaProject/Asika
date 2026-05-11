package telegram

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

func (b *Bot) handleShowQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
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
		if repoGroup == "" || item.RepoGroup == repoGroup || strings.HasPrefix(string(key), repoGroup+"#") {
			items = append(items, item)
		}
		return nil
	})
	if len(items) == 0 {
		return c.Send(fmt.Sprintf("Queue empty for <b>%s</b>.", html.EscapeString(repoGroup)),
			&telebot.SendOptions{ParseMode: telebot.ModeHTML})
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Merge Queue — %s</b>\n\n", html.EscapeString(repoGroup)))
	for _, item := range items {
		statusEmoji := "⏳"
		switch item.Status {
		case "done":
			statusEmoji = "✅"
		case "failed":
			statusEmoji = "❌"
		case "merging":
			statusEmoji = "🔄"
		}
		sb.WriteString(fmt.Sprintf("%s %s (%s) — %s\n", statusEmoji, item.PRID, item.Status, item.AddedAt.Format(time.RFC3339)))
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleClearQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
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
		return c.Send("No repo group configured.")
	}
	if b.queueMgr == nil {
		return c.Send("Queue manager not initialized.")
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to clear queue: %v", err))
	}
	return c.Send(fmt.Sprintf("Queue cleared for <b>%s</b>. %d items removed.", html.EscapeString(repoGroup), count),
		&telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleRemoveFromQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /queue_remove <repo_group> <pr_id>")
	}
	if b.queueMgr == nil {
		return c.Send("Queue manager not initialized.")
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		return c.Send(fmt.Sprintf("Failed to remove: %v", err))
	}
	return c.Send(fmt.Sprintf("Removed <b>%s</b> from queue.", html.EscapeString(args[2])),
		&telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleRecheckQueue(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	if b.queueMgr == nil {
		return c.Send("Queue manager not initialized.")
	}
	go b.queueMgr.CheckQueue()
	return c.Send("Queue recheck triggered.")
}
