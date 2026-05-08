package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/queue"
)

// queueMgr is a package-level variable to access the queue manager
var queueMgr *queue.Manager

// InitQueueMgr initializes the queue manager for handlers
func InitQueueMgr(mgr *queue.Manager) {
	queueMgr = mgr
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

	// Get queue items from DB - match by item.RepoGroup field or legacy key prefix
	err := db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if item.RepoGroup == repoGroup || strings.HasPrefix(string(key), repoGroup+"#") {
			items = append(items, item)
		}
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

	if queueMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "queue manager not initialized"})
		return
	}

	go queueMgr.CheckQueue()
	slog.Info("queue recheck triggered", "repo_group", repoGroup)

	c.JSON(http.StatusOK, gin.H{"message": "queue recheck triggered"})
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

	if queueMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "queue manager not initialized"})
		return
	}

	count, err := queueMgr.ClearQueue(repoGroup)
	if err != nil {
		slog.Error("failed to clear queue", "repo_group", repoGroup, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear queue"})
		return
	}

	slog.Info("queue cleared via API", "repo_group", repoGroup, "count", count)
	c.JSON(http.StatusOK, gin.H{"message": "queue cleared", "count": count})
}
