package workflow

import (
	"context"
	"fmt"
	"log/slog"

	"asika/common/models"
	"asika/common/platforms"
)

type Executor struct {
	client platforms.PlatformClient
	info   *PlatformInfo
}

func NewExecutor(client platforms.PlatformClient, info *PlatformInfo) *Executor {
	return &Executor{
		client: client,
		info:   info,
	}
}

func (e *Executor) ExecuteLabels(ctx context.Context, rules []models.WorkflowLabelRule, prCtx *PRContext) error {
	eval := &Evaluator{client: e.client, info: e.info}

	for _, rule := range rules {
		shouldApply, err := eval.EvaluateCondition(rule.Condition, prCtx)
		if err != nil {
			slog.Warn("failed to evaluate label condition", "condition", rule.Condition, "error", err)
			continue
		}

		if !shouldApply {
			continue
		}

		if rule.Action == "add" {
			slog.Info("adding label", "label", rule.Label, "pr", e.info.PRNumber)
			if err := e.client.AddLabel(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber, rule.Label, ""); err != nil {
				slog.Warn("failed to add label", "label", rule.Label, "error", err)
			}
		} else if rule.Action == "remove" {
			slog.Info("removing label", "label", rule.Label, "pr", e.info.PRNumber)
			if err := e.client.RemoveLabel(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber, rule.Label); err != nil {
				slog.Warn("failed to remove label", "label", rule.Label, "error", err)
			}
		}
	}

	return nil
}

func (e *Executor) ExecuteMerge(ctx context.Context, rule models.WorkflowMergeRule, prCtx *PRContext) error {
	if !rule.Enabled {
		return nil
	}

	eval := &Evaluator{client: e.client, info: e.info}
	shouldMerge, err := eval.EvaluateCondition(rule.Condition, prCtx)
	if err != nil {
		return fmt.Errorf("failed to evaluate merge condition: %w", err)
	}

	if !shouldMerge {
		slog.Info("merge condition not met", "condition", rule.Condition)
		return nil
	}

	if !rule.AutoMerge {
		slog.Info("auto_merge is disabled, skipping merge")
		return nil
	}

	method := rule.MergeMethod
	if method == "" {
		method = "merge"
	}

	slog.Info("merging PR", "pr", e.info.PRNumber, "method", method)
	if err := e.client.MergePR(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber, method); err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}

	if rule.DeleteBranch && e.info.HeadBranch != "" {
		slog.Info("deleting branch", "branch", e.info.HeadBranch)
		if err := e.client.DeleteBranch(ctx, e.info.RepoOwner, e.info.RepoName, e.info.HeadBranch); err != nil {
			slog.Warn("failed to delete branch", "branch", e.info.HeadBranch, "error", err)
		}
	}

	return nil
}

func (e *Executor) ExecuteClose(ctx context.Context, rule models.WorkflowCloseRule, prCtx *PRContext) error {
	if !rule.Enabled {
		return nil
	}

	eval := &Evaluator{client: e.client, info: e.info}
	shouldClose, err := eval.EvaluateCondition(rule.Condition, prCtx)
	if err != nil {
		return fmt.Errorf("failed to evaluate close condition: %w", err)
	}

	if !shouldClose {
		slog.Info("close condition not met", "condition", rule.Condition)
		return nil
	}

	if rule.AddLabel != "" {
		slog.Info("adding label before closing", "label", rule.AddLabel)
		if err := e.client.AddLabel(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber, rule.AddLabel, ""); err != nil {
			slog.Warn("failed to add label", "label", rule.AddLabel, "error", err)
		}
	}

	if rule.Comment != "" {
		slog.Info("adding comment before closing")
		if err := e.client.CommentPR(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber, rule.Comment); err != nil {
			slog.Warn("failed to add comment", "error", err)
		}
	}

	slog.Info("closing PR", "pr", e.info.PRNumber)
	if err := e.client.ClosePR(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber); err != nil {
		return fmt.Errorf("failed to close PR: %w", err)
	}

	return nil
}
