package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// feishuBotHandler is a package-level reference to the feishu bot.
var feishuBotHandler FeishuBotHandler

// FeishuBotHandler is the interface for handling feishu events.
type FeishuBotHandler interface {
	HandleEvent(ctx context.Context, body []byte) (interface{}, error)
}

// InitFeishuBot sets the feishu bot handler for event callbacks.
func InitFeishuBot(handler FeishuBotHandler) {
	feishuBotHandler = handler
}

// FeishuEventHandler handles POST /api/v1/feishu/event
func FeishuEventHandler(c *gin.Context) {
	if feishuBotHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "feishu bot not initialized"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	resp, err := feishuBotHandler.HandleEvent(c.Request.Context(), body)
	if err != nil {
		slog.Error("feishu event handler error", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if resp != nil {
		c.JSON(http.StatusOK, resp)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
