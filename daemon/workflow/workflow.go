package workflow

import (
	"context"
	"fmt"
	"log/slog"

	"asika/common/platforms"
)

func Run(workDir string) error {
	ctx := context.Background()

	slog.Info("workflow mode started")

	info, err := DetectPlatform()
	if err != nil {
		return fmt.Errorf("platform detection failed: %w", err)
	}
	slog.Info("detected platform", "platform", info.Platform, "pr", info.PRNumber, "repo", info.RepoFull)

	cfg, err := LoadConfig(workDir)
	if err != nil {
		return fmt.Errorf("config loading failed: %w", err)
	}

	if !cfg.Workflow.Enabled {
		slog.Info("workflow is disabled in config, exiting")
		return nil
	}

	client, err := initPlatformClient(info)
	if err != nil {
		return fmt.Errorf("platform client initialization failed: %w", err)
	}

	evaluator := NewEvaluator(client, info)
	executor := NewExecutor(client, info)

	slog.Info("fetching PR context")
	prCtx, err := evaluator.GetPRContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get PR context: %w", err)
	}

	slog.Info("pr context",
		"ci_passed", prCtx.CIPassed,
		"has_conflicts", prCtx.HasConflicts,
		"approved", prCtx.IsApproved,
		"draft", prCtx.IsDraft,
		"labels", prCtx.Labels)

	slog.Info("executing label operations", "count", len(cfg.Workflow.Labels))
	if err := executor.ExecuteLabels(ctx, cfg.Workflow.Labels, prCtx); err != nil {
		slog.Warn("label operations failed", "error", err)
	}

	if cfg.Workflow.Merge.Enabled {
		slog.Info("executing merge operation")
		if err := executor.ExecuteMerge(ctx, cfg.Workflow.Merge, prCtx); err != nil {
			slog.Error("merge operation failed", "error", err)
			return err
		}
	}

	if cfg.Workflow.Close.Enabled {
		slog.Info("executing close operation")
		if err := executor.ExecuteClose(ctx, cfg.Workflow.Close, prCtx); err != nil {
			slog.Error("close operation failed", "error", err)
			return err
		}
	}

	slog.Info("workflow execution completed successfully")
	return nil
}

func initPlatformClient(info *PlatformInfo) (platforms.PlatformClient, error) {
	switch info.Platform {
	case "github":
		return platforms.NewGitHubClient(info.Token, "", info.BaseURL), nil
	case "gitlab":
		return platforms.NewGitLabClient(info.Token, info.BaseURL, ""), nil
	case "gitea":
		return platforms.NewGiteaClient(info.BaseURL, info.Token, ""), nil
	case "forgejo":
		return platforms.NewGiteaClient(info.BaseURL, info.Token, ""), nil
	case "bitbucket":
		return platforms.NewBitbucketClient(info.Token, ""), nil
	case "gerrit":
		username := ""
		return platforms.NewGerritClient(info.BaseURL, username, info.Token, ""), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", info.Platform)
	}
}
