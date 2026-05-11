package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"asika/common/config"
	"asika/common/utils"
	"asika/common/version"
)

func (b *Bot) handleShowQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	repoGroup := ""
	if len(args) > 1 {
		repoGroup = args[1]
	}
	items, err := b.queueMgr.GetQueueItems(repoGroup)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Error fetching queue: %v", err))
		return
	}
	if len(items) == 0 {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Queue is empty for repo group *%s*.", repoGroup))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*Merge Queue for %s*\n\n", repoGroup))
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. %s [%s]\n", i+1, item.PRID, item.Status))
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleRecheckQueue(ev *slack.MessageEvent, client *socketmode.Client) {
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	go b.queueMgr.CheckQueue()
	b.postMessage(client, ev.Channel, "Queue recheck triggered.")
}

func (b *Bot) handleClearQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
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
		b.postMessage(client, ev.Channel, "No repo group configured.")
		return
	}
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	count, err := b.queueMgr.ClearQueue(repoGroup)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to clear queue: %v", err))
		return
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("Queue cleared for *%s*. %d items removed.", repoGroup, count))
}

func (b *Bot) handleRemoveFromQueue(ev *slack.MessageEvent, client *socketmode.Client, args []string) {
	if len(args) < 3 {
		b.postMessage(client, ev.Channel, "Usage: queue_remove <repo_group> <pr_id>")
		return
	}
	if b.queueMgr == nil {
		b.postMessage(client, ev.Channel, "Queue manager not initialized.")
		return
	}
	if err := b.queueMgr.RemoveFromQueue(args[1], args[2]); err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to remove: %v", err))
		return
	}
	b.postMessage(client, ev.Channel, fmt.Sprintf("Removed *%s* from queue.", args[2]))
}

func (b *Bot) handleShowConfig(ev *slack.MessageEvent, client *socketmode.Client) {
	cfg := b.cfg
	text := fmt.Sprintf("*Asika Config*\nListen: %s\nMode: %s\nCPU Threads: min=%d max=%d\nRepo Groups: %d",
		cfg.Server.Listen, cfg.Server.Mode, cfg.Server.MinProcs, cfg.Server.MaxProcs, len(cfg.RepoGroups))
	b.postMessage(client, ev.Channel, text)
}

func (b *Bot) handleStats(ev *slack.MessageEvent, client *socketmode.Client) {
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=30", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch stats: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		b.postMessage(client, ev.Channel, "Error parsing stats response")
		return
	}
	var sb strings.Builder
	sb.WriteString("*📊 DORA Metrics*\n\n")
	if v, ok := result["deployment_frequency"]; ok {
		sb.WriteString(fmt.Sprintf("🚀 Deployments/Day: *%.2f*\n", utils.ToFloat64(v)))
	}
	if v, ok := result["lead_time_hours"]; ok {
		sb.WriteString(fmt.Sprintf("⏱ Lead Time: *%s*\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	if v, ok := result["change_failure_rate"]; ok {
		sb.WriteString(fmt.Sprintf("💥 Failure Rate: *%.1f%%*\n", utils.ToFloat64(v)*100))
	}
	if v, ok := result["mttr_hours"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 MTTR: *%s*\n", utils.FormatHours(utils.ToFloat64(v))))
	}
	sb.WriteString("\n*Overview*\n")
	if v, ok := result["total_prs"]; ok {
		sb.WriteString(fmt.Sprintf("📋 Total PRs: *%v*\n", v))
	}
	if v, ok := result["open_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟢 Open: *%v*\n", v))
	}
	if v, ok := result["merged_prs"]; ok {
		sb.WriteString(fmt.Sprintf("🟣 Merged: *%v*\n", v))
	}
	if v, ok := result["queue_items"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Queue: *%v*\n", v))
	}
	if byGroup, ok := result["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		sb.WriteString("\n*By Repo Group*\n")
		for k, v := range byGroup {
			sb.WriteString(fmt.Sprintf("  %s: *%v*\n", k, v))
		}
	}
	if byPlat, ok := result["prs_by_platform"].(map[string]interface{}); ok && len(byPlat) > 0 {
		sb.WriteString("\n*By Platform*\n")
		for k, v := range byPlat {
			sb.WriteString(fmt.Sprintf("  %s: *%v*\n", k, v))
		}
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func (b *Bot) handleUsage(ev *slack.MessageEvent, client *socketmode.Client) {
	url := fmt.Sprintf("http://localhost%s/api/v1/usage", b.cfg.Server.Listen)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+b.internalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.postMessage(client, ev.Channel, fmt.Sprintf("Failed to fetch usage: %v", err))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		b.postMessage(client, ev.Channel, "Error parsing usage response")
		return
	}
	var sb strings.Builder
	sb.WriteString("*💻 System Usage*\n\n")
	if v, ok := result["cpu_percent"]; ok {
		sb.WriteString(fmt.Sprintf("🖥 CPU: *%.1f%%*\n", utils.ToFloat64(v)))
	}
	if v, ok := result["num_cpu"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 Cores: *%v*\n", v))
	}
	if v, ok := result["goroutines"]; ok {
		sb.WriteString(fmt.Sprintf("🧵 Goroutines: *%v*\n", v))
	}
	if v, ok := result["pid"]; ok {
		sb.WriteString(fmt.Sprintf("🔢 PID: *%v*\n", v))
	}
	sb.WriteString("\n*Memory*\n")
	if v, ok := result["mem_alloc_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📦 Alloc: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_total_mb"]; ok {
		sb.WriteString(fmt.Sprintf("📊 Total: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_sys_mb"]; ok {
		sb.WriteString(fmt.Sprintf("🔧 Sys: *%s*\n", formatMemMB(utils.ToFloat64(v))))
	}
	if v, ok := result["mem_limit_mb"]; ok {
		limit := utils.ToFloat64(v)
		if limit > 0 {
			sb.WriteString(fmt.Sprintf("🚫 GOMEMLIMIT: *%s*\n", formatMemMB(limit)))
			if pct, ok := result["mem_percent"]; ok {
				sb.WriteString(fmt.Sprintf("📈 Usage: *%.1f%%*\n", utils.ToFloat64(pct)))
			}
		}
	}
	b.postMessage(client, ev.Channel, sb.String())
}

func formatMemMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

func (b *Bot) handleVersion(ev *slack.MessageEvent, client *socketmode.Client) {
	b.postMessage(client, ev.Channel, fmt.Sprintf("*Asika*\nVersion: `%s`", version.Version))
}
