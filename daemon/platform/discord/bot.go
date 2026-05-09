package discord

import (
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"

	"asika/common/auth"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

// Bot wraps the Discord bot with Asika management functionality.
type Bot struct {
	session       *discordgo.Session
	cfg           *models.Config
	clients       map[platforms.PlatformType]platforms.PlatformClient
	queueMgr      *queue.Manager
	syncerRef     *syncer.Syncer
	spamDetector  *syncer.SpamDetector
	notifier      *notifier.DiscordNotifier
	adminIDs      map[string]bool
	operatorIDs   map[string]bool
	viewerIDs     map[string]bool
	stop          chan struct{}
	internalToken string
}

// NewBot creates a new Discord bot.
func NewBot(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncerRef *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
	discordNotifier *notifier.DiscordNotifier,
	adminIDs []string,
	operatorIDs []string,
	viewerIDs []string,
) *Bot {
	token, _ := auth.GenerateInternalToken()
	b := &Bot{
		cfg:           cfg,
		clients:       clients,
		queueMgr:      queueMgr,
		syncerRef:     syncerRef,
		spamDetector:  spamDetector,
		notifier:      discordNotifier,
		adminIDs:      make(map[string]bool),
		operatorIDs:   make(map[string]bool),
		viewerIDs:     make(map[string]bool),
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

// SetSession sets the Discord session.
func (b *Bot) SetSession(s *discordgo.Session) {
	b.session = s
}

// Start starts the bot and registers command handlers.
func (b *Bot) Start() {
	if b.session == nil {
		slog.Warn("discord bot: no session, skipping start")
		return
	}
	slog.Info("starting discord interactive bot")
	b.session.AddHandler(b.handleMessageCreate)
	go b.session.Open()
}

// Stop stops the bot gracefully.
func (b *Bot) Stop() {
	close(b.stop)
	if b.session != nil {
		b.session.Close()
	}
	slog.Info("discord bot stopped")
}

func (b *Bot) isAdmin(userID string) bool {
	if len(b.adminIDs) == 0 && len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return true
	}
	return b.adminIDs[userID]
}

func (b *Bot) isOperator(userID string) bool {
	if b.isAdmin(userID) {
		return true
	}
	if len(b.operatorIDs) == 0 && len(b.viewerIDs) == 0 {
		return false
	}
	return b.operatorIDs[userID]
}

// getUserRole returns the role name for the user: "admin", "operator", or "viewer"
func (b *Bot) getUserRole(userID string) string {
	if b.isAdmin(userID) {
		return "admin"
	}
	if b.isOperator(userID) {
		return "operator"
	}
	return "viewer"
}

func (b *Bot) requireAdmin(userID string) bool {
	return b.isAdmin(userID)
}

func (b *Bot) requireOperator(userID string) bool {
	return b.isOperator(userID)
}

func (b *Bot) getClientForPlatform(platform string) platforms.PlatformClient {
	if b.clients == nil {
		return nil
	}
	return b.clients[platforms.PlatformType(platform)]
}

func (b *Bot) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	if !b.requireAdmin(m.Author.ID) {
		return
	}
	content := strings.TrimSpace(m.Content)
	if content == "" {
		return
	}
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])
	switch cmd {
	case "!help":
		b.handleHelp(s, m)
	case "!prs":
		b.handleListPRs(s, m, parts)
	case "!pr":
		b.handleShowPR(s, m, parts)
	case "!approve":
		b.handleApprovePR(s, m, parts)
	case "!close":
		b.handleClosePR(s, m, parts)
	case "!reopen":
		b.handleReopenPR(s, m, parts)
	case "!spam":
		b.handleMarkSpam(s, m, parts)
	case "!queue":
		b.handleShowQueue(s, m, parts)
	case "!recheck":
		b.handleRecheckQueue(s, m)
	case "!queue_clear":
		b.handleClearQueue(s, m, parts)
	case "!queue_remove":
		b.handleRemoveFromQueue(s, m, parts)
	case "!config":
		b.handleShowConfig(s, m)
	case "!rebase":
		b.handleRebasePR(s, m, parts)
	case "!cherry-pick":
		b.handleCherryPickPR(s, m, parts)
	case "!stats":
		b.handleStats(s, m)
	case "!usage":
		b.handleUsage(s, m)
	case "!adduser":
		b.handleAddUser(s, m, parts)
	case "!deluser":
		b.handleDelUser(s, m, parts)
	case "!listusers":
		b.handleListUsers(s, m)
	case "!apikey":
		b.handleAPIKey(s, m, parts)
	case "!version":
		b.handleVersion(s, m)
	default:
		if strings.HasPrefix(cmd, "!") {
			s.ChannelMessageSend(m.ChannelID, "Unknown command. Use !help for available commands.")
		}
	}
}
