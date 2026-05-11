package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
)

// ScheduleMerge handles POST /api/v1/repos/:repo_group/prs/:pr_id/schedule-merge
func ScheduleMerge(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var req struct {
		ScheduleAt string `json:"schedule_at"` // RFC3339, e.g. "2026-05-11T14:00:00+08:00"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.ScheduleAt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schedule_at is required"})
		return
	}

	scheduleAt, err := time.Parse(time.RFC3339, req.ScheduleAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule_at: must be RFC3339 format"})
		return
	}

	if scheduleAt.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schedule_at must be in the future"})
		return
	}

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	var found *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if json.Unmarshal(value, &pr) != nil {
			return nil
		}
		if pr.RepoGroup == repoGroup && (pr.ID == prID || fmt.Sprintf("%d", pr.PRNumber) == prID) {
			found = &pr
		}
		return nil
	})

	if found == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	if err := pr.AddToQueueScheduled(found, scheduleAt); err != nil {
		slog.Error("failed to schedule merge", "repo_group", repoGroup, "pr_id", prID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to schedule merge"})
		return
	}

	slog.Info("merge scheduled", "repo_group", repoGroup, "pr_id", prID, "schedule_at", scheduleAt)
	c.JSON(http.StatusOK, gin.H{
		"message":     "merge scheduled",
		"pr_id":       prID,
		"schedule_at": scheduleAt.Format(time.RFC3339),
	})
}

// GetQueue handles GET /api/v1/queue/:repo_group (8.3)
func GetQueue(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	items := make([]models.QueueItem, 0)

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusOK, items)
		return
	}

	// Get queue items from DB by prefix scan
	prefix := repoGroup + "#"
	err := db.BucketForEachPrefix(db.BucketQueueItems, prefix, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusOK, items)
		return
	}

	c.JSON(http.StatusOK, items)
}

// RecheckQueue handles POST /api/v1/queue/:repo_group/recheck (8.3)
func RecheckQueue(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusOK, gin.H{"message": "no repo group configured"})
		return
	}

	pr.RecheckQueue()
	slog.Info("queue recheck triggered", "repo_group", repoGroup)

	c.JSON(http.StatusOK, gin.H{"message": "queue recheck triggered"})
}

// RemoveFromQueue handles DELETE /api/v1/queue/:repo_group/:pr_id (8.3)
func RemoveFromQueue(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	if prID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_id is required"})
		return
	}

	if err := pr.RemoveFromQueue(repoGroup, prID); err != nil {
		slog.Error("failed to remove queue item", "repo_group", repoGroup, "pr_id", prID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	slog.Info("queue item removed via API", "repo_group", repoGroup, "pr_id", prID)
	c.JSON(http.StatusOK, gin.H{"message": "queue item removed"})
}

// ClearQueue handles DELETE /api/v1/queue/:repo_group (8.3)
func ClearQueue(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	count, err := pr.ClearQueue(repoGroup)
	if err != nil {
		slog.Error("failed to clear queue", "repo_group", repoGroup, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear queue"})
		return
	}

	slog.Info("queue cleared via API", "repo_group", repoGroup, "count", count)
	c.JSON(http.StatusOK, gin.H{"message": "queue cleared", "count": count})
}
