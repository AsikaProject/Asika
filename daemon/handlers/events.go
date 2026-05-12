package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/events"
)

func StreamEvents(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	ch := events.Subscribe()
	defer func() {
		slog.Debug("event stream client disconnected")
	}()

	fmt.Fprintf(c.Writer, "event: connected\ndata: %s\n\n", `{"status":"connected"}`)
	flusher.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", evt.Type, string(data))
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(c.Writer, "event: ping\ndata: %s\n\n", `{"time":"`+time.Now().Format(time.RFC3339)+`"}`)
			flusher.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}
