package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/i18n"

    "asika/common/auth"
    "asika/common/config"
    "asika/common/db"
    "asika/common/models"
)

// Login handles POST /api/v1/auth/login (8.1)
func Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	cfg := config.Current()
	if cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "server not initialized"})
		return
	}

	// Find user in DB
	var user models.User
	data, err := db.Get(db.BucketUsers, req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Generate JWT
	token, err := auth.GenerateJWT(user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	// Set cookie for SSR page auth (browser navigations don't send Authorization header)
	c.SetCookie(
		"asika_token", token,
		int(config.GenerateTokenExpiry(cfg.Auth.TokenExpiry).Seconds()),
		"/", "", false, true,
	)

	c.JSON(http.StatusOK, gin.H{"token": token, "username": user.Username, "role": user.Role})
}

// Logout handles POST /api/v1/auth/logout (8.1)
func Logout(c *gin.Context) {
    token := extractLogoutToken(c)
    if token != "" {
        auth.BlacklistToken(token)
    }
    c.SetCookie("asika_token", "", -1, "/", "", false, true)
    c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func extractLogoutToken(c *gin.Context) string {
    if token, err := c.Cookie("asika_token"); err == nil && token != "" {
        return token
    }
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

// ListUsers handles GET /api/v1/users (8.1)
func ListUsers(c *gin.Context) {
	var users []models.User
	err := db.ForEach(db.BucketUsers, func(key, value []byte) error {
		var user models.User
		if err := json.Unmarshal(value, &user); err != nil {
			return err
		}
		user.PasswordHash = "***"
		users = append(users, user)
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	c.JSON(http.StatusOK, users)
}

// CreateUser handles POST /api/v1/users (8.1)
func CreateUser(c *gin.Context) {
	var req struct {
		Username          string   `json:"username"`
		Password          string   `json:"password"`
		Role              string   `json:"role"`
		AllowedRepoGroups []string `json:"allowed_repo_groups"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	if req.Role == "" {
		req.Role = "viewer"
	}

	validRoles := map[string]bool{"viewer": true, "operator": true, "admin": true}
	if !validRoles[req.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role: must be viewer, operator, or admin"})
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user := models.User{
		Username:          req.Username,
		PasswordHash:      string(hash),
		Role:              req.Role,
		AllowedRepoGroups: req.AllowedRepoGroups,
	}

	data, err := json.Marshal(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if err := db.Put(db.BucketUsers, req.Username, data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user created", "username": req.Username})
}

// DeleteUser handles DELETE /api/v1/users/:username (8.1)
func DeleteUser(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	if err := db.Delete(db.BucketUsers, username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

// SetLocale handles POST /api/v1/locale
// Sets the UI locale via cookie.
func SetLocale(c *gin.Context) {
	var req struct {
		Locale string `json:"locale"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Locale == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "locale required"})
		return
	}
	i18n.SetLocale(req.Locale)
	c.SetCookie("asika_lang", req.Locale, 86400*365, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "locale set", "locale": req.Locale})
}
