package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"log/slog"
)

// AuthMiddleware creates a gin middleware for JWT authentication
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token", "code": 401})
			c.Abort()
			return
		}

		claims, err := ValidateJWT(token)
		if err != nil {
			slog.Warn("invalid token", "error", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token", "code": 401})
			c.Abort()
			return
		}

		// Store claims in context
		c.Set("username", GetUsername(claims))
		c.Set("role", GetUserRole(claims))
		c.Set("claims", claims)

		c.Next()
	}
}

// RequireRole creates a middleware that requires a specific role
func RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		if !HasPermission(role.(string), requiredRole) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": 403})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyRole creates a middleware that requires any of the specified roles
func RequireAnyRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		userRole := role.(string)
		for _, r := range roles {
			if HasPermission(userRole, r) {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": 403})
		c.Abort()
	}
}

// extractToken extracts the JWT token from the Authorization header
func extractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}

	return parts[1]
}

// GetCurrentUser returns the current user from context
func GetCurrentUser(c *gin.Context) (string, string) {
	username, _ := c.Get("username")
	role, _ := c.Get("role")

	usernameStr := ""
	roleStr := ""

	if v, ok := username.(string); ok {
		usernameStr = v
	}
	if v, ok := role.(string); ok {
		roleStr = v
	}

	return usernameStr, roleStr
}
