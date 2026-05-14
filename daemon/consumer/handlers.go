package consumer

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"asika/common/events"
	"asika/common/models"
	"asika/daemon/handlers"
)

func (c *Consumer) handlePROpened(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR opened", "title", pr.Title, "author", pr.Author)

	if pr.ID == "" {
		pr.ID = uuid.New().String()
	}
	pr.CreatedAt = time.Now()
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "opened",
		Actor:     pr.Author,
	})
	c.updatePR(event, pr)

	if c.labeler != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("labeler panic recovered", "error", r, "pr_number", pr.PRNumber, "repo_group", event.RepoGroup)
				}
			}()
			c.labeler.HandlePROpened(pr, event.RepoGroup)
		}()
	}
	if c.reviewer != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("reviewer panic recovered", "error", r, "pr_number", pr.PRNumber, "repo_group", event.RepoGroup)
				}
			}()
			c.reviewer.HandlePROpened(pr, event.RepoGroup)
		}()
	}
	if c.staleMgr != nil {
		c.staleMgr.HandleActivity(pr, event.RepoGroup)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("syncPRLinks panic recovered", "error", r, "pr_number", pr.PRNumber)
			}
		}()
		syncPRLinks(c.writer, pr)
	}()
}

func (c *Consumer) handlePRClosed(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR closed", "title", pr.Title)

	pr.State = "closed"
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "closed",
		Actor:     "system",
	})
	c.updatePR(event, pr)
}

func (c *Consumer) handlePRMerged(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR merged", "title", pr.Title)

	pr.State = "merged"
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "merged",
		Actor:     "system",
	})
	c.updatePR(event, pr)

	if c.syncer != nil {
		go func() {
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Minute)
			defer cancel()
			if err := c.syncer.SyncOnMerge(ctx, pr); err != nil {
				slog.Error("sync failed", "error", err, "repo_group", event.RepoGroup)
			}
		}()
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("NotifyCrossSpaceDeps panic recovered", "error", r, "pr_number", pr.PRNumber)
			}
		}()
		handlers.NotifyCrossSpaceDeps(pr)
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("UpdateStackMemberStateOnMerge panic recovered", "error", r, "pr_number", pr.PRNumber)
			}
		}()
		handlers.UpdateStackMemberStateOnMerge(pr)
	}()
}

func (c *Consumer) handlePRApproved(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR approved", "title", pr.Title)

	if c.queue != nil {
		if err := c.queue.AddToQueue(pr); err != nil {
			slog.Error("failed to add PR to queue", "error", err, "pr_id", pr.ID)
		} else {
			slog.Info("PR added to merge queue", "pr_id", pr.ID, "repo_group", pr.RepoGroup)
		}
	}
}

func (c *Consumer) handleSpamDetected(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Warn("spam detected", "title", pr.Title, "author", pr.Author)

	pr.SpamFlag = true
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "marked_spam",
		Actor:     "system",
	})
	c.updatePR(event, pr)

	if c.spamDetector != nil {
		go func() {
			ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
			defer cancel()
			c.spamDetector.HandleSpamWithContext(ctx, pr, event.RepoGroup)
		}()
	}
}

func (c *Consumer) handlePRLabeled(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR labeled", "repo_group", event.RepoGroup, "pr_number", pr.PRNumber)

	if label, ok := event.Payload.(string); ok && label != "" {
		found := false
		for _, l := range pr.Labels {
			if l == label {
				found = true
				break
			}
		}
		if !found {
			pr.Labels = append(pr.Labels, label)
		}
	}

	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "labeled",
		Actor:     "system",
	})
	c.updatePR(event, pr)
}

func (c *Consumer) handlePRReopened(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}

	slog.Info("PR reopened (spam recovery)", "title", pr.Title, "repo_group", pr.RepoGroup)

	pr.State = "open"
	pr.SpamFlag = false
	pr.UpdatedAt = time.Now()
	pr.Events = append(pr.Events, models.PREvent{
		Timestamp: time.Now(),
		Action:    "reopened",
		Actor:     "system",
	})
	c.updatePR(event, pr)

	if c.staleMgr != nil {
		c.staleMgr.HandleActivity(pr, event.RepoGroup)
	}

	if c.syncer != nil {
		go func() {
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Minute)
			defer cancel()
			if err := c.syncer.SyncOnMerge(ctx, pr); err != nil {
				slog.Error("failed to sync spam-reopened PR", "error", err, "pr_id", pr.ID)
			}
		}()
	}
}

func (c *Consumer) handlePRReverted(event events.Event) {
	pr := event.PR
	if pr == nil {
		return
	}
	slog.Info("PR reverted", "title", pr.Title, "pr_number", pr.PRNumber, "repo_group", event.RepoGroup)
}

func (c *Consumer) handleBranchDeleted(event events.Event) {
	branch, ok := event.Payload.(string)
	if !ok || branch == "" {
		slog.Warn("branch deleted event missing branch name")
		return
	}

	slog.Info("branch deleted", "branch", branch, "repo_group", event.RepoGroup)

	if c.syncer != nil {
		go c.syncer.SyncBranchDeletion(event.RepoGroup, event.Platform, branch)
	}
}
