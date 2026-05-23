package server

import (
	"github.com/gin-gonic/gin"
)

// CORS middleware adds Cross-Origin Resource Sharing headers.
// allowedOrigins: list of allowed origins, ["*"] for all.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"
	origins := make(map[string]bool)
	for _, o := range allowedOrigins {
		if o != "*" {
			origins[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		if allowAll {
			c.Header("Access-Control-Allow-Origin", "*")
		} else if origins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-CSRF-Token, X-Fingerprint-Token")
		} else {
			c.Next()
			return
		}

		if !allowAll {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if allowAll {
			c.Header("Access-Control-Allow-Headers", "Content-Type")
		}
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
