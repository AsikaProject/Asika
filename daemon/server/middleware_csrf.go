package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	csrfTokens   = make(map[string]time.Time)
	csrfTokensMu sync.Mutex
	csrfTTL      = 1 * time.Hour
)

func init() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			csrfTokensMu.Lock()
			for token, created := range csrfTokens {
				if time.Since(created) > csrfTTL {
					delete(csrfTokens, token)
				}
			}
			csrfTokensMu.Unlock()
		}
	}()
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func CSRFProtect() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		username, _ := c.Get("username")
		if username == nil || username.(string) == "" {
			c.Next()
			return
		}

		token := c.GetHeader("X-CSRF-Token")
		if token == "" {
			token = c.PostForm("_csrf")
		}
		if token == "" {
			token = c.Query("_csrf")
		}

		csrfTokensMu.Lock()
		_, valid := csrfTokens[token]
		csrfTokensMu.Unlock()

		if !valid {
			slog.Warn("csrf validation failed", "path", c.Request.URL.Path, "ip", c.ClientIP())
			c.JSON(http.StatusForbidden, gin.H{"error": "CSRF validation failed", "code": 403})
			c.Abort()
			return
		}

		c.Next()
	}
}

func IssueCSRFToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := generateCSRFToken()
		csrfTokensMu.Lock()
		csrfTokens[token] = time.Now()
		csrfTokensMu.Unlock()
		c.Header("X-CSRF-Token", token)
		c.Set("csrf_token", token)
		c.Next()
	}
}
