package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/db"
	"asika/common/models"
)

func SetQueuePriority(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var req struct {
		Priority int `json:"priority"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Priority < 0 || req.Priority > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "priority must be between 0 and 100"})
		return
	}

	key := fmt.Sprintf("%s#%s", repoGroup, prID)
	data, err := db.Get(db.BucketQueueItems, key)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "queue item not found"})
		return
	}

	var item models.QueueItem
	if err := json.Unmarshal(data, &item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse queue item"})
		return
	}

	oldPriority := item.Priority
	item.Priority = req.Priority

	updated, err := json.Marshal(item)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal queue item"})
		return
	}

	if err := db.Put(db.BucketQueueItems, key, updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update queue item"})
		return
	}

	slog.Info("queue priority updated", "repo_group", repoGroup, "pr_id", prID, "old_priority", oldPriority, "new_priority", req.Priority)
	c.JSON(http.StatusOK, gin.H{
		"message":      "priority updated",
		"pr_id":        prID,
		"old_priority": oldPriority,
		"new_priority": req.Priority,
	})
}
