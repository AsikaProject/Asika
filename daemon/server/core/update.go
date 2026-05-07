package core

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"asika/common/models"
	"asika/common/utils"
	"asika/common/version"
)

var updateHTTPClient = &http.Client{Timeout: 30 * time.Second}

func startUpdateCheck(cfg *models.Config) {
	if !cfg.Updates.Check {
		return
	}

	interval := utils.ParseDuration(cfg.Updates.Interval, 24*time.Hour)
	go func() {
		ticker := time.NewTicker(interval)
		for range ticker.C {
			checkAndNotify(cfg)
		}
	}()
	slog.Info("update checker started", "interval", interval)
}

func checkAndNotify(cfg *models.Config) {
	type releaseResponse struct {
		TagName string `json:"tag_name"`
	}

	resp, err := updateHTTPClient.Get("https://api.github.com/repos/AsikaProject/asika/releases/latest")
	if err != nil {
		slog.Warn("update check failed", "error", err)
		return
	}
	defer resp.Body.Close()

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("update check: failed to decode response", "error", err)
		return
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := version.Version

	if latestVersion == "" || latestVersion == currentVersion {
		return
	}

	slog.Info("new version available", "current", currentVersion, "latest", latestVersion)

	if cfg.Updates.NotifyOnNew {
		title := "Asika Update Available"
		body := "A new version of Asika (" + latestVersion + ") is available.\nRun `asika self-update` to upgrade."
		SendNotificationSync(title, body)
	}
}


