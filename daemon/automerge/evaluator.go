package automerge

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
)

type Evaluator struct {
	cfg     *models.Config
	clients map[platforms.PlatformType]platforms.PlatformClient
}

func NewEvaluator(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) *Evaluator {
	return &Evaluator{
		cfg:     cfg,
		clients: clients,
	}
}

func (e *Evaluator) EvaluateAll() {
	if !e.cfg.AutoMerge.Enabled {
		return
	}
	groups := config.GetRepoGroups(e.cfg)
	for i := range groups {
		e.evaluateGroup(&groups[i])
	}
}

func (e *Evaluator) EvaluatePR(pr *models.PRRecord) {
	if !e.cfg.AutoMerge.Enabled || pr == nil {
		return
	}
	if pr.State != "open" || pr.IsDraft || pr.SpamFlag {
		return
	}
	for _, rule := range e.cfg.AutoMerge.Rules {
		if rule.Enabled && e.matchesRule(pr, &rule) {
			slog.Info("auto-merge: PR matches rule, merging", "pr", pr.PRNumber, "rule", rule.Name, "repo_group", pr.RepoGroup)
			e.mergePR(pr)
			return
		}
	}
}

func (e *Evaluator) evaluateGroup(group *models.RepoGroup) {
	for _, rule := range e.cfg.AutoMerge.Rules {
		if !rule.Enabled {
			continue
		}
		for _, pt := range platforms.GroupPlatforms(group) {
			client, ok := e.clients[pt]
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
				slog.Warn("auto-merge: failed to list PRs", "platform", pt, "error", err)
				continue
			}
			for _, pr := range prs {
				if pr == nil || pr.IsDraft || pr.SpamFlag {
					continue
				}
				if e.matchesRule(pr, &rule) {
					slog.Info("auto-merge: PR matches rule, merging", "pr", pr.PRNumber, "rule", rule.Name)
					e.mergePR(pr)
				}
			}
		}
	}
}

func (e *Evaluator) matchesRule(pr *models.PRRecord, rule *models.AutoMergeRule) bool {
	for _, excluded := range rule.ExcludeAuthors {
		if pr.Author == excluded {
			return false
		}
	}
	for _, excluded := range rule.ExcludeLabels {
		for _, l := range pr.Labels {
			if l == excluded {
				return false
			}
		}
	}
	if len(rule.Labels) > 0 {
		hasLabel := false
		for _, ruleLabel := range rule.Labels {
			for _, prLabel := range pr.Labels {
				if ruleLabel == prLabel {
					hasLabel = true
					break
				}
			}
			if hasLabel {
				break
			}
		}
		if !hasLabel {
			return false
		}
	}
	if rule.RequiredApprovals > 0 {
		group := config.GetRepoGroupByName(e.cfg, pr.RepoGroup)
		required := rule.RequiredApprovals
		if group != nil && group.MergeQueue.RequiredApprovals > required {
			required = group.MergeQueue.RequiredApprovals
		}
		if pr.IsApproved && required > 1 {
			data, err := db.GetPRByIndex(pr.ID, pr.RepoGroup, pr.PRNumber)
			if err != nil {
				return false
			}
			var fullPR models.PRRecord
			if err := json.Unmarshal(data, &fullPR); err != nil {
				return false
			}
			approvals := 0
			for _, ev := range fullPR.Events {
				if ev.Action == "approved" {
					approvals++
				}
			}
			if approvals < required {
				return false
			}
		} else if !pr.IsApproved {
			return false
		}
	}
	if rule.CIRequired && pr.HasConflict {
		return false
	}
	return true
}

func (e *Evaluator) mergePR(pr *models.PRRecord) {
	client, ok := e.clients[platforms.PlatformType(pr.Platform)]
	if !ok {
		slog.Error("auto-merge: no client for platform", "platform", pr.Platform)
		return
	}
	group := config.GetRepoGroupByName(e.cfg, pr.RepoGroup)
	if group == nil {
		slog.Error("auto-merge: repo group not found", "repo_group", pr.RepoGroup)
		return
	}
	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		slog.Error("auto-merge: cannot resolve repo", "platform", pr.Platform, "repo_group", pr.RepoGroup)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	method, err := client.GetDefaultMergeMethod(ctx, owner, repo)
	if err != nil {
		method = "merge"
	}
	if err := client.MergePR(ctx, owner, repo, pr.PRNumber, method); err != nil {
		slog.Error("auto-merge: merge failed", "pr", pr.PRNumber, "error", err)
		return
	}
	slog.Info("auto-merge: merge succeeded", "pr", pr.PRNumber, "rule", pr.RepoGroup)
}
