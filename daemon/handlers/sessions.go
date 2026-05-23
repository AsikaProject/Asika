package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"asika/common/auth"
	"asika/common/db"
)

func ListSessions(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	sessions, err := db.ListUserSessions(username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sessions"})
		return
	}

	currentSessionID := ""
	if claims, ok := c.Get("claims"); ok {
		if jwtClaims, ok := claims.(map[string]interface{}); ok {
			if sid, ok := jwtClaims["sid"].(string); ok {
				currentSessionID = sid
			}
		}
	}

	type sessionView struct {
		ID         string `json:"id"`
		IssuedAt   string `json:"issued_at"`
		LastUsedAt string `json:"last_used_at"`
		ExpiresAt  string `json:"expires_at"`
		IPAddress  string `json:"ip_address"`
		UserAgent  string `json:"user_agent"`
		IsCurrent  bool   `json:"is_current"`
	}

	var result []sessionView
	for _, s := range sessions {
		result = append(result, sessionView{
			ID:         s.ID,
			IssuedAt:   s.IssuedAt.Format("2006-01-02 15:04:05"),
			LastUsedAt: s.LastUsedAt.Format("2006-01-02 15:04:05"),
			ExpiresAt:  s.ExpiresAt.Format("2006-01-02 15:04:05"),
			IPAddress:  s.IPAddress,
			UserAgent:  s.UserAgent,
			IsCurrent:  s.ID == currentSessionID,
		})
	}

	c.JSON(http.StatusOK, result)
}

func RevokeSession(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	sessionID := c.Param("id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session id required"})
		return
	}

	session, err := db.GetSession(sessionID)
	if err != nil || session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	if session.Username != username.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot revoke other users' sessions"})
		return
	}

	if err := db.DeleteSession(sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "session revoked"})
}

func RevokeOtherSessions(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	currentSessionID := ""
	if claims, ok := c.Get("claims"); ok {
		if jwtClaims, ok := claims.(map[string]interface{}); ok {
			if sid, ok := jwtClaims["sid"].(string); ok {
				currentSessionID = sid
			}
		}
	}

	sessions, err := db.ListUserSessions(username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sessions"})
		return
	}

	revoked := 0
	for _, s := range sessions {
		if s.ID != currentSessionID {
			if err := db.DeleteSession(s.ID); err == nil {
				revoked++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "other sessions revoked", "count": revoked})
}

func RevokeAllSessions(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	currentSessionID := ""
	if claims, ok := c.Get("claims"); ok {
		if jwtClaims, ok := claims.(map[string]interface{}); ok {
			if sid, ok := jwtClaims["sid"].(string); ok {
				currentSessionID = sid
			}
		}
	}

	sessions, err := db.ListUserSessions(username.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sessions"})
		return
	}

	revoked := 0
	for _, s := range sessions {
		if s.ID != currentSessionID {
			auth.BlacklistToken(s.ID)
			if err := db.DeleteSession(s.ID); err == nil {
				revoked++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "all other sessions revoked", "count": revoked})
}
