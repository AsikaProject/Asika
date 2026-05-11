package feishu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"asika/common/config"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) showConfigText() string {
	cfg := config.Current()
	if cfg == nil {
		return "Config not loaded."
	}
	groups := config.GetRepoGroups(cfg)
	return fmt.Sprintf(
		"Asika Config:\n  Server: %s (%s)\n  CPU Threads: min=%d max=%d\n  DB: %s\n  Events: %s\n  Spam: %v\n  Repo Groups: %d\n  Notify Channels: %d",
		cfg.Server.Listen, cfg.Server.Mode, cfg.Server.MinProcs, cfg.Server.MaxProcs, cfg.Database.Path,
		cfg.Events.Mode, cfg.Spam.Enabled, len(groups), len(cfg.Notify),
	)
}

func (b *Bot) showUsageText() string {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch usage: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Error parsing usage response"
	}
	var lines []string
	lines = append(lines, "System Usage", "─────────────")
	if v, ok := result["cpu_percent"]; ok {
		lines = append(lines, fmt.Sprintf("CPU: %.1f%%", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		lines = append(lines, fmt.Sprintf("Cores: %v", v))
	}
	if v, ok := result["goroutines"]; ok {
		lines = append(lines, fmt.Sprintf("Goroutines: %v", v))
	}
	if v, ok := result["pid"]; ok {
		lines = append(lines, fmt.Sprintf("PID: %v", v))
	}
	lines = append(lines, "", "Memory", "─────────────")
	if v, ok := result["mem_alloc_mb"]; ok {
		lines = append(lines, fmt.Sprintf("Alloc: %s", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		lines = append(lines, fmt.Sprintf("Total: %s", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		lines = append(lines, fmt.Sprintf("Sys: %s", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			lines = append(lines, fmt.Sprintf("GOMEMLIMIT: %s", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				lines = append(lines, fmt.Sprintf("Usage: %.1f%%", utils.ToFloat64(pct)))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) showStatsText() string {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("Failed to fetch stats: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "Error parsing stats response"
	}
	var lines []string
	lines = append(lines, "DORA Metrics", "─────────────")
	if v, ok := result["deployment_frequency"]; ok {
		lines = append(lines, fmt.Sprintf("Deployments/Day: %.2f", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		lines = append(lines, fmt.Sprintf("Lead Time: %s", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		lines = append(lines, fmt.Sprintf("Failure Rate: %.1f%%", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		lines = append(lines, fmt.Sprintf("MTTR: %s", utils.FormatHours(utils.ToFloat64(v))))
	}
	lines = append(lines, "", "Overview", "─────────────")
	if v, ok := result["total_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Total PRs: %v", v))
	}
	if v, ok := result["open_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Open: %v", v))
	}
	if v, ok := result["merged_prs"]; ok {
		lines = append(lines, fmt.Sprintf("Merged: %v", v))
	}
	if v, ok := result["queue_items"]; ok {
		lines = append(lines, fmt.Sprintf("Queue: %v", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		lines = append(lines, "", "By Repo Group", "─────────────")
		for k, v := range byGroup {
			lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		lines = append(lines, "", "By Platform", "─────────────")
		for k, v := range byPlat {
			lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
		}
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) showVersionText() string {
	return fmt.Sprintf("Asika\nVersion: %s", version.Version)
}
