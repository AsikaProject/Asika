package labeler

import (
	"context"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
)

// Labeler handles label rule application
type Labeler struct {
	clients map[platforms.PlatformType]platforms.PlatformClient
}

// NewLabeler creates a new labeler
func NewLabeler(clients map[platforms.PlatformType]platforms.PlatformClient) *Labeler {
	return &Labeler{
		clients: clients,
	}
}

// HandlePROpened handles PR opened event by fetching diff files and applying rules
func (l *Labeler) HandlePROpened(pr *models.PRRecord, repoGroup string) {
	cfg := config.Current()
	if cfg == nil {
		return
	}

	client, ok := l.clients[platforms.PlatformType(pr.Platform)]
	if !ok {
		slog.Error("no client for platform", "platform", pr.Platform)
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		slog.Error("repo group not found", "name", repoGroup)
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return
	}

	ctx := context.Background()
	files, err := client.GetDiffFiles(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		slog.Error("failed to get diff files", "error", err, "platform", pr.Platform)
		return
	}

	rules := mergeRules(cfg.LabelRules, group.LabelRules)
	sort.Slice(rules, func(i, j int) bool { return rules[i].Priority > rules[j].Priority })

	for _, rule := range rules {
		if matchCompoundRule(rule, files, pr.Title, pr.Author) {
			slog.Info("adding label", "label", rule.Label, "pr", pr.PRNumber, "rule_priority", rule.Priority)
			color := rule.Color
			if color == "" {
				color = "ededed"
			}
			if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, rule.Label, color); err != nil {
				slog.Error("failed to add label", "error", err, "label", rule.Label)
			}
			if rule.Exclusive {
				slog.Info("exclusive rule matched, stopping further labeling", "label", rule.Label, "pr", pr.PRNumber)
				break
			}
		}
	}
}

// ApplyRules applies label rules to a PR based on its changed files, title, and description
func (l *Labeler) ApplyRules(pr *models.PRRecord, repoGroup string, files []string) {
	cfg := config.Current()
	if cfg == nil {
		return
	}

	client, ok := l.clients[platforms.PlatformType(pr.Platform)]
	if !ok {
		slog.Error("no client for platform", "platform", pr.Platform)
		return
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		return
	}

	rules := mergeRules(cfg.LabelRules, group.LabelRules)
	sort.Slice(rules, func(i, j int) bool { return rules[i].Priority > rules[j].Priority })

	ctx := context.Background()
	for _, rule := range rules {
		if matchCompoundRule(rule, files, pr.Title, pr.Author) {
			slog.Info("adding label", "label", rule.Label, "pr", pr.PRNumber)
			color := rule.Color
			if color == "" {
				color = "ededed"
			}
			if err := client.AddLabel(ctx, owner, repo, pr.PRNumber, rule.Label, color); err != nil {
				slog.Error("failed to add label", "error", err, "label", rule.Label)
			}
			if rule.Exclusive {
				break
			}
		}
	}
}

func mergeRules(global, group []models.LabelRule) []models.LabelRule {
	merged := make([]models.LabelRule, 0, len(global)+len(group))
	merged = append(merged, group...)
	merged = append(merged, global...)
	return merged
}

// matchCompoundRule evaluates a label rule against PR data.
// Supports both simple (single pattern) and compound (conditions + logic) rules.
func matchCompoundRule(rule models.LabelRule, files []string, title, author string) bool {
	// Compound rule with conditions
	if len(rule.Conditions) > 0 {
		results := make([]bool, len(rule.Conditions))
		for i, cond := range rule.Conditions {
			results[i] = MatchRule(cond.Pattern, files, title, author)
		}
		logic := strings.ToLower(rule.Logic)
		if logic == "or" {
			for _, r := range results {
				if r {
					return true
				}
			}
			return false
		}
		// Default: AND
		for _, r := range results {
			if !r {
				return false
			}
		}
		return true
	}
	// Simple rule with single pattern
	if rule.Pattern != "" {
		return MatchRule(rule.Pattern, files, title, author)
	}
	return false
}

// MatchRule checks if a pattern matches against files, title, or author.
// Supports scope prefixes: file: (default), title:, author:
func MatchRule(pattern string, files []string, title, author string) bool {
	scope := "file"
	pat := pattern

	if idx := strings.Index(pattern, ":"); idx > 0 && idx < 10 {
		prefix := strings.ToLower(pattern[:idx])
		if prefix == "title" || prefix == "author" || prefix == "file" {
			scope = prefix
			pat = pattern[idx+1:]
		}
	}

	var targets []string
	switch scope {
	case "title":
		targets = []string{title}
	case "author":
		targets = []string{author}
	default:
		targets = files
	}

	for _, target := range targets {
		if matchSinglePattern(pat, target) {
			return true
		}
	}
	return false
}

func matchPattern(pattern string, files []string) bool {
	for _, file := range files {
		if matchSinglePattern(pattern, file) {
			return true
		}
	}
	return false
}

var compiledPatterns = make(map[string]*regexp.Regexp)

func matchSinglePattern(pattern, file string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		matched, _ := path.Match(pattern, file)
		if matched {
			return true
		}
	}

	re, ok := compiledPatterns[pattern]
	if !ok {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return false
		}
		compiledPatterns[pattern] = re
	}
	return re.MatchString(file)
}
