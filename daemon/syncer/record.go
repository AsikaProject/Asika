package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
)

// SyncBranchDeletion syncs branch deletion to all configured target platforms with retry.
func (s *Syncer) SyncBranchDeletion(repoGroup, sourcePlatform, branch string) {
	group := config.GetRepoGroupByName(s.cfg, repoGroup)
	if group == nil || group.Mode != "multi" {
		return
	}

	ctx := context.Background()
	targets := s.getTargetPlatforms(group, sourcePlatform)

	for _, target := range targets {
		client := s.clients[platforms.PlatformType(target.name)]
		if client == nil {
			continue
		}

		owner, repo := config.GetOwnerRepoFromGroup(group, target.name)
		if owner == "" || repo == "" {
			continue
		}

		var lastErr error
		for attempt := 0; attempt < syncMaxRetries; attempt++ {
			if attempt > 0 {
				delay := syncRetryBaseDelay * time.Duration(1<<uint(attempt-1))
				time.Sleep(delay)
			}
			if err := client.DeleteBranch(ctx, owner, repo, branch); err != nil {
				lastErr = err
				if isTransientError(err) {
					slog.Warn("transient error deleting branch, retrying",
						"platform", target.name, "branch", branch, "attempt", attempt+1)
					continue
				}
				break
			}
			slog.Info("branch deleted", "platform", target.name, "branch", branch)
			return
		}
		if lastErr != nil {
			slog.Error("failed to delete branch", "platform", target.name, "branch", branch, "error", lastErr)
		}
	}
}

// notifySyncFailure sends a notification about sync failure.
func (s *Syncer) notifySyncFailure(pr *models.PRRecord, targetPlatforms, reason string) {
	if s.notifyFn == nil {
		return
	}
	title := fmt.Sprintf("⚠️ Sync Failed: %s#%d", pr.RepoGroup, pr.PRNumber)
	body := fmt.Sprintf("PR: %s\nSource: %s\nTarget: %s\nReason: %s", pr.Title, pr.Platform, targetPlatforms, reason)
	s.notifyFn(title, body)
}

// recordSync records sync history in bbolt.
// Returns error on failure; callers should log and continue.
func (s *Syncer) recordSync(pr *models.PRRecord, branch, targetPlatform, status, errorMsg string) error {
	record := models.SyncRecord{
		ID:             uuid.New().String(),
		PRID:           pr.ID,
		RepoGroup:      pr.RepoGroup,
		SourcePlatform: pr.Platform,
		TargetPlatform: targetPlatform,
		Branch:         branch,
		CommitSHA:      pr.MergeCommitSHA,
		Status:         status,
		ErrorMessage:   errorMsg,
		Timestamp:      time.Now(),
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal sync record: %w", err)
	}
	if err := db.Put(db.BucketSyncHistory, record.ID, data); err != nil {
		return fmt.Errorf("store sync record: %w", err)
	}
	return nil
}
