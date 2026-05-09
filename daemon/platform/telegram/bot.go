package telegram

import (
	"log/slog"

	"gopkg.in/telebot.v3"

	"asika/common/auth"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

// Bot wraps the Telegram bot with Asika management functionality.
type Bot struct {
	bot           *telebot.Bot
	cfg           *models.Config
	clients       map[platforms.PlatformType]platforms.PlatformClient
	queueMgr      *queue.Manager
	syncerRef     *syncer.Syncer
	spamDetector  *syncer.SpamDetector
	notifier      *notifier.TelegramNotifier
	adminIDs      map[int64]bool
	operatorIDs   map[int64]bool
	viewerIDs     map[int64]bool
	internalToken string
	stop          chan struct{}
}

// NewBot creates a new Telegram bot with interactive decision support.
func NewBot(
	bot *telebot.Bot,
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	telegramNotifier *notifier.TelegramNotifier,
	adminIDs []int64,
	operatorIDs []int64,
	viewerIDs []int64,
) *Bot {
	token, _ := auth.GenerateInternalToken()
	b := &Bot{
		bot:           bot,
		cfg:           cfg,
		clients:       clients,
		queueMgr:      queueMgr,
		syncerRef:     syncerRef,
		spamDetector:  spamDetector,
		notifier:      telegramNotifier,
		adminIDs:      make(map[int64]bool),
		operatorIDs:   make(map[int64]bool),
		viewerIDs:     make(map[int64]bool),
		stop:          make(chan struct{}),
		internalToken: token,
	}
	for _, id := range adminIDs {
		b.adminIDs[id] = true
	}
	for _, id := range operatorIDs {
		b.operatorIDs[id] = true
	}
	for _, id := range viewerIDs {
		b.viewerIDs[id] = true
	}
	return b
}

// Start starts the bot polling and registers command handlers.
func (b *Bot) Start() {
	if b.bot == nil {
		slog.Warn("telegram bot: no bot instance, skipping start")
		return
	}
	slog.Info("starting telegram interactive bot")
	b.registerCommands()
	b.registerBotMenu()
	go b.bot.Start()
}

// Stop stops the bot gracefully.
func (b *Bot) Stop() {
	close(b.stop)
	if b.bot != nil {
		b.bot.Stop()
	}
	slog.Info("telegram bot stopped")
}

func (b *Bot) registerCommands() {
	b.bot.Handle("/start", b.handleStart)
	b.bot.Handle("/help", b.handleHelp)
	b.bot.Handle("/prs", b.handleListPRs)
	b.bot.Handle("/pr", b.handleShowPR)
	b.bot.Handle("/approve", b.handleApprovePR)
	b.bot.Handle("/close", b.handleClosePR)
	b.bot.Handle("/reopen", b.handleReopenPR)
	b.bot.Handle("/spam", b.handleMarkSpam)
	b.bot.Handle("/queue", b.handleShowQueue)
	b.bot.Handle("/recheck", b.handleRecheckQueue)
	b.bot.Handle("/queue_clear", b.handleClearQueue)
	b.bot.Handle("/queue_remove", b.handleRemoveFromQueue)
	b.bot.Handle("/config", b.handleShowConfig)
	b.bot.Handle("/stalecheck", b.handleStaleCheck)
	b.bot.Handle("/unstale", b.handleUnstale)
	b.bot.Handle("/rebase", b.handleRebasePR)
	b.bot.Handle("/cherrypick", b.handleCherryPickPR)
	b.bot.Handle("/stats", b.handleStats)
	b.bot.Handle("/usage", b.handleUsage)
	b.bot.Handle("/adduser", b.handleAddUser)
	b.bot.Handle("/deluser", b.handleDelUser)
	b.bot.Handle("/listusers", b.handleListUsers)
	b.bot.Handle("/version", b.handleVersion)
	b.bot.Handle(telebot.OnCallback, b.handleCallback)
	b.bot.Handle(telebot.OnText, b.handleText)
}

func (b *Bot) registerBotMenu() {
	commands := []telebot.Command{
		{Text: "start", Description: "Welcome & admin info"},
		{Text: "help", Description: "Show all commands"},
		{Text: "prs", Description: "List PRs in a group"},
		{Text: "pr", Description: "Show PR details & actions"},
		{Text: "approve", Description: "Approve a PR"},
		{Text: "close", Description: "Close a PR"},
		{Text: "reopen", Description: "Reopen a PR"},
		{Text: "spam", Description: "Mark PR as spam"},
		{Text: "queue", Description: "Show merge queue"},
		{Text: "recheck", Description: "Trigger queue recheck"},
		{Text: "config", Description: "Show current config"},
		{Text: "stalecheck", Description: "Check for stale PRs"},
		{Text: "unstale", Description: "Remove stale label"},
		{Text: "rebase", Description: "Rebase a PR"},
		{Text: "cherrypick", Description: "Cherry-pick a PR"},
		{Text: "stats", Description: "Show DORA metrics"},
		{Text: "usage", Description: "Show CPU & memory usage"},
		{Text: "adduser", Description: "Add a new user (admin)"},
		{Text: "deluser", Description: "Delete a user (admin)"},
		{Text: "listusers", Description: "List all users"},
		{Text: "version", Description: "Show version info"},
	}
	if err := b.bot.SetCommands(commands); err != nil {
		slog.Warn("telegram bot: failed to set command menu", "error", err)
	}
}
