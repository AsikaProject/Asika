package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"asika/common/config"
	"asika/common/models"
)

// WebhookConfig represents a webhook configuration entry
type WebhookConfig struct {
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config"`
}

// ListWebhooks handles GET /api/v1/webhooks
func ListWebhooks(c *gin.Context) {
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	webhooks := make([]WebhookConfig, 0)
	for _, nc := range cfg.Notify {
		if nc.Type == "webhook" {
			webhooks = append(webhooks, WebhookConfig{
				Type:   nc.Type,
				Config: nc.Config,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"webhooks": webhooks})
}

// CreateWebhook handles POST /api/v1/webhooks
func CreateWebhook(c *gin.Context) {
	var req WebhookConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Type != "webhook" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be 'webhook'"})
		return
	}

	// Validate required fields
	url, ok := req.Config["url"].(string)
	if !ok || url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	// Add to config
	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	newNotify := models.NotifyConfig{
		Type:   req.Type,
		Config: req.Config,
	}
	cfg.Notify = append(cfg.Notify, newNotify)

	// Save config
	if err := config.SaveToFile(*cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "webhook created", "webhook": req})
}

// DeleteWebhook handles DELETE /api/v1/webhooks/:index
func DeleteWebhook(c *gin.Context) {
	indexStr := c.Param("index")
	index := 0
	if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index"})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	// Find webhook index in notify list
	webhookIndex := -1
	currentIndex := 0
	for i, nc := range cfg.Notify {
		if nc.Type == "webhook" {
			if currentIndex == index {
				webhookIndex = i
				break
			}
			currentIndex++
		}
	}

	if webhookIndex == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	// Remove from config
	cfg.Notify = append(cfg.Notify[:webhookIndex], cfg.Notify[webhookIndex+1:]...)

	// Save config
	if err := config.SaveToFile(*cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "webhook deleted"})
}

// TestWebhook handles POST /api/v1/webhooks/:index/test
func TestWebhook(c *gin.Context) {
	indexStr := c.Param("index")
	index := 0
	if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index"})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config not loaded"})
		return
	}

	// Find webhook in notify list
	webhookIndex := -1
	currentIndex := 0
	for i, nc := range cfg.Notify {
		if nc.Type == "webhook" {
			if currentIndex == index {
				webhookIndex = i
				break
			}
			currentIndex++
		}
	}

	if webhookIndex == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	// Send test notification
	SendNotifications("Test Webhook", "This is a test notification from Asika")

	c.JSON(http.StatusOK, gin.H{"message": "test notification sent"})
}
