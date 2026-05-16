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
	"asika/common/events"
	"asika/common/models"
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

	currentUser, _ := c.Get("username")
	currentRole, _ := c.Get("role")
	isAdmin := currentRole != nil && currentRole.(string) == "admin"

	ch := events.Subscribe()
	defer func() {
		events.Unsubscribe(ch)
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
			if !isAdmin && evt.RepoGroup != "" && currentUser != nil {
				if !canUserAccessRepoGroup(currentUser.(string), evt.RepoGroup) {
					continue
				}
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

func canUserAccessRepoGroup(username, repoGroup string) bool {
	cfg := config.Current()
	if cfg == nil {
		return false
	}
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return false
	}
	data, err := db.Get(db.BucketUsers, username)
	if err != nil {
		return false
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	if len(user.AllowedRepoGroups) == 0 {
		return true
	}
	for _, g := range user.AllowedRepoGroups {
		if g == repoGroup {
			return true
		}
	}
	return false
}
