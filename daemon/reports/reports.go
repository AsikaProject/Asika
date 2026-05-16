package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/notifier"
)

type ScheduleConfig = models.ScheduleConfig

var defaultSchedule = ScheduleConfig{
	Enabled: false,
	Cron:    "weekly",
}

type Scheduler struct {
	cfg  ScheduleConfig
	cron *cron.Cron
	stop chan struct{}
}

func NewScheduler(cfg ScheduleConfig) *Scheduler {
	if cfg.Cron == "" {
		cfg.Cron = defaultSchedule.Cron
	}
	return &Scheduler{
		cfg:  cfg,
		stop: make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	if !s.cfg.Enabled {
		slog.Info("scheduled reports disabled")
		return
	}

	cronExpr := s.cfg.Cron
	if cronExpr == "" {
		cronExpr = "weekly"
	}
	if !isValidCron(cronExpr) {
		slog.Warn("invalid cron expression, falling back to weekly", "cron", cronExpr)
		cronExpr = "weekly"
	}

	s.cron = cron.New(cron.WithLogger(slogCronLogger{}))
	_, err := s.cron.AddFunc(cronSchedule(cronExpr), func() {
		s.runReport()
	})
	if err != nil {
		slog.Error("failed to schedule report", "error", err)
		return
	}
	s.cron.Start()
	slog.Info("scheduled reports started", "cron", cronExpr)
}

func (s *Scheduler) Stop() {
	if s.cron != nil {
		s.cron.Stop()
	}
	close(s.stop)
}

func isValidCron(expr string) bool {
	for _, v := range []string{"hourly", "daily", "weekly", "monthly"} {
		if expr == v {
			return true
		}
	}
	_, err := cron.ParseStandard(expr)
	return err == nil
}

func cronSchedule(expr string) string {
	switch expr {
	case "hourly":
		return "@hourly"
	case "daily":
		return "@daily"
	case "weekly":
		return "@weekly"
	case "monthly":
		return "@monthly"
	default:
		return expr
	}
}

type slogCronLogger struct{}

func (l slogCronLogger) Info(msg string, keysAndValues ...interface{}) {
	slog.Info(msg, keysAndValues...)
}

func (l slogCronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	slog.Error(msg, "error", err)
}

func (s *Scheduler) runReport() {
	slog.Info("generating scheduled report")
	report, period, err := s.generateReport()
	if err != nil {
		slog.Error("failed to generate report", "error", err)
		return
	}
	s.sendReport(report)

	entry := db.ReportHistoryEntry{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Period:    period,
		Content:   report,
	}
	data, _ := json.Marshal(entry)
	if err := db.PutReportHistory(entry.ID, data); err != nil {
		slog.Warn("failed to store report history", "error", err)
	}
}

func (s *Scheduler) sendReport(body string) {
	cfg := config.Current()
	if cfg == nil || len(cfg.Notify) == 0 {
		slog.Warn("no notifiers configured for scheduled reports")
		return
	}
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

func (s *Scheduler) generateReport() (string, int, error) {
	cfg := config.Current()
	if cfg == nil {
		return "", 0, fmt.Errorf("config not loaded")
	}

	addr := cfg.Server.Listen
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "localhost" + addr
	}

	period := 7
	if s.cfg.PeriodDays > 0 {
		period = s.cfg.PeriodDays
	}

	stats, err := fetchStats(addr, period)
	if err != nil {
		return "", period, err
	}

	teamStats, _ := fetchTeamStats(addr, period)

	report := formatReportHTML(stats, teamStats, period)
	return report, period, nil
}

func fetchStats(addr string, period int) (map[string]interface{}, error) {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	} else if strings.HasPrefix(host, "0.0.0.0:") {
		host = "localhost" + host[len("0.0.0.0"):]
	}
	url := fmt.Sprintf("http://%s/api/v1/stats?period=%d", host, period)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Report", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stats request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats API returned %d", resp.StatusCode)
	}
	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode stats: %w", err)
	}
	return stats, nil
}

func fetchTeamStats(addr string, period int) (*models.TeamStats, error) {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	} else if strings.HasPrefix(host, "0.0.0.0:") {
		host = "localhost" + host[len("0.0.0.0"):]
	}
	url := fmt.Sprintf("http://%s/api/v1/stats/team?period=%d", host, period)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Report", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var ts models.TeamStats
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		return nil, nil
	}
	return &ts, nil
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

func formatReportHTML(stats map[string]interface{}, teamStats *models.TeamStats, period int) string {
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

	report := fmt.Sprintf("📊 Asika Weekly Report (last %d days)\n\n", period)
	report += "DORA Metrics:\n"
	report += fmt.Sprintf("  Deployments/Day: %.1f\n", deployments)
	report += fmt.Sprintf("  Lead Time: %.1f hours\n", leadTime)
	report += fmt.Sprintf("  Failure Rate: %.1f%%\n", failureRate*100)
	report += fmt.Sprintf("  MTTR: %.1f hours\n\n", mttr)
	report += "PR Overview:\n"
	report += fmt.Sprintf("  Total PRs: %d | Open: %d | Merged: %d\n", totalPRs, openPRs, mergedPRs)
	report += fmt.Sprintf("  Queue Items: %d | Failed Queue: %d\n\n", queueItems, failedQueue)

	if byGroup, ok := stats["prs_by_repo_group"].(map[string]interface{}); ok && len(byGroup) > 0 {
		report += "By Repo Group:\n"
		for group, count := range byGroup {
			report += fmt.Sprintf("  %s: %v\n", group, count)
		}
		report += "\n"
	}

	if byPlatform, ok := stats["prs_by_platform"].(map[string]interface{}); ok && len(byPlatform) > 0 {
		report += "By Platform:\n"
		for plat, count := range byPlatform {
			report += fmt.Sprintf("  %s: %v\n", plat, count)
		}
		report += "\n"
	}

	if teamStats != nil && len(teamStats.TopContributors) > 0 {
		report += "Top Contributors:\n"
		for i, a := range teamStats.TopContributors {
			report += fmt.Sprintf("  %d. %s — opened: %d, merged: %d, reviewed: %d\n",
				i+1, a.Author, a.PRsOpened, a.PRsMerged, a.PRsReviewed)
		}
	}

	return report
}
