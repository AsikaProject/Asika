package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) handleShowQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) > 0 {
			repoGroup = groups[0].Name
		}
	}
	var items []models.QueueItem
	db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if repoGroup == "" || item.RepoGroup == repoGroup || strings.HasPrefix(string(key), repoGroup+"#") {
			items = append(items, item)
		}
		return nil
	})
	if len(items) == 0 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Queue empty for **%s**.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Merge Queue — %s**\n\n", repoGroup))
	for _, item := range items {
		statusEmoji := "⏳"
		switch item.Status {
		case "done":
			statusEmoji = "✅"
		case "failed":
			statusEmoji = "❌"
		case "merging":
			statusEmoji = "🔄"
		}
		sb.WriteString(fmt.Sprintf("%s %s (%s) — %s\n", statusEmoji, item.PRID, item.Status, item.AddedAt.Format(time.RFC3339)))
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleRecheckQueue(s *discordgo.Session, m *discordgo.MessageCreate) {
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}
	go b.queueMgr.CheckQueue()
	s.ChannelMessageSend(m.ChannelID, "Queue recheck triggered.")
}

func (b *Bot) handleClearQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	} else {
		groups := config.GetRepoGroups(b.cfg)
		if len(groups) > 0 {
			repoGroup = groups[0].Name
		}
	}
	if repoGroup == "" {
		s.ChannelMessageSend(m.ChannelID, "No repo group configured.")
		return
	}
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to clear queue: %v", err))
		return
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Queue cleared for **%s**. %d items removed.", repoGroup, count))
}

func (b *Bot) handleRemoveFromQueue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSend(m.ChannelID, "Usage: !queue_remove <repo_group> <pr_id")
		return
	}
	if b.queueMgr == nil {
		s.ChannelMessageSend(m.ChannelID, "Queue manager not initialized.")
		return
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to remove: %v", err))
		return
	}
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Removed **%s** from queue.", args[2]))
}

func (b *Bot) handleShowConfig(s *discordgo.Session, m *discordgo.MessageCreate) {
	cfg := config.Current()
	if cfg == nil {
		s.ChannelMessageSend(m.ChannelID, "Config not loaded.")
		return
	}
	groups := config.GetRepoGroups(cfg)
	var sb strings.Builder
	sb.WriteString("**Current Config**\n\n")
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
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleUsage(s *discordgo.Session, m *discordgo.MessageCreate) {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to fetch usage: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing usage response")
		return
	}
	var sb strings.Builder
	sb.WriteString("**💻 System Usage**\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: **%.1f%%**\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: **%v**\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: **%v**\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: **%v**\n", v))
	}
	sb.WriteString("\n**Memory**\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: **%s**\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: **%s**\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: **%s**\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: **%s**\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: **%.1f%%**\n", utils.ToFloat64(pct)))
			}
		}
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleStats(s *discordgo.Session, m *discordgo.MessageCreate) {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to fetch stats: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		s.ChannelMessageSend(m.ChannelID, "Error parsing stats response")
		return
	}
	var sb strings.Builder
	sb.WriteString("**📊 DORA Metrics**\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: **%.2f**\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: **%s**\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: **%.1f%%**\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: **%s**\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n**Overview**\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: **%v**\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: **%v**\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: **%v**\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: **%v**\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n**By Repo Group**\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: **%v**\n", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n**By Platform**\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: **%v**\n", k, v))
		}
	}
	s.ChannelMessageSend(m.ChannelID, sb.String())
}

func (b *Bot) handleVersion(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("**Asika**\nVersion: `%s`", version.Version))
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}
