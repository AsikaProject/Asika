package core

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"asika/common/config"
	"asika/common/hooks"
	"asika/common/models"
	"asika/common/platforms"
)

var (
	hooksDispatcherMu sync.RWMutex
	hooksDispatcher   *hooks.Dispatcher
)

func setHooksDispatcher(d *hooks.Dispatcher) {
	hooksDispatcherMu.Lock()
	defer hooksDispatcherMu.Unlock()
	hooksDispatcher = d
}

func reloadHooks(cfg *models.Config) {
	hooksDispatcherMu.Lock()
	defer hooksDispatcherMu.Unlock()
	if hooksDispatcher != nil {
		hooksDispatcher.Stop()
		hooksDispatcher = nil
	}
	if len(cfg.Hooks) > 0 {
		hookList := make([]hooks.Hook, len(cfg.Hooks))
		for i, h := range cfg.Hooks {
			hookList[i] = hooks.Hook{
				Events: h.Events,
				URL:    h.URL,
				Secret: h.Secret,
			}
			if h.Filter != nil {
				hookList[i].Filter = &hooks.FilterConfig{
					RepoGroups: h.Filter.RepoGroups,
					Platforms:  h.Filter.Platforms,
				}
			}
			if h.Retry != nil {
				hookList[i].Retry = &hooks.RetryConfig{
					MaxAttempts: h.Retry.MaxAttempts,
					Backoff:     h.Retry.Backoff,
				}
			}
		}
		hooksDispatcher = hooks.NewDispatcher(hookList)
		hooksDispatcher.Start()
	}
}

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
			reloadHooks(cfg)
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
	reloadHooks(loadedCfg)
	slog.Info("config reloaded after update")
}
