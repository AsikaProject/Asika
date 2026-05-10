package core

import (
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/consumer"
	"asika/daemon/handlers"
	"asika/daemon/platform/discord"
	"asika/daemon/platform/feishu"
	"asika/daemon/platform/slack"
	"asika/daemon/platform/telegram"
	"asika/daemon/polling"
	"asika/daemon/queue"
	"asika/daemon/reports"
	"asika/daemon/server"
	"asika/daemon/syncer"
)

// InitConfig holds all initialized subsystems for orderly shutdown.
type InitConfig struct {
	Cfg           *models.Config
	Clients       map[platforms.PlatformType]platforms.PlatformClient
	Server        *server.Server
	QueueMgr      *queue.Manager
	SpamDetector  *syncer.SpamDetector
	Poller        *polling.Poller
	EventConsumer *consumer.Consumer
	TgBot         *telegram.Bot
	FsBot         *feishu.Bot
	DiscordBot    *discord.Bot
	SlackBot      *slack.Bot
}

// InitWithRetry initializes the database with retries for lock conflicts.
func InitWithRetry(cfg *models.Config, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := db.Init(cfg.Database); err == nil {
			return nil
		} else if i < maxRetries-1 {
			slog.Warn("db init failed, retrying", "attempt", i+1, "max", maxRetries, "error", err)
			time.Sleep(2 * time.Second)
		}
	}
	return db.Init(cfg.Database)
}

// Bootstrap initializes all daemon subsystems.
// Returns InitConfig for orderly shutdown.
func Bootstrap(cfg *models.Config) (*InitConfig, error) {
	if err := InitWithRetry(cfg, 5); err != nil {
		return nil, err
	}
	slog.Info("database initialized", "path", cfg.Database.Path)

	procs := runtime.NumCPU()
	if cfg.Server.MaxProcs > 0 {
		procs = cfg.Server.MaxProcs
	}
	if cfg.Server.MinProcs > 0 && procs < cfg.Server.MinProcs {
		procs = cfg.Server.MinProcs
	}
	runtime.GOMAXPROCS(procs)
	slog.Info("GOMAXPROCS set", "procs", procs, "min_procs", cfg.Server.MinProcs, "max_procs", cfg.Server.MaxProcs, "num_cpu", runtime.NumCPU())

	if err := db.RunMigrations(); err != nil {
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	auth.Init(cfg.Auth.JWTSecret, config.GenerateTokenExpiry(cfg.Auth.TokenExpiry))

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	if cfg.Tokens.GitHub != "" {
		clients[platforms.PlatformGitHub] = platforms.NewGitHubClient(cfg.Tokens.GitHub, cfg.Events.WebhookSecret, cfg.GitHubBaseURL)
	}
	if cfg.Tokens.GitLab != "" {
		clients[platforms.PlatformGitLab] = platforms.NewGitLabClient(cfg.Tokens.GitLab, cfg.GitLabBaseURL, cfg.Events.WebhookSecret)
	}
	if cfg.Tokens.Gitea != "" {
		giteaURL := cfg.GiteaBaseURL
		if giteaURL == "" {
			giteaURL = "https://gitea.example.com"
		}
		if gc := platforms.NewGiteaClient(giteaURL, cfg.Tokens.Gitea, cfg.Events.WebhookSecret); gc != nil {
			clients[platforms.PlatformGitea] = gc
		}
	}
	if cfg.Tokens.Forgejo != "" {
		forgejoURL := cfg.ForgejoBaseURL
		if forgejoURL == "" {
			forgejoURL = cfg.GiteaBaseURL
		}
		if forgejoURL == "" {
			forgejoURL = "https://forgejo.example.com"
		}
		if gc := platforms.NewForgejoClient(forgejoURL, cfg.Tokens.Forgejo, cfg.Events.WebhookSecret); gc != nil {
			clients[platforms.PlatformForgejo] = gc
		}
	}
	if cfg.Tokens.Codeberg != "" {
		codebergURL := cfg.GiteaBaseURL
		if codebergURL == "" {
			codebergURL = "https://codeberg.org"
		}
		if gc := platforms.NewForgejoClient(codebergURL, cfg.Tokens.Codeberg, cfg.Events.WebhookSecret); gc != nil {
			clients[platforms.PlatformCodeberg] = gc
		}
	}
	if cfg.Tokens.Bitbucket != "" {
		clients[platforms.PlatformBitbucket] = platforms.NewBitbucketClient(cfg.Tokens.Bitbucket, cfg.Events.WebhookSecret)
	}
	if cfg.Tokens.Gerrit.URL != "" && cfg.Tokens.Gerrit.Username != "" && cfg.Tokens.Gerrit.Password != "" {
		clients[platforms.PlatformGerrit] = platforms.NewGerritClient(
			cfg.Tokens.Gerrit.URL, cfg.Tokens.Gerrit.Username, cfg.Tokens.Gerrit.Password, cfg.Events.WebhookSecret,
		)
	}

	events.Init()

	if err := platforms.CheckMergeMethods(cfg, clients); err != nil {
		platforms.ExitOnCheckFailed(err)
	}

	ic := &InitConfig{
		Cfg:     cfg,
		Clients: clients,
	}

	MigrateRepoGroupNames(cfg)
	MigratePRStates(cfg)
	SyncPRStates(cfg, clients)

	ic.QueueMgr, ic.SpamDetector, ic.Poller, ic.EventConsumer, _ = StartWorkers(cfg, clients)
	handlers.OnWorkerPoolConfigReload(func(cfg models.WorkerPoolConfig) {
		if ic.EventConsumer != nil {
			ic.EventConsumer.UpdateWorkerPoolConfig(cfg)
		}
	})
	handlers.OnProcsReload(func(minProcs, maxProcs int) {
		procs := runtime.NumCPU()
		if maxProcs > 0 {
			procs = maxProcs
		}
		if minProcs > 0 && procs < minProcs {
			procs = minProcs
		}
		runtime.GOMAXPROCS(procs)
		slog.Info("GOMAXPROCS updated", "procs", procs, "min_procs", minProcs, "max_procs", maxProcs)
	})
	handlers.InitPoller(ic.Poller)

	InitNotifiers(cfg, clients)
	handlers.SetNotifyUrgentFunc(SendNotificationUrgentSync)

	SetupConfigReload()

	ic.TgBot = StartTelegram(cfg, clients, ic.QueueMgr, nil, ic.SpamDetector)
	ic.FsBot = StartFeishu(cfg, clients, ic.QueueMgr, nil, ic.SpamDetector)
	ic.DiscordBot = StartDiscord(cfg, clients, ic.QueueMgr, nil, ic.SpamDetector)
	ic.SlackBot = StartSlack(cfg, clients, ic.QueueMgr, nil, ic.SpamDetector)

	startUpdateCheck(cfg)

	if cfg.Reports.Enabled {
		reportScheduler := reports.NewScheduler(cfg.Reports)
		reportScheduler.Start()
		slog.Info("scheduled reports enabled", "cron", cfg.Reports.Cron)
	}

	srv := server.NewServer(cfg, clients)
	ic.Server = srv

	return ic, nil
}

// BootstrapLegacy is the original Bootstrap for callers that don't need InitConfig.
// Kept for backward compatibility.
func BootstrapLegacy(cfg *models.Config) (*server.Server, error) {
	ic, err := Bootstrap(cfg)
	if err != nil {
		return nil, err
	}
	return ic.Server, nil
}
