package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"asika/common/config"
	"asika/common/models"
	"asika/common/notifier"
)

// ScheduleConfig is aliased to models.ScheduleConfig for use in this package.
type ScheduleConfig = models.ScheduleConfig

var defaultSchedule = ScheduleConfig{
	Enabled: false,
	Cron:    "weekly",
}

// Scheduler manages periodic report generation and delivery.
type Scheduler struct {
	cfg    ScheduleConfig
	ticker *time.Ticker
	stop   chan struct{}
}

// NewScheduler creates a new report scheduler.
func NewScheduler(cfg ScheduleConfig) *Scheduler {
	if cfg.Cron == "" {
		cfg.Cron = defaultSchedule.Cron
	}

	return &Scheduler{
		cfg:  cfg,
		stop: make(chan struct{}),
	}
}

// Start begins the scheduled report loop.
func (s *Scheduler) Start() {
	if !s.cfg.Enabled {
		slog.Info("scheduled reports disabled")
		return
	}
	interval := cronToInterval(s.cfg.Cron)
	s.ticker = time.NewTicker(interval)
	slog.Info("scheduled reports started", "interval", interval)

	go func() {
		// Run immediately on start, then on each tick
		s.runReport()
		for {
			select {
			case <-s.ticker.C:
				s.runReport()
			case <-s.stop:
				s.ticker.Stop()
				return
			}
		}
	}()
}

// Stop halts the scheduler.
func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) runReport() {
	slog.Info("generating scheduled report")
	report, err := s.generateReport()
	if err != nil {
		slog.Error("failed to generate report", "error", err)
		return
	}
	s.sendReport(report)
}

func (s *Scheduler) sendReport(body string) {
	cfg := config.Current()
	if cfg == nil || len(cfg.Notify) == 0 {
		slog.Warn("no notifiers configured for scheduled reports")
		return
	}
	// Create notifiers from config and send
	notifiers := make([]notifier.Notifier, 0, len(cfg.Notify))
	for _, nc := range cfg.Notify {
		n := createNotifier(nc)
		if n != nil {
			notifiers = append(notifiers, n)
		}
	}
	notifier.WirePlatformNotifiers(notifiers, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, n := range notifiers {
		if err := n.Send(ctx, "Asika Scheduled Report", body); err != nil {
			slog.Warn("report notification failed", "type", n.Type(), "error", err)
		}
	}
}

func createNotifier(nc models.NotifyConfig) notifier.Notifier {
	switch nc.Type {
	case "smtp":
		return notifier.NewSMTPNotifier(nc.Config)
	case "telegram":
		return notifier.NewTelegramNotifier(nc.Config)
	case "discord":
		return notifier.NewDiscordNotifier(nc.Config)
	case "slack":
		return notifier.NewSlackNotifier(nc.Config)
	case "slack_bot":
		return notifier.NewSlackBotNotifier(nc.Config)
	case "feishu":
		return notifier.NewFeishuNotifier(nc.Config)
	case "webhook":
		return notifier.NewWebhookNotifier(nc.Config)
	case "msteams":
		return notifier.NewMSTeamsNotifier(nc.Config)
	case "dingtalk":
		return notifier.NewDingTalkNotifier(nc.Config)
	case "wecom":
		return notifier.NewWeComNotifier(nc.Config)
	}
	return nil
}

func (s *Scheduler) generateReport() (string, error) {
	cfg := config.Current()
	if cfg == nil {
		return "", fmt.Errorf("config not loaded")
	}

	// Call the stats API internally via HTTP
	addr := cfg.Server.Listen
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "localhost" + addr
	}
	url := fmt.Sprintf("http://localhost%s/api/v1/stats?period=7", addr)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// Use internal auth bypass
	req.Header.Set("X-Internal-Report", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("stats request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stats API returned %d", resp.StatusCode)
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return "", fmt.Errorf("failed to decode stats: %w", err)
	}

	return formatReport(stats), nil
}

func formatReport(stats map[string]interface{}) string {
	var period int
	if v, ok := stats["period_days"].(float64); ok {
		period = int(v)
	}

	var deployments, leadTime, failureRate, mttr float64
	var totalPRs, mergedPRs, openPRs, queueItems, failedQueue int

	if v, ok := stats["deployment_frequency"].(float64); ok {
		deployments = v
	}
	if v, ok := stats["lead_time_hours"].(float64); ok {
		leadTime = v
	}
	if v, ok := stats["change_failure_rate"].(float64); ok {
		failureRate = v
	}
	if v, ok := stats["mttr_hours"].(float64); ok {
		mttr = v
	}
	if v, ok := stats["total_prs"].(float64); ok {
		totalPRs = int(v)
	}
	if v, ok := stats["merged_prs"].(float64); ok {
		mergedPRs = int(v)
	}
	if v, ok := stats["open_prs"].(float64); ok {
		openPRs = int(v)
	}
	if v, ok := stats["queue_items"].(float64); ok {
		queueItems = int(v)
	}
	if v, ok := stats["failed_queue_items"].(float64); ok {
		failedQueue = int(v)
	}

	report := fmt.Sprintf("📊 Asika DORA Report (last %d days)\n\n", period)
	report += fmt.Sprintf("Deployments/Day: %.1f\n", deployments)
	report += fmt.Sprintf("Lead Time: %.1f hours\n", leadTime)
	report += fmt.Sprintf("Failure Rate: %.1f%%\n", failureRate*100)
	report += fmt.Sprintf("MTTR: %.1f hours\n\n", mttr)
	report += "Overview:\n"
	report += fmt.Sprintf("  Total PRs: %d\n", totalPRs)
	report += fmt.Sprintf("  Open: %d\n", openPRs)
	report += fmt.Sprintf("  Merged: %d\n", mergedPRs)
	report += fmt.Sprintf("  Queue Items: %d\n", queueItems)
	report += fmt.Sprintf("  Failed Queue: %d\n", failedQueue)

	if byGroup, ok := stats["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		report += "\nBy Repo Group:\n"
		for group, count := range byGroup {
			report += fmt.Sprintf("  %s: %v\n", group, count)
		}
	}

	return report
}

// cronToInterval converts a cron-like string to a time.Duration.
func cronToInterval(cron string) time.Duration {
	switch cron {
	case "hourly":
		return 1 * time.Hour
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default:
		if d, err := time.ParseDuration(cron); err == nil {
			return d
		}
		return 7 * 24 * time.Hour
	}
}
