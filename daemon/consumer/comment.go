package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"asika/common/config"
	"asika/common/events"
	"asika/common/models"
	"asika/common/platforms"
)

func (c *Consumer) handlePRComment(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	payload, ok := event.Payload.(*models.PRCommentPayload)
	if !ok || payload == nil {
		slog.Warn("pr_comment event missing payload", "repo_group", event.RepoGroup, "pr", pr.PRNumber)
		return
	}

	commentBody := strings.TrimSpace(payload.CommentBody)
	commentAuthor := payload.CommentAuthor

	slog.Info("PR comment received", "repo_group", event.RepoGroup, "pr", pr.PRNumber, "author", commentAuthor, "body", commentBody)

	if !strings.HasPrefix(commentBody, "/") {
		return
	}

	group := config.GetRepoGroupByName(c.cfg, pr.RepoGroup)
	if group == nil {
		slog.Warn("repo group not found for comment command", "repo_group", pr.RepoGroup)
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
	if owner == "" || repo == "" {
		slog.Warn("cannot resolve repo for comment command", "platform", pr.Platform, "repo_group", pr.RepoGroup)
		return
	}

	client := c.clients[platforms.PlatformType(pr.Platform)]
	if client == nil {
		slog.Warn("no client for platform", "platform", pr.Platform)
		return
	}

	parts := strings.Fields(commentBody)
	if len(parts) == 0 {
		return
	}
	command := strings.ToLower(parts[0])
	args := parts[1:]

	ctx := context.Background()
	var result string

	switch command {
	case "/approve":
		result = c.cmdApprove(ctx, client, pr, owner, repo, commentAuthor)
	case "/close":
		result = c.cmdClose(ctx, client, pr, owner, repo, commentAuthor)
	case "/reopen":
		result = c.cmdReopen(ctx, client, pr, owner, repo, commentAuthor)
	case "/merge":
		result = c.cmdMerge(ctx, client, pr, owner, repo, group, commentAuthor)
	case "/spam":
		result = c.cmdSpam(ctx, client, pr, owner, repo, commentAuthor)
	case "/rebase":
		result = c.cmdRebase(ctx, client, pr, owner, repo, commentAuthor)
	case "/cherry-pick":
		result = c.cmdCherryPick(ctx, client, pr, owner, repo, args, commentAuthor)
	case "/queue":
		result = c.cmdQueue(pr, commentAuthor)
	case "/recheck":
		result = c.cmdRecheck(pr, commentAuthor)
	case "/help":
		result = "Available commands: /approve, /close, /reopen, /merge, /spam, /rebase, /cherry-pick, /queue, /recheck, /help"
	default:
		slog.Info("unknown comment command", "command", command, "pr", pr.PRNumber)
		return
	}

	if result != "" {
		reply := fmt.Sprintf("@%s %s", commentAuthor, result)
		if err := client.CommentPR(ctx, owner, repo, pr.PRNumber, reply); err != nil {
			slog.Error("failed to post command result comment", "error", err, "pr", pr.PRNumber)
		}
	}
}

func (c *Consumer) cmdApprove(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if err := client.ApprovePR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to approve: %v", err)
	}
	return fmt.Sprintf("PR #%d approved by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdClose(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to close: %v", err)
	}
	return fmt.Sprintf("PR #%d closed by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdReopen(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if err := client.ReopenPR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to reopen: %v", err)
	}
	return fmt.Sprintf("PR #%d reopened by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdMerge(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo string, group *models.RepoGroup, author string) string {
	method, err := client.GetDefaultMergeMethod(ctx, owner, repo)
	if err != nil {
		method = "merge"
	}
	if err := client.MergePR(ctx, owner, repo, pr.PRNumber, method); err != nil {
		return fmt.Sprintf("Failed to merge: %v", err)
	}
	return fmt.Sprintf("PR #%d merged by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdSpam(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	if c.spamDetector != nil {
		c.spamDetector.HandleSpam(pr, pr.RepoGroup)
	}
	if err := client.ClosePR(ctx, owner, repo, pr.PRNumber); err != nil {
		return fmt.Sprintf("Failed to mark as spam: %v", err)
	}
	return fmt.Sprintf("PR #%d marked as spam and closed by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdRebase(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo, author string) string {
	return "Rebase via comment is not yet supported. Please use the web UI or bot commands."
}

func (c *Consumer) cmdCherryPick(ctx context.Context, client platforms.PlatformClient, pr *models.PRRecord, owner, repo string, args []string, author string) string {
	if len(args) == 0 {
		return "Usage: /cherry-pick <target-branch>"
	}
	targetBranch := args[0]
	_ = targetBranch
	return fmt.Sprintf("Cherry-pick via comment is not yet supported. Please use the web UI or bot commands.")
}

func (c *Consumer) cmdQueue(pr *models.PRRecord, author string) string {
	if c.queue != nil {
		if err := c.queue.AddToQueue(pr); err != nil {
			return fmt.Sprintf("Failed to add to queue: %v", err)
		}
	}
	return fmt.Sprintf("PR #%d added to merge queue by %s via comment.", pr.PRNumber, author)
}

func (c *Consumer) cmdRecheck(pr *models.PRRecord, author string) string {
	if c.queue != nil {
		c.queue.CheckQueue()
	}
	return fmt.Sprintf("Queue recheck triggered by %s via comment.", author)
}
