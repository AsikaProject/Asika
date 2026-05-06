package stale

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"asika/common/config"
	"asika/common/events"
	"asika/common/models"
	"asika/common/notifier"
	"asika/common/platforms"
)

type Manager struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
}

func NewManager(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Manager {
	return &Manager{
		cfg:     cfg,
		clients: clients,
	}
}

func (m *Manager) CheckAllGroups() {
	if !m.cfg.Stale.Enabled {
		return
	}
	groups := config.GetRepoGroups(m.cfg)
	for _, group := range groups {
		m.CheckRepoGroup(&group)
	}
}

func (m *Manager) CheckRepoGroup(group *models.RepoGroup) {
	cfg := m.cfg.Stale
	slog.Info("stale: checking repo group", "group", group.Name)

	platformsForGroup := groupPlatforms(group)
	for _, pt := range platformsForGroup {
		client, ok := m.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}

		ctx := context.Background()
		prs, err := client.ListPRs(ctx, owner, repo, "open")
		if err != nil {
			slog.Error("stale: failed to list PRs", "platform", pt, "error", err)
			continue
		}

		for _, pr := range prs {
			if pr == nil {
				continue
			}
			m.processPR(client, group, owner, repo, pr, &cfg)
		}
	}
}

func (m *Manager) CheckRepoGroupDryRun(group *models.RepoGroup) []StaleAction {
	cfg := m.cfg.Stale
	var actions []StaleAction

	platformsForGroup := groupPlatforms(group)
	for _, pt := range platformsForGroup {
		client, ok := m.clients[pt]
		if !ok {
			continue
		}
		owner, repo := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || repo == "" {
			continue
		}

		ctx := context.Background()
		prs, err := client.ListPRs(ctx, owner, repo, "open")
		if err != nil {
			continue
		}

		for _, pr := range prs {
			if pr == nil {
				continue
			}
			action := m.analyzePR(client, group, pr, &cfg)
			if action.Type != "" {
				action.Platform = string(pt)
				action.Repo = fmt.Sprintf("%s/%s", owner, repo)
				actions = append(actions, action)
			}
		}
	}
	return actions
}

type StaleAction struct {
	Type      string `json:"type"` // "mark" | "close" | "skip"
	Platform  string `json:"platform"`
	Repo      string `json:"repo"`
	PRNumber  int    `json:"pr_number"`
	PRTitle   string `json:"pr_title"`
	Reason    string `json:"reason"`
}

func (m *Manager) analyzePR(client platforms.PlatformClient, group *models.RepoGroup, pr *models.PRRecord, cfg *models.StaleConfig) StaleAction {
	labelBase := cfg.StaleLabel
	if labelBase == "" {
		labelBase = "stale"
	}

	hasStaleLabel := hasLabel(pr.Labels, labelBase)
	daysInactive := inactivityDays(pr.UpdatedAt)

	if cfg.SkipDraftPRs && pr.IsDraft {
		return StaleAction{Type: "skip", PRNumber: pr.PRNumber, PRTitle: pr.Title, Reason: "draft PR"}
	}

	isExempt := false
	for _, exempt := range cfg.ExemptLabels {
		if hasLabel(pr.Labels, exempt) {
			isExempt = true
			break
		}
	}
	if isExempt {
		return StaleAction{Type: "skip", PRNumber: pr.PRNumber, PRTitle: pr.Title, Reason: "exempt label"}
	}

	if hasStaleLabel && cfg.DaysUntilClose > 0 && daysInactive >= cfg.DaysUntilStale+cfg.DaysUntilClose {
		return StaleAction{Type: "close", PRNumber: pr.PRNumber, PRTitle: pr.Title, Reason: fmt.Sprintf("stale for %d days, inactive for %d days total", daysInactive-cfg.DaysUntilStale, daysInactive)}
	}

	if !hasStaleLabel && daysInactive >= cfg.DaysUntilStale {
		return StaleAction{Type: "mark", PRNumber: pr.PRNumber, PRTitle: pr.Title, Reason: fmt.Sprintf("inactive for %d days", daysInactive)}
	}

	return StaleAction{}
}

func (m *Manager) processPR(client platforms.PlatformClient, group *models.RepoGroup, owner, repo string, pr *models.PRRecord, cfg *models.StaleConfig) {
	labelBase := cfg.StaleLabel
	if labelBase == "" {
		labelBase = "stale"
	}

	if cfg.SkipDraftPRs && pr.IsDraft {
		return
	}

	isExempt := false
	for _, exempt := range cfg.ExemptLabels {
		if hasLabel(pr.Labels, exempt) {
			isExempt = true
			break
		}
	}
	if isExempt {
		return
	}

	daysInactive := inactivityDays(pr.UpdatedAt)
	hasStaleLabel := hasLabel(pr.Labels, labelBase)

	if !hasStaleLabel && daysInactive >= cfg.DaysUntilStale {
		slog.Info("stale: marking PR", "pr", pr.PRNumber, "title", pr.Title, "days", daysInactive)
		m.markStale(client, group, owner, repo, pr)
	} else if hasStaleLabel && cfg.DaysUntilClose > 0 {
		if daysInactive >= cfg.DaysUntilStale+cfg.DaysUntilClose {
			slog.Info("stale: closing PR", "pr", pr.PRNumber, "title", pr.Title, "days", daysInactive)
			m.autoClose(client, group, owner, repo, pr)
		}
	}
}

func (m *Manager) HandleActivity(pr *models.PRRecord, repoGroup string) {
	cfg := m.cfg.Stale
	if !cfg.Enabled || !cfg.RemoveOnActivity {
		return
	}

	labelBase := cfg.StaleLabel
	if labelBase == "" {
		labelBase = "stale"
	}

	if !hasLabel(pr.Labels, labelBase) {
		return
	}

	groups := config.GetRepoGroups(m.cfg)
	for _, g := range groups {
		if g.Name == repoGroup {
			m.removeStaleLabel(pr, &g, labelBase)
			return
		}
	}
}

func (m *Manager) removeStaleLabel(pr *models.PRRecord, group *models.RepoGroup, label string) {
	platformsForGroup := groupPlatforms(group)
	for _, pt := range platformsForGroup {
		client, ok := m.clients[pt]
		if !ok {
			continue
		}
		owner, r := config.GetOwnerRepoFromGroup(group, string(pt))
		if owner == "" || r == "" {
			continue
		}

		ctx := context.Background()
		if err := client.RemoveLabel(ctx, owner, r, pr.PRNumber, label); err != nil {
			slog.Warn("stale: failed to remove stale label", "pr", pr.PRNumber, "error", err)
			continue
		}
		slog.Info("stale: removed label on activity", "pr", pr.PRNumber, "platform", pt)
	}
}

func (m *Manager) markStale(client platforms.PlatformClient, group *models.RepoGroup, owner, repo string, pr *models.PRRecord) {
	cfg := m.cfg.Stale
	labelBase := cfg.StaleLabel
	if labelBase == "" {
		labelBase = "stale"
	}

	ctx := context.Background()

	if err := ensureLabelExists(ctx, client, owner, repo, labelBase); err != nil {
		slog.Warn("stale: failed to ensure label exists", "label", labelBase, "error", err)
	}

	if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, labelBase, "cccccc"); err != nil {
		slog.Error("stale: failed to add label", "pr", pr.PRNumber, "error", err)
		return
	}

	daysInactive := inactivityDays(pr.UpdatedAt)
	closeIn := "never"
	if cfg.DaysUntilClose > 0 {
		remaining := cfg.DaysUntilClose - (daysInactive - cfg.DaysUntilStale)
		if remaining < 0 {
			remaining = 0
		}
		closeIn = fmt.Sprintf("%d days", remaining)
	}

	exemptLabel := ""
	if len(cfg.ExemptLabels) > 0 {
		exemptLabel = cfg.ExemptLabels[0]
	}

	commentBody := cfg.CommentOnStale
	if commentBody == "" {
		commentBody = "This PR has been marked as stale due to {{.Days}} days of inactivity."
	}

	data := commentData{
		Days:       daysInactive,
		CloseIn:    closeIn,
		ExemptLabel: exemptLabel,
	}
	rendered, err := renderComment(commentBody, data)
	if err != nil {
		slog.Warn("stale: failed to render comment template", "error", err)
		rendered = commentBody
	}

	if err := client.CommentPR(ctx, owner, repo, pr.PRNumber, rendered); err != nil {
		slog.Error("stale: failed to comment", "pr", pr.PRNumber, "error", err)
	}

	if cfg.NotifyOnStale {
		m.sendNotification(group.Name, "PR Marked Stale",
			fmt.Sprintf("PR #%d (%s) in %s has been marked as stale after %d days of inactivity.", pr.PRNumber, pr.Title, group.Name, daysInactive))
	}

	events.PublishPR(events.EventPRLabeled, group.Name, "", pr, "stale")
}

func (m *Manager) autoClose(client platforms.PlatformClient, group *models.RepoGroup, owner, repo string, pr *models.PRRecord) {
	ctx := context.Background()

	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		slog.Error("stale: failed to close PR", "pr", pr.PRNumber, "error", err)
		return
	}

	commentBody := m.cfg.Stale.CommentOnClose
	if commentBody == "" {
		commentBody = "Automatically closed due to inactivity."
	}

	if err := client.CommentPR(ctx, owner, repo, pr.PRNumber, commentBody); err != nil {
		slog.Warn("stale: failed to add close comment", "pr", pr.PRNumber, "error", err)
	}

	m.sendNotification(group.Name, "PR Closed (Stale)",
		fmt.Sprintf("PR #%d (%s) in %s has been automatically closed due to inactivity.", pr.PRNumber, pr.Title, group.Name))

	events.PublishPR(events.EventPRClosed, group.Name, "", pr, "stale")
}

func (m *Manager) sendNotification(group, title, body string) {
	channels := m.cfg.Notify
	if len(channels) == 0 {
		return
	}
	for _, nc := range channels {
		var n notifier.Notifier
		switch nc.Type {
		case "smtp":
			n = notifier.NewSMTPNotifier(nc.Config)
		case "wecom":
			n = notifier.NewWeComNotifier(nc.Config)
		case "github_at":
			n = notifier.NewGitHubAtNotifier(nc.Config)
		case "gitlab_at":
			n = notifier.NewGitLabAtNotifier(nc.Config)
		case "gitea_at":
			n = notifier.NewGiteaAtNotifier(nc.Config)
		case "telegram":
			n = notifier.NewTelegramNotifier(nc.Config)
		case "feishu":
			n = notifier.NewFeishuNotifier(nc.Config)
		case "dingtalk":
			n = notifier.NewDingTalkNotifier(nc.Config)
		}
		if n == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := n.Send(ctx, "["+group+"] "+title, body)
		cancel()
		if err != nil {
			slog.Warn("stale: notification failed", "type", nc.Type, "error", err)
		}
	}
}

func groupPlatforms(group *models.RepoGroup) []platforms.PlatformType {
	var result []platforms.PlatformType
	if group.GitHub != "" {
		result = append(result, platforms.PlatformGitHub)
	}
	if group.GitLab != "" {
		result = append(result, platforms.PlatformGitLab)
	}
	if group.Gitea != "" {
		result = append(result, platforms.PlatformGitea)
	}
	if group.Forgejo != "" {
		result = append(result, platforms.PlatformForgejo)
	}
	if group.Codeberg != "" {
		result = append(result, platforms.PlatformCodeberg)
	}
	if group.Bitbucket != "" {
		result = append(result, platforms.PlatformBitbucket)
	}
	return result
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func inactivityDays(lastActive time.Time) int {
	if lastActive.IsZero() {
		return 0
	}
	dur := time.Since(lastActive)
	return int(math.Floor(dur.Hours() / 24))
}

func ensureLabelExists(ctx context.Context, client platforms.PlatformClient, owner, repo, labelName string) error {
	err := client.CreateLabel(ctx, owner, repo, labelName, "cccccc", "Marked as inactive by Asika")
	if err != nil {
		return err
	}
	return nil
}
