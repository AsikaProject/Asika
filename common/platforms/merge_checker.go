package platforms

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"asika/common/config"
	"asika/common/models"
)

// CheckMergeMethods checks if all repositories have consistent merge methods
func CheckMergeMethods(cfg *models.Config, clients map[PlatformType]PlatformClient) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	repos := config.GetRepoGroups(cfg)
	for _, repo := range repos {
		if err := checkRepoMergeMethod(cfg, clients, repo); err != nil {
			return err
		}
	}

	slog.Info("merge method check passed")
	return nil
}

// checkRepoMergeMethod checks a single repository group
func checkRepoMergeMethod(cfg *models.Config, clients map[PlatformType]PlatformClient, repo models.RepoGroup) error {
	ctx := context.Background()

	// Check GitHub
	if repo.GitHub != "" && clients[PlatformGitHub] != nil {
		if err := checkPlatformMergeMethod(ctx, clients[PlatformGitHub], "github", repo.GitHub, repo.DefaultBranch); err != nil {
			return err
		}
	}

	// Check GitLab
	if repo.GitLab != "" && clients[PlatformGitLab] != nil {
		if err := checkPlatformMergeMethod(ctx, clients[PlatformGitLab], "gitlab", repo.GitLab, repo.DefaultBranch); err != nil {
			return err
		}
	}

	// Check Gitea
	if repo.Gitea != "" && clients[PlatformGitea] != nil {
		if err := checkPlatformMergeMethod(ctx, clients[PlatformGitea], "gitea", repo.Gitea, repo.DefaultBranch); err != nil {
			return err
		}
	}

	// Check Forgejo
	if repo.Forgejo != "" && clients[PlatformForgejo] != nil {
		if err := checkPlatformMergeMethod(ctx, clients[PlatformForgejo], "forgejo", repo.Forgejo, repo.DefaultBranch); err != nil {
			return err
		}
	}

	// Check Codeberg
	if repo.Codeberg != "" && clients[PlatformCodeberg] != nil {
		if err := checkPlatformMergeMethod(ctx, clients[PlatformCodeberg], "codeberg", repo.Codeberg, repo.DefaultBranch); err != nil {
			return err
		}
	}

	// Check Bitbucket
	if repo.Bitbucket != "" && clients[PlatformBitbucket] != nil {
		if err := checkPlatformMergeMethod(ctx, clients[PlatformBitbucket], "bitbucket", repo.Bitbucket, repo.DefaultBranch); err != nil {
			return err
		}
	}

	return nil
}

// checkPlatformMergeMethod checks a single platform repository
func checkPlatformMergeMethod(ctx context.Context, client PlatformClient, platform, repo, branch string) error {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format: %s", repo)
	}

	hasMultiple, err := client.HasMultipleMergeMethods(ctx, parts[0], parts[1])
	if err != nil {
		slog.Warn("cannot check merge methods, skipping", "platform", platform, "repo", repo, "error", err)
		return nil
	}

	if hasMultiple {
		defaultMethod, err := client.GetDefaultMergeMethod(ctx, parts[0], parts[1])
		if err != nil || defaultMethod == "" {
			return fmt.Errorf("repository %s has multiple merge methods but cannot determine default: %v", repo, err)
		}
		slog.Info("repository has multiple merge methods, using default",
			"platform", platform,
			"repo", repo,
			"default", defaultMethod)
	}

	return nil
}

// ExitOnCheckFailed exits if merge method check fails
func ExitOnCheckFailed(err error) {
	if err != nil {
		slog.Error("FATAL: merge method check failed", "error", err)
		os.Exit(1)
	}
}