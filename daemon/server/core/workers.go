package core

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"

	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
	"asika/common/utils"
	"asika/daemon/consumer"
	"asika/daemon/handlers"
	"asika/daemon/handlers/pr"
	"asika/daemon/handlers/webhook"
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
	eventConsumer.Start()
	slog.Info("event consumer started")

	// Webhook retry worker
	webhook.StartWebhookRetryWorker()

	// Stale PR checker
	staleMgr = stale.NewManager(cfg, clients)
	handlers.InitStaleManager(staleMgr)
	eventConsumer.SetStaleManager(staleMgr)
	startStaleCheck(cfg, staleMgr)

	// Webhook health checker
	startWebhookHealthChecker(cfg, poller)

	// Serial validation worker
	serialWorker := queue.NewSerialWorker(cfg, clients)
	serialWorker.Start()
	pr.InitSerialWorker(serialWorker)

	// Escalation worker
	escalationWorker := NewEscalationWorker()
	escalationWorker.Start()

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

func startWebhookHealthChecker(cfg *models.Config, poller *polling.Poller) {
	healthCheckInterval := utils.ParseDuration(cfg.Events.HealthCheckInterval, 2*time.Minute)
	threshold := utils.ParseDuration(cfg.Events.HealthCheckThreshold, 5*time.Minute)

	go func() {
		ticker := time.NewTicker(healthCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			checkWebhookHealth(cfg, poller, threshold)
		}
	}()
	slog.Info("webhook health checker started", "interval", healthCheckInterval, "threshold", threshold)
}

func checkWebhookHealth(cfg *models.Config, poller *polling.Poller, threshold time.Duration) {
	healthData, err := db.ListWebhookHealth()
	if err != nil {
		slog.Warn("webhook health check: failed to read health data", "error", err)
		return
	}

	for _, rg := range cfg.RepoGroups {
		platforms := webhookPlatforms(rg)
		if len(platforms) == 0 {
			continue
		}

		unhealthyCount := 0
		for _, plat := range platforms {
			key := fmt.Sprintf("%s:%s", rg.Name, plat)
			lastSeen, exists := healthData[key]
			if !exists || time.Since(lastSeen) > threshold {
				unhealthyCount++
			}
		}

		shouldForce := unhealthyCount > 0
		wasForce := poller.IsForcePoll(rg.Name)
		if shouldForce != wasForce {
			poller.SetForcePoll(rg.Name, shouldForce)
			if shouldForce {
				slog.Warn("webhook unhealthy, enabling forced polling", "repo_group", rg.Name, "unhealthy_platforms", unhealthyCount)
			} else {
				slog.Info("webhook recovered, disabling forced polling", "repo_group", rg.Name)
			}
		}
	}
}

func webhookPlatforms(rg models.RepoGroupConfig) []string {
	p := make([]string, 0)
	if rg.GitHub != "" {
		p = append(p, "github")
	}
	if rg.GitLab != "" {
		p = append(p, "gitlab")
	}
	if rg.Gitea != "" {
		p = append(p, "gitea")
	}
	if rg.Forgejo != "" {
		p = append(p, "forgejo")
	}
	if rg.Codeberg != "" {
		p = append(p, "codeberg")
	}
	if rg.Bitbucket != "" {
		p = append(p, "bitbucket")
	}
	if rg.Gerrit != "" {
		p = append(p, "gerrit")
	}
	return p
}
