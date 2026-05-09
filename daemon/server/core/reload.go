package core

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
)

// SetupConfigReload sets up SIGHUP signal handler for hot config reload.
func SetupConfigReload() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	go func() {
		for range sigChan {
			slog.Info("received SIGHUP, reloading config")
			cfg, err := config.Load(config.ConfigPath)
			if err != nil {
				slog.Error("failed to reload config", "error", err)
				continue
			}
			config.Store(cfg)
			slog.Info("config reloaded successfully")
		}
	}()
}

// ReloadConfigAfterUpdate should be called after config file is written.
// It reloads config from disk and re-initializes notifiers with clients.
func ReloadConfigAfterUpdate(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) {
	loadedCfg, err := config.Load(config.ConfigPath)
	if err != nil {
		slog.Error("failed to reload config after update", "error", err)
		return
	}
	config.Store(loadedCfg)
	InitNotifiers(loadedCfg, clients)
	slog.Info("config reloaded after update")
}
