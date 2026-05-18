package core

import (
	"context"
	"encoding/json"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/common/platforms"
)

// MigrateRepoGroupNames updates old DB records when repo group name changed (e.g. "main" -> "default").
func MigrateRepoGroupNames(cfg *models.Config) {
	if len(cfg.RepoGroups) == 0 {
		return
	}
	currentName := cfg.RepoGroups[0].Name

	validNames := make(map[string]bool)
	for _, rg := range cfg.RepoGroups {
		validNames[rg.Name] = true
	}

	// Migrate PR records
	var prKeysToDelete []string
	var prsToReinsert []struct {
		key    string
		value  []byte
		pr     models.PRRecord
		newKey string
	}
	_ = db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if !validNames[pr.RepoGroup] {
			pr.RepoGroup = currentName
			newKey := currentName + "#" + pr.ID
			updated, _ := json.Marshal(pr)
			prsToReinsert = append(prsToReinsert, struct {
				key    string
				value  []byte
				pr     models.PRRecord
				newKey string
			}{string(key), updated, pr, newKey})
			prKeysToDelete = append(prKeysToDelete, string(key))
		}
		return nil
	})
	for _, item := range prsToReinsert {
		if err := db.PutPRWithIndex(item.newKey, item.value, item.pr.ID, item.pr.RepoGroup, item.pr.PRNumber); err != nil {
			slog.Error("repo group migration: failed to reinsert PR", "pr_id", item.pr.ID, "error", err)
		}
	}
	for _, k := range prKeysToDelete {
		if err := db.Delete(db.BucketPRs, k); err != nil {
			slog.Error("repo group migration: failed to delete old PR key", "key", k, "error", err)
		}
	}

	// Migrate queue items
	var qiKeysToDelete []string
	var qisToReinsert []struct {
		key    string
		value  []byte
		newKey string
	}
	_ = db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if json.Unmarshal(value, &item) != nil {
			return nil
		}
		if !validNames[item.RepoGroup] {
			item.RepoGroup = currentName
			newKey := currentName + "#" + item.PRID
			updated, _ := json.Marshal(item)
			qisToReinsert = append(qisToReinsert, struct {
				key    string
				value  []byte
				newKey string
			}{string(key), updated, newKey})
			qiKeysToDelete = append(qiKeysToDelete, string(key))
		}
		return nil
	})
	for _, item := range qisToReinsert {
		if err := db.Put(db.BucketQueueItems, item.newKey, item.value); err != nil {
			slog.Error("repo group migration: failed to reinsert queue item", "key", item.newKey, "error", err)
		}
	}
	for _, k := range qiKeysToDelete {
		if err := db.Delete(db.BucketQueueItems, k); err != nil {
			slog.Error("repo group migration: failed to delete old queue key", "key", k, "error", err)
		}
	}

	if len(prsToReinsert)+len(qisToReinsert) > 0 {
		slog.Info("repo group migration complete", "prs_migrated", len(prsToReinsert), "queue_items_migrated", len(qisToReinsert))
	}
}

// MigratePRStates fixes historical PR records: closed PRs with MergedAt set should be "merged".
func MigratePRStates(cfg *models.Config) {
	var keysToUpdate []struct {
		key   string
		value []byte
	}
	_ = db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if pr.State == "closed" && !pr.MergedAt.IsZero() {
			pr.State = "merged"
			data, _ := json.Marshal(pr)
			keysToUpdate = append(keysToUpdate, struct {
				key   string
				value []byte
			}{string(key), data})
		}
		return nil
	})
	for _, item := range keysToUpdate {
		if err := db.Put(db.BucketPRs, item.key, item.value); err != nil {
			slog.Error("PR state migration: failed to update PR", "key", item.key, "error", err)
		}
	}
	if len(keysToUpdate) > 0 {
		slog.Info("PR state migration complete", "merged_fixed", len(keysToUpdate))
	}
}

// SyncPRStates refreshes PR state from platforms for open/closed PRs in local DB.
func SyncPRStates(cfg *models.Config, clients map[platforms.PlatformType]platforms.PlatformClient) {
	if len(clients) == 0 {
		return
	}

	type prUpdate struct {
		key  string
		data []byte
		pr   models.PRRecord
	}

	ctx := context.Background()
	var updates []prUpdate

	_ = db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if pr.PRNumber == 0 || pr.Platform == "" {
			return nil
		}
		if pr.State == "merged" || pr.State == "closed" {
			return nil
		}

		group := config.GetRepoGroupByName(cfg, pr.RepoGroup)
		if group == nil {
			return nil
		}

		platType := platforms.PlatformType(pr.Platform)
		client, ok := clients[platType]
		if !ok {
			return nil
		}

		owner, repo := config.GetOwnerRepoFromGroup(group, pr.Platform)
		updated, err := client.GetPR(ctx, owner, repo, pr.PRNumber)
		if err != nil || updated == nil {
			return nil
		}

		if updated.State != pr.State {
			pr.State = updated.State
		}
		if updated.MergeCommitSHA != "" && pr.MergeCommitSHA != updated.MergeCommitSHA {
			pr.MergeCommitSHA = updated.MergeCommitSHA
		}
		pr.IsApproved = updated.IsApproved
		pr.IsDraft = updated.IsDraft
		pr.HasConflict = updated.HasConflict
		pr.HTMLURL = updated.HTMLURL
		pr.Labels = updated.Labels
		pr.UpdatedAt = updated.UpdatedAt

		data, _ := json.Marshal(pr)
		updates = append(updates, prUpdate{string(key), data, pr})
		return nil
	})

	for _, u := range updates {
		if err := db.PutPRWithIndex(u.key, u.data, u.pr.ID, u.pr.RepoGroup, u.pr.PRNumber); err != nil {
			slog.Error("PR state sync: failed to update PR", "pr_id", u.pr.ID, "error", err)
		}
	}

	if len(updates) > 0 {
		slog.Info("PR state sync complete", "updated", len(updates))
	}
}
