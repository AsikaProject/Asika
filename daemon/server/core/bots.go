package core

import (
	"log/slog"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"gopkg.in/telebot.v3"

	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
	"asika/daemon/handlers"
	"asika/daemon/platform/discord"
	"asika/daemon/platform/feishu"
	"asika/daemon/platform/slack"
	"asika/daemon/platform/telegram"
	"asika/daemon/queue"
	"asika/daemon/syncer"
)

// StartTelegram starts the Telegram interactive bot if configured.
// Returns the bot instance (or nil) so it can be stopped on shutdown.
func StartTelegram(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncr *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
) *telegram.Bot {
	if cfg == nil || !cfg.Telegram.Enabled || cfg.Telegram.Token == "" {
		return nil
	}

	pref := telebot.Settings{
		Token:  cfg.Telegram.Token,
		Poller: &telebot.LongPoller{Timeout: 10},
	}

	bot, err := telebot.NewBot(pref)
	if err != nil {
		slog.Error("failed to create telegram bot", "error", err)
		return nil
	}

	cfgMap := map[string]interface{}{
		"token": cfg.Telegram.Token,
		"to":    toStringList(cfg.Telegram.ChatIDs),
	}
	telegramNotifier := notifier.NewTelegramNotifier(cfgMap)

	tgBot := telegram.NewBot(
		bot, cfg, clients, queueMgr, syncr, spamDetector,
		telegramNotifier, cfg.Telegram.AdminIDs, cfg.Telegram.OperatorIDs, cfg.Telegram.ViewerIDs,
	)

	go tgBot.Start()
	slog.Info("telegram bot started", "admin_ids", len(cfg.Telegram.AdminIDs))
	return tgBot
}

// StartFeishu starts the Feishu interactive bot if configured.
// Returns the bot instance (or nil) so it can be stopped on shutdown.
func StartFeishu(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncr *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
) *feishu.Bot {
	if cfg == nil || !cfg.Feishu.Enabled || cfg.Feishu.AppID == "" {
		return nil
	}

	cfgMap := map[string]interface{}{
		"webhook_url": cfg.Feishu.WebhookURL,
		"app_id":      cfg.Feishu.AppID,
		"app_secret":  cfg.Feishu.AppSecret,
	}
	feishuNotifier := notifier.NewFeishuNotifier(cfgMap)

	fsBot := feishu.NewBot(
		cfg, clients, queueMgr, syncr, spamDetector, feishuNotifier,
	)

	handlers.InitFeishuBot(fsBot)

	go fsBot.Start()
	slog.Info("feishu bot started", "app_id", cfg.Feishu.AppID)
	return fsBot
}

// StartDiscord starts the Discord interactive bot if configured.
func StartDiscord(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncr *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
) *discord.Bot {
	if cfg == nil || !cfg.Discord.Enabled || cfg.Discord.Token == "" {
		return nil
	}

	cfgMap := map[string]interface{}{
		"token":       cfg.Discord.Token,
		"channel_ids": toStringList(cfg.Discord.AdminIDs),
	}
	discordNotifier := notifier.NewDiscordNotifier(cfgMap)

	discordBot := discord.NewBot(
		cfg, clients, queueMgr, syncr, spamDetector,
		discordNotifier, cfg.Discord.AdminIDs, cfg.Discord.OperatorIDs, cfg.Discord.ViewerIDs,
	)

	if discordNotifier.Session() == nil {
		slog.Warn("discord bot: failed to create session")
		return nil
	}

	discordBot.SetSession(discordNotifier.Session())
	go discordBot.Start()
	slog.Info("discord bot started", "admin_ids", len(cfg.Discord.AdminIDs))
	return discordBot
}

// StartSlack starts the Slack interactive bot if configured.
// Uses Socket Mode for bidirectional communication (no public URL needed).
func StartSlack(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
	queueMgr *queue.Manager,
	syncr *syncer.Syncer,
	spamDetector *syncer.SpamDetector,
) *slack.Bot {
	if cfg == nil || !cfg.Slack.Enabled || cfg.Slack.Token == "" || cfg.Slack.AppToken == "" {
		return nil
	}

	slackClient := slackapi.New(cfg.Slack.Token, slackapi.OptionAppLevelToken(cfg.Slack.AppToken))
	socketClient := socketmode.New(slackClient)

	cfgMap := map[string]interface{}{
		"token":      cfg.Slack.Token,
		"channel_id": "",
	}
	slackNotifier := notifier.NewSlackBotNotifier(cfgMap)

	slackBot := slack.NewBot(cfg, clients, queueMgr, syncr, spamDetector, slackNotifier, cfg.Slack.AdminIDs, cfg.Slack.OperatorIDs, cfg.Slack.ViewerIDs)
	slackBot.SetSocketClient(socketClient)

	go func() {
		for evt := range socketClient.Events {
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				slog.Info("slack bot connecting...")
			case socketmode.EventTypeConnected:
				slog.Info("slack bot connected")
			case socketmode.EventTypeDisconnect:
				slog.Warn("slack bot disconnected")
			default:
				slackBot.HandleEvent(evt, socketClient)
			}
		}
	}()

	slog.Info("slack bot started", "admin_ids", len(cfg.Slack.AdminIDs))
	return slackBot
}
