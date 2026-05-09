package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/db"
	"asika/common/models"
)

// GetSyncHistory handles GET /api/v1/sync/history (8.5)
func GetSyncHistory(c *gin.Context) {
	var history []models.SyncRecord
	err := db.ForEach(db.BucketSyncHistory, func(key, value []byte) error {
		var record models.SyncRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return nil
		}
		history = append(history, record)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read sync history"})
		return
	}

	c.JSON(http.StatusOK, history)
}

// RetrySync handles POST /api/v1/sync/retry/:sync_id (8.5)
func RetrySync(c *gin.Context) {
	syncID := c.Param("sync_id")

	data, err := db.Get(db.BucketSyncHistory, syncID)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sync record not found"})
		return
	}

	var record models.SyncRecord
	if err := json.Unmarshal(data, &record); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse sync record"})
		return
	}

	if record.Status != "failed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only failed syncs can be retried"})
		return
	}

	if record.PRID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sync record missing PR ID"})
		return
	}

	if syncerRef == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "syncer not initialized"})
		return
	}

	pr := findPRInDB(record.RepoGroup, record.PRID)
	if pr == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found for retry"})
		return
	}

	slog.Info("retrying sync", "sync_id", syncID, "pr_id", record.PRID, "target", record.TargetPlatform)

	// Update the sync record status
	data, _ = json.Marshal(record)
	db.Put(db.BucketSyncHistory, syncID, data)

	// Trigger the sync
	go func() {
		ctx := context.Background()
		if err := syncerRef.SyncOnMerge(ctx, pr); err != nil {
			slog.Error("retry sync failed", "sync_id", syncID, "error", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "retry triggered for sync " + syncID})
}

// findPRInDB finds a PR by repo group and PR ID in bbolt
func findPRInDB(repoGroup, prID string) *models.PRRecord {
	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && pr.ID == prID {
			found = &pr
		}
		return nil
	})
	return found
}
