package reviewer

import (
	"context"
	"log/slog"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
	"asika/daemon/labeler"
)

// Reviewer handles automatic reviewer assignment based on review rules.
type Reviewer struct {
	clients map[platforms.PlatformType]platforms.PlatformClient
}

// NewReviewer creates a new reviewer.
func NewReviewer(clients map[platforms.PlatformType]platforms.PlatformClient) *Reviewer {
	return &Reviewer{clients: clients}
}

// HandlePROpened processes a PR opened event and assigns reviewers if rules match.
func (r *Reviewer) HandlePROpened(pr *models.PRRecord, repoGroup string) {
	cfg := config.Current()
	if cfg == nil {
		return
	}
	rules := cfg.ReviewRules
	if len(rules) == 0 {
		return
	}

	client, ok := r.clients[platforms.PlatformType(pr.Platform)]
	if !ok {
		slog.Error("no client for reviewer assignment", "platform", pr.Platform)
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

	// Fetch diff files for pattern matching
	ctx := context.Background()
	files, err := client.GetDiffFiles(ctx, owner, repo, pr.PRNumber)
	if err != nil {
		slog.Warn("failed to get diff files for reviewer assignment", "error", err, "pr", pr.PRNumber)
		return
	}

	// Collect all reviewers from matching rules
	reviewerSet := make(map[string]bool)
	for _, rule := range rules {
		if matchReviewRule(rule, files, pr.Title, pr.Author) {
			for _, rev := range rule.Reviewers {
				reviewerSet[rev] = true
			}
		}
	}

	if len(reviewerSet) == 0 {
		return
	}

	reviewers := make([]string, 0, len(reviewerSet))
	for rev := range reviewerSet {
		reviewers = append(reviewers, rev)
	}

	slog.Info("requesting reviewers", "pr", pr.PRNumber, "reviewers", reviewers)
	if err := client.RequestReview(ctx, owner, repo, pr.PRNumber, reviewers); err != nil {
		slog.Error("failed to request reviewers", "error", err, "pr", pr.PRNumber)
	}
}

func matchReviewRule(rule models.ReviewRule, files []string, title, author string) bool {
	if rule.Pattern == "" {
		return false
	}
	return labeler.MatchRule(rule.Pattern, files, title, author)
}
