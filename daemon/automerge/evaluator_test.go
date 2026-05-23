package automerge

import (
	"testing"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
)

func TestNewEvaluator(t *testing.T) {
	cfg := &models.Config{}
	clients := map[platforms.PlatformType]platforms.PlatformClient{}
	e := NewEvaluator(cfg, clients)
	if e == nil {
		t.Fatal("expected non-nil Evaluator")
	}
	if e.cfg != cfg {
		t.Error("cfg not set correctly")
	}
	if e.clients == nil {
		t.Error("clients map should be initialized")
	}
}

func TestMatchesRule(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{
				Name: "test-group",
				MergeQueue: models.MergeQueueConfig{
					RequiredApprovals: 1,
				},
			},
		},
	}
	config.Store(cfg)
	e := NewEvaluator(cfg, nil)

	t.Run("empty rule matches everything", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "alice",
			Labels:    []string{"bug"},
		}
		rule := &models.AutoMergeRule{Enabled: true}
		if !e.matchesRule(pr, rule) {
			t.Error("empty rule should match any PR")
		}
	})

	t.Run("PR matches label rule", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "alice",
			Labels:    []string{"auto-merge", "bug"},
		}
		rule := &models.AutoMergeRule{
			Enabled: true,
			Labels:  []string{"auto-merge"},
		}
		if !e.matchesRule(pr, rule) {
			t.Error("PR with matching label should match rule")
		}
	})

	t.Run("PR excluded by author", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "bot-user",
			Labels:    []string{"auto-merge"},
		}
		rule := &models.AutoMergeRule{
			Enabled:        true,
			Labels:         []string{"auto-merge"},
			ExcludeAuthors: []string{"bot-user"},
		}
		if e.matchesRule(pr, rule) {
			t.Error("PR from excluded author should not match")
		}
	})

	t.Run("PR excluded by label", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "alice",
			Labels:    []string{"auto-merge", "do-not-merge"},
		}
		rule := &models.AutoMergeRule{
			Enabled:       true,
			Labels:        []string{"auto-merge"},
			ExcludeLabels: []string{"do-not-merge"},
		}
		if e.matchesRule(pr, rule) {
			t.Error("PR with excluded label should not match")
		}
	})

	t.Run("PR missing required label", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "alice",
			Labels:    []string{"bug"},
		}
		rule := &models.AutoMergeRule{
			Enabled: true,
			Labels:  []string{"auto-merge"},
		}
		if e.matchesRule(pr, rule) {
			t.Error("PR without required label should not match")
		}
	})

	t.Run("multiple labels any match", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "alice",
			Labels:    []string{"ready"},
		}
		rule := &models.AutoMergeRule{
			Enabled: true,
			Labels:  []string{"auto-merge", "ready", "lgtm"},
		}
		if !e.matchesRule(pr, rule) {
			t.Error("PR with any matching label should match")
		}
	})

	t.Run("CIRequired with conflict", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup:   "test-group",
			Author:      "alice",
			Labels:      []string{"auto-merge"},
			HasConflict: true,
		}
		rule := &models.AutoMergeRule{
			Enabled:    true,
			Labels:     []string{"auto-merge"},
			CIRequired: true,
		}
		if e.matchesRule(pr, rule) {
			t.Error("PR with conflict should not match when CIRequired")
		}
	})

	t.Run("CIRequired without conflict", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup:   "test-group",
			Author:      "alice",
			Labels:      []string{"auto-merge"},
			HasConflict: false,
		}
		rule := &models.AutoMergeRule{
			Enabled:    true,
			Labels:     []string{"auto-merge"},
			CIRequired: true,
		}
		if !e.matchesRule(pr, rule) {
			t.Error("PR without conflict should match when CIRequired")
		}
	})

	t.Run("RequiredApprovals with IsApproved false", func(t *testing.T) {
		pr := &models.PRRecord{
			RepoGroup: "test-group",
			Author:    "alice",
			Labels:    []string{"auto-merge"},
			IsApproved: false,
		}
		rule := &models.AutoMergeRule{
			Enabled:           true,
			Labels:            []string{"auto-merge"},
			RequiredApprovals: 1,
		}
		if e.matchesRule(pr, rule) {
			t.Error("PR not approved should not match when RequiredApprovals > 0")
		}
	})
}

func TestEvaluatePR(t *testing.T) {
	t.Run("AutoMerge disabled returns early", func(t *testing.T) {
		cfg := &models.Config{
			AutoMerge: models.AutoMergeConfig{Enabled: false},
		}
		e := NewEvaluator(cfg, nil)
		pr := &models.PRRecord{
			State:     "open",
			IsDraft:   false,
			SpamFlag:  false,
			Labels:    []string{"auto-merge"},
		}
		e.EvaluatePR(pr)
	})

	t.Run("nil PR does not panic", func(t *testing.T) {
		cfg := &models.Config{
			AutoMerge: models.AutoMergeConfig{Enabled: true},
		}
		e := NewEvaluator(cfg, nil)
		e.EvaluatePR(nil)
	})

	t.Run("closed PR is skipped", func(t *testing.T) {
		cfg := &models.Config{
			AutoMerge: models.AutoMergeConfig{
				Enabled: true,
				Rules: []models.AutoMergeRule{
					{Enabled: true, Labels: []string{"auto-merge"}},
				},
			},
		}
		e := NewEvaluator(cfg, nil)
		pr := &models.PRRecord{
			State:    "closed",
			IsDraft:  false,
			SpamFlag: false,
			Labels:   []string{"auto-merge"},
		}
		e.EvaluatePR(pr)
	})

	t.Run("draft PR is skipped", func(t *testing.T) {
		cfg := &models.Config{
			AutoMerge: models.AutoMergeConfig{
				Enabled: true,
				Rules: []models.AutoMergeRule{
					{Enabled: true, Labels: []string{"auto-merge"}},
				},
			},
		}
		e := NewEvaluator(cfg, nil)
		pr := &models.PRRecord{
			State:    "open",
			IsDraft:  true,
			SpamFlag: false,
			Labels:   []string{"auto-merge"},
		}
		e.EvaluatePR(pr)
	})

	t.Run("spam PR is skipped", func(t *testing.T) {
		cfg := &models.Config{
			AutoMerge: models.AutoMergeConfig{
				Enabled: true,
				Rules: []models.AutoMergeRule{
					{Enabled: true, Labels: []string{"auto-merge"}},
				},
			},
		}
		e := NewEvaluator(cfg, nil)
		pr := &models.PRRecord{
			State:    "open",
			IsDraft:  false,
			SpamFlag: true,
			Labels:   []string{"auto-merge"},
		}
		e.EvaluatePR(pr)
	})
}
