package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/db"
	"asika/common/models"
)

func GetNotificationPrefs(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	currentUser, _ := c.Get("username")
	currentRole, _ := c.Get("role")
	if currentUser != nil && currentRole != nil {
		if currentUser.(string) != username && currentRole.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: can only view own preferences", "code": 403})
			return
		}
	}

	data, err := db.GetNotificationPrefs(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read preferences"})
		return
	}

	if data == nil {
		c.JSON(http.StatusOK, models.NotificationPreferences{
			Username:   username,
			Enabled:    true,
			EventPrefs: make(map[string]bool),
			DigestMode: "realtime",
		})
		return
	}

	var prefs models.NotificationPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decode preferences"})
		return
	}
	c.JSON(http.StatusOK, prefs)
}

func UpdateNotificationPrefs(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	currentUser, _ := c.Get("username")
	currentRole, _ := c.Get("role")
	if currentUser != nil && currentRole != nil {
		if currentUser.(string) != username && currentRole.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: can only modify own preferences", "code": 403})
			return
		}
	}

	var prefs models.NotificationPreferences
	if err := c.ShouldBindJSON(&prefs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	prefs.Username = username
	data, err := json.Marshal(prefs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode preferences"})
		return
	}

	if err := db.PutNotificationPrefs(username, data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save preferences"})
		return
	}

	resetNotifierPrefsCache()

	slog.Info("notification preferences updated", "username", username)
	c.JSON(http.StatusOK, prefs)
}
