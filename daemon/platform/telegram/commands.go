package telegram

import (
	"fmt"
	"html"

	"gopkg.in/telebot.v3"
)

func (b *Bot) isAdmin(c telebot.Context) bool {
	if len(b.adminIDs) == 0 && len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return true
	}
	return b.adminIDs[c.Sender().ID]
}

func (b *Bot) isOperator(c telebot.Context) bool {
	if b.isAdmin(c) {
		return true
	}
	if len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return false
	}
	return b.operatorIDs[c.Sender().ID]
}

func (b *Bot) isViewer(c telebot.Context) bool {
	if b.isOperator(c) {
		return true
	}
	return b.viewerIDs[c.Sender().ID]
}

// getUserRole returns the role name for the sender: "admin", "operator", or "viewer"
func (b *Bot) getUserRole(c telebot.Context) string {
	if b.isAdmin(c) {
		return "admin"
	}
	if b.isOperator(c) {
		return "operator"
	}
	return "viewer"
}

func (b *Bot) requireAdmin(c telebot.Context) bool {
	if !b.isAdmin(c) {
		c.Send("Access denied. Admin only.")
		return false
	}
	return true
}

func (b *Bot) requireOperator(c telebot.Context) bool {
	if !b.isOperator(c) {
		c.Send("Access denied. Operator or Admin only.")
		return false
	}
	return true
}

func (b *Bot) handleStart(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	userID := c.Sender().ID
	username := c.Sender().Username
	msg := fmt.Sprintf(
		"<b>Welcome to Asika Bot</b>\n\nHello @%s (ID: %d)\n\nUse /help to see available commands.\n\nYou have admin privileges.",
		html.EscapeString(username), userID,
	)
	return c.Send(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleHelp(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	help := `<b>Asika Bot Commands</b>

📋 <b>PR Management</b>
/prs repo_group — List PRs
/pr repo_group number — Show PR details
/approve repo_group pr_id — Approve a PR
/close repo_group pr_id — Close a PR
/reopen repo_group pr_id — Reopen a PR (spam recovery)
/revert repo_group pr_number — Revert a merged PR
/spam repo_group pr_id — Mark PR as spam

📊 <b>Queue</b>
/queue repo_group — Show merge queue
/recheck repo_group — Trigger queue recheck

⚙️ <b>Config</b>
/config — Show current config (masked)

🧹 <b>Stale PRs</b>
/stale repo_group — Show stale PRs
/unstale repo_group pr_number — Remove stale label

🔄 <b>Rebase</b>
/rebase repo_group pr_number — Rebase a PR onto its base branch

🍒 <b>Cherry-pick</b>
/cherrypick repo_group pr_number target_branch — Cherry-pick a merged PR

📈 <b>Stats</b>
/stats — Show DORA metrics

ℹ️ <b>Info</b>
/version — Show version info`
	return c.Send(help, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}
