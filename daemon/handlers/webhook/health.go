package webhook

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

type webhookHealthEntry struct {
	RepoGroup string    `json:"repo_group"`
	Platform  string    `json:"platform"`
	LastSeen  time.Time `json:"last_seen"`
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message"`
}

type webhookHealthResponse struct {
	Entries  []webhookHealthEntry `json:"entries"`
	Overall  string               `json:"overall"`
	Interval string               `json:"check_interval"`
}

func WebhookHealthHandler(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	healthData, err := db.ListWebhookHealth()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read webhook health data"})
		return
	}

	threshold := cfg.Events.HealthCheckThreshold
	if threshold == "" {
		threshold = "5m"
	}
	thresholdDuration := parseHealthThreshold(threshold, cfg.Events.PollingInterval)

	entries := make([]webhookHealthEntry, 0)
	allHealthy := true

	for _, rg := range cfg.RepoGroups {
		platforms := collectPlatforms(rg)
		for _, plat := range platforms {
			key := fmt.Sprintf("%s:%s", rg.Name, plat)
			lastSeen, exists := healthData[key]
			healthy := false
			msg := "No webhook received yet"
			if exists {
				elapsed := time.Since(lastSeen)
				if elapsed <= thresholdDuration {
					healthy = true
					msg = fmt.Sprintf("Last webhook received %s ago", roundDuration(elapsed))
				} else {
					msg = fmt.Sprintf("No webhook received for %s (threshold: %s)", roundDuration(elapsed), roundDuration(thresholdDuration))
				}
			}
			if !healthy {
				allHealthy = false
			}
			entries = append(entries, webhookHealthEntry{
				RepoGroup: rg.Name,
				Platform:  plat,
				LastSeen:  lastSeen,
				Healthy:   healthy,
				Message:   msg,
			})
		}
	}

	overall := "healthy"
	if !allHealthy {
		overall = "degraded"
	}

	c.JSON(http.StatusOK, webhookHealthResponse{
		Entries:  entries,
		Overall:  overall,
		Interval: thresholdDuration.String(),
	})
}

func collectPlatforms(rg models.RepoGroupConfig) []string {
	platforms := make([]string, 0)
	if rg.GitHub != "" {
		platforms = append(platforms, "github")
	}
	if rg.GitLab != "" {
		platforms = append(platforms, "gitlab")
	}
	if rg.Gitea != "" {
		platforms = append(platforms, "gitea")
	}
	if rg.Forgejo != "" {
		platforms = append(platforms, "forgejo")
	}
	if rg.Codeberg != "" {
		platforms = append(platforms, "codeberg")
	}
	if rg.Bitbucket != "" {
		platforms = append(platforms, "bitbucket")
	}
	if rg.Gerrit != "" {
		platforms = append(platforms, "gerrit")
	}
	return platforms
}

func parseHealthThreshold(threshold, fallbackPollingInterval string) time.Duration {
	if d, err := time.ParseDuration(threshold); err == nil && d > 0 {
		return d
	}
	pollingInterval := parseHealthThresholdDuration(fallbackPollingInterval, 30*time.Second)
	return pollingInterval * 2
}

func parseHealthThresholdDuration(s string, defaultVal time.Duration) time.Duration {
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return defaultVal
}

func roundDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
