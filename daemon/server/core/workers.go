package core

import (
	"log/slog"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"

	"asika/common/models"
	"asika/common/platforms"
	"asika/common/utils"
	"asika/daemon/consumer"
	"asika/daemon/handlers"
	"asika/daemon/polling"
	"asika/daemon/queue"
	"asika/daemon/stale"
	"asika/daemon/syncer"
)

// StartWorkers starts all background workers (queue, spam, poller, consumer, stale).
func StartWorkers(
	cfg *models.Config,
	clients map[platforms.PlatformType]platforms.PlatformClient,
) (
	queueMgr *queue.Manager,
	spamDetector *syncer.SpamDetector,
	poller *polling.Poller,
	eventConsumer *consumer.Consumer,
	staleMgr *stale.Manager,
) {
	syncr := syncer.NewSyncer(cfg, clients)
	handlers.InitSyncer(syncr)

	// Merge queue
	queueMgr = queue.NewManager(cfg, clients)
	handlers.InitQueueMgr(queueMgr)
	queueMgr.Recover()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				queueMgr.CheckQueue()
			case <-queueMgr.StopChan():
				slog.Info("merge queue checker stopped")
				return
			}
		}
	}()
	slog.Info("merge queue checker started")

	// Spam detector
	spamDetector = syncer.NewSpamDetectorWithClients(cfg, clients)
	go func() {
		if !cfg.Spam.Enabled {
			return
		}
		window := utils.ParseDuration(cfg.Spam.TimeWindow, 10*time.Minute)
		ticker := time.NewTicker(window / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				spamDetector.Scan()
			case <-spamDetector.StopChan():
				slog.Info("spam detector stopped")
				return
			}
		}
	}()
	slog.Info("spam detector started", "enabled", cfg.Spam.Enabled)

	// Spam auto-clean worker
	if cfg.Spam.AutoCleanEnabled {
		autoCleanInterval := utils.ParseDuration(cfg.Spam.AutoCleanInterval, 24*time.Hour)
		go func() {
			ticker := time.NewTicker(autoCleanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					slog.Info("spam auto-clean running")
					cfg.Spam.TriggerOnTitleKw = nil
					cfg.Spam.TriggerOnAuthor = false
					persistSpamClean(cfg)
				case <-spamDetector.StopChan():
					slog.Info("spam auto-clean worker stopped")
					return
				}
			}
		}()
		slog.Info("spam auto-clean worker started", "interval", autoCleanInterval)
	}

	// Poller
	poller = polling.NewPoller(cfg, clients)
	poller.PollOnce() // Initial fetch
	if cfg.Events.Mode == "polling" {
		go poller.Start()
		slog.Info("background poller started")
	}

	// Event consumer
	eventConsumer = consumer.NewConsumerWithClients(cfg, clients)
	go eventConsumer.Start()
	slog.Info("event consumer started")

	// Webhook retry worker
	handlers.StartWebhookRetryWorker()

	// Stale PR checker
	staleMgr = stale.NewManager(cfg, clients)
	handlers.InitStaleManager(staleMgr)
	eventConsumer.SetStaleManager(staleMgr)
	startStaleCheck(cfg, staleMgr)

	return
}

	func persistSpamClean(cfg *models.Config) {
		configPath := os.Getenv("ASIKA_CONFIG")
		if configPath == "" {
			configPath = "/etc/asika_config.toml"
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			slog.Warn("spam auto-clean: failed to read config", "error", err)
			return
		}
		var existing map[string]interface{}
		if err := toml.Unmarshal(data, &existing); err != nil {
			slog.Warn("spam auto-clean: failed to parse config", "error", err)
			return
		}
		if spam, ok := existing["spam"].(map[string]interface{}); ok {
			spam["trigger_on_title_kw"] = []interface{}{}
			spam["trigger_on_author"] = false
		}
		newData, err := toml.Marshal(&existing)
		if err != nil {
			slog.Warn("spam auto-clean: failed to marshal config", "error", err)
			return
		}
		if err := os.WriteFile(configPath, newData, 0600); err != nil {
			slog.Warn("spam auto-clean: failed to write config", "error", err)
			return
		}
		slog.Info("spam auto-clean: config persisted")
	}

	func startStaleCheck(cfg *models.Config, mgr *stale.Manager) {
		if !cfg.Stale.Enabled {
			return
		}

		interval := utils.ParseDuration(cfg.Stale.CheckInterval, 6*time.Hour)
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			mgr.CheckAllGroups()
			for range ticker.C {
				mgr.CheckAllGroups()
			}
		}()
		slog.Info("stale checker started", "interval", interval)
	}
