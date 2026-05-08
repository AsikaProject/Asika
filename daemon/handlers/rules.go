package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
)

// GetLabelRules handles GET /api/v1/rules/labels (8.4)
func GetLabelRules(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	// Try to get from DB first (hot-reloadable)
	data, err := db.Get(db.BucketConfig, "label_rules")
	if err == nil {
		var rules []map[string]interface{}
		if json.Unmarshal(data, &rules) == nil {
			c.JSON(http.StatusOK, rules)
			return
		}
	}

	// Fallback to config
	c.JSON(http.StatusOK, cfg.LabelRules)
}

// UpdateLabelRules handles PUT /api/v1/rules/labels (8.4)
func UpdateLabelRules(c *gin.Context) {
	var rules []models.LabelRule
	if err := c.ShouldBindJSON(&rules); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Save to DB for hot reload
	data, err := json.Marshal(rules)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal rules"})
		return
	}

	if err := db.Put(db.BucketConfig, "label_rules", data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save rules"})
		return
	}

	// Update in-memory config (deep copy to avoid data race)
	cfg := config.Current()
	if cfg != nil {
		newCfg := *cfg
		newCfg.LabelRules = rules
		config.Store(&newCfg)
	}

	slog.Info("label rules updated")
	c.JSON(http.StatusOK, gin.H{"message": "rules updated"})
}
