package config

import (
	"log/slog"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches the config file for changes and triggers hot-reload
type Watcher struct {
	watcher  *fsnotify.Watcher
	onReload func()
}

// NewWatcher creates a new config file watcher
func NewWatcher(onReload func()) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		watcher:  w,
		onReload: onReload,
	}, nil
}

// Start starts watching the config file
func (w *Watcher) Start(configPath string) error {
	dir := filepath.Dir(configPath)
	if err := w.watcher.Add(dir); err != nil {
		return err
	}

	go w.watch(configPath)
	return nil
}

// watch monitors the config file for changes
func (w *Watcher) watch(configPath string) {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				if filepath.Clean(event.Name) == filepath.Clean(configPath) {
					slog.Info("config file changed, reloading")
					cfg, err := Load(configPath)
					if err != nil {
						slog.Error("failed to reload config", "error", err)
						return
					}
					Store(cfg)
					slog.Info("config reloaded successfully")
					if w.onReload != nil {
						w.onReload()
					}
				}
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("watcher error", "error", err)
		}
	}
}

// Close closes the watcher
func (w *Watcher) Close() error {
	return w.watcher.Close()
}
