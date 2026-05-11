package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
	commonutil "asika/common/platformutil"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) handleShowConfig(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	cfg := config.Current()
	if cfg == nil {
		return c.Send("Config not loaded.")
	}
	groups := config.GetRepoGroups(cfg)
	var sb strings.Builder
	sb.WriteString("<b>Current Config</b>\n\n")
	sb.WriteString(fmt.Sprintf("  Server: %s (%s)\n", cfg.Server.Listen, cfg.Server.Mode))
	sb.WriteString(fmt.Sprintf("  CPU Threads: min=%d max=%d\n", cfg.Server.MinProcs, cfg.Server.MaxProcs))
	sb.WriteString(fmt.Sprintf("  DB: %s\n", cfg.Database.Path))
	sb.WriteString(fmt.Sprintf("  Events: %s\n", cfg.Events.Mode))
	sb.WriteString(fmt.Sprintf("  Spam: enabled=%v\n", cfg.Spam.Enabled))
	sb.WriteString(fmt.Sprintf("  Notify channels: %d\n", len(cfg.Notify)))
	sb.WriteString(fmt.Sprintf("  Label rules: %d\n", len(cfg.LabelRules)))
	sb.WriteString(fmt.Sprintf("  Repo groups: %d\n", len(groups)))
	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("    - %s (%s)\n", g.Name, g.Mode))
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleStaleCheck(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	}
	dryRun := len(args) > 2 && args[2] == "--dry-run"
	cfg := config.Current()
	if cfg == nil || !cfg.Stale.Enabled {
		return c.Send("Stale PR management is not enabled in config.")
	}
	var groups []models.RepoGroup
	if repoGroup != "" {
		g := config.GetRepoGroupByName(cfg, repoGroup)
		if g == nil {
			return c.Send("Repo group not found: " + repoGroup)
		}
		groups = []models.RepoGroup{*g}
	} else {
		groups = config.GetRepoGroups(cfg)
	}
	var lines []string
	if dryRun {
		lines = append(lines, "<b>Stale PR Dry Run:</b>")
	} else {
		lines = append(lines, "<b>Stale PR Check Results:</b>")
	}
	for _, group := range groups {
		prs, err := b.fetchOpenPRs(&group)
		if err != nil {
			lines = append(lines, fmt.Sprintf("- %s: error listing PRs", group.Name))
			continue
		}
		for _, pr := range prs {
			days := commonutil.InactivityDays(pr.UpdatedAt)
			hasStale := commonutil.HasLabelStr(pr.Labels, cfg.Stale.StaleLabel, "stale")
			isExempt := false
			for _, exempt := range cfg.Stale.ExemptLabels {
				if commonutil.HasLabelStr(pr.Labels, exempt, "") {
					isExempt = true
					break
				}
			}
			if cfg.Stale.SkipDraftPRs && pr.IsDraft {
				continue
			}
			if isExempt {
				continue
			}
			if hasStale && cfg.Stale.DaysUntilClose > 0 && days >= cfg.Stale.DaysUntilStale+cfg.Stale.DaysUntilClose {
				lines = append(lines, fmt.Sprintf("- [CLOSE] #%d %s (%s, %dd stale)",
					pr.PRNumber, html.EscapeString(commonutil.Truncate(pr.Title, 40)), group.Name, days))
			} else if !hasStale && days >= cfg.Stale.DaysUntilStale {
				lines = append(lines, fmt.Sprintf("- [MARK] #%d %s (%s, %dd inactive)",
					pr.PRNumber, html.EscapeString(commonutil.Truncate(pr.Title, 40)), group.Name, days))
			}
		}
	}
	if len(lines) == 1 {
		return c.Send("No stale PRs found.")
	}
	return c.Send(strings.Join(lines, "\n"), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleUnstale(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	args := strings.Fields(c.Text())
	if len(args) < 3 {
		return c.Send("Usage: /unstale repo_group pr_number")
	}
	repoGroup := args[1]
	prNumber := args[2]
	cfg := config.Current()
	if cfg == nil {
		return c.Send("Config not loaded.")
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return c.Send("Repo group not found: " + repoGroup)
	}
	label := cfg.Stale.StaleLabel
	if label == "" {
		label = "stale"
	}
	removed := false
	for _, pt := range platforms.GroupPlatforms(group) {
		client, ok := b.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := client.RemoveLabel(ctx, owner, repo, commonutil.ParseInt(prNumber), label)
		cancel()
		if err != nil {
			continue
		}
		removed = true
	}
	if !removed {
		return c.Send("Failed to remove stale label.")
	}
	return c.Send("Stale label removed from PR #" + prNumber + " in " + repoGroup)
}

func (b *Bot) handleUsage(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to fetch usage: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return c.Send("Error parsing usage response")
	}
	var sb strings.Builder
	sb.WriteString("<b>💻 System Usage</b>\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: <b>%.1f%%</b>\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: <b>%v</b>\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: <b>%v</b>\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: <b>%v</b>\n", v))
	}
	sb.WriteString("\n<b>Memory</b>\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: <b>%s</b>\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: <b>%s</b>\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: <b>%s</b>\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: <b>%s</b>\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: <b>%.1f%%</b>\n", utils.ToFloat64(pct)))
			}
		}
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) handleStats(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to fetch stats: %v", err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return c.Send("Error parsing stats response")
	}
	var sb strings.Builder
	sb.WriteString("<b>📊 DORA Metrics</b>\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: <b>%.2f</b>\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: <b>%s</b>\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: <b>%.1f%%</b>\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: <b>%s</b>\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n<b>Overview</b>\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: <b>%v</b>\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: <b>%v</b>\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: <b>%v</b>\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: <b>%v</b>\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n<b>By Repo Group</b>\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: <b>%v</b>\n", html.EscapeString(k), v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n<b>By Platform</b>\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: <b>%v</b>\n", k, v))
		}
	}
	return c.Send(sb.String(), &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func (b *Bot) handleVersion(c telebot.Context) error {
	if !b.requireAdmin(c) {
		return nil
	}
	return c.Send(fmt.Sprintf("<b>Asika</b>\nVersion: <code>%s</code>", version.Version),
		&telebot.SendOptions{ParseMode: telebot.ModeHTML})
}
