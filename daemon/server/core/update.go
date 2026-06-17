package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"asika/common/models"
	"asika/common/utils"
	"asika/common/version"
)

const githubLatestReleaseAPI = "https://api.github.com/repos/AsikaProject/asika/releases/latest"

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
	// Dev builds have no stable ordering and would otherwise spam upgrade
	// notifications on every poll. Match the Web UI behaviour and skip.
	if version.IsDevBuild(version.Version) {
		return
	}

	type releaseResponse struct {
		TagName string `json:"tag_name"`
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, githubLatestReleaseAPI, nil)
	if err != nil {
		slog.Warn("update check: build request", "error", err)
		return
	}
	// Authenticate when a token is configured — anonymous calls are heavily
	// rate-limited and frequently fail with 403/429.
	if cfg.Tokens.GitHub != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Tokens.GitHub)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		slog.Warn("update check failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("update check: non-200 response", "status", resp.StatusCode)
		return
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("update check: failed to decode response", "error", err)
		return
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := version.Version

	if !version.IsUpgradeable(currentVersion, latestVersion) {
		return
	}

	slog.Info("new version available", "current", currentVersion, "latest", latestVersion)

	if cfg.Updates.NotifyOnNew {
		title := "Asika Update Available"
		body := "A new version of Asika (" + latestVersion + ") is available.\nRun `asika self-update` to upgrade."
		SendNotificationSync(title, body)
	}
}
