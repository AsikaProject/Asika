package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/i18n"
	"time"

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

	// Only operator can have granular permissions; viewer and admin have fixed permissions
	perms := models.UserPermissions{}
	if req.Role == "operator" {
		// Permissions are not set on creation via this handler; they default to false
		// and must be explicitly enabled via update
	}

	user := models.User{
		Username:          req.Username,
		PasswordHash:      string(hash),
		Role:              req.Role,
		AllowedRepoGroups: req.AllowedRepoGroups,
		Permissions:       perms,
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

// UpdateUser handles PUT /api/v1/users/:username (8.1)
func UpdateUser(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	var req struct {
		Password          *string  `json:"password"`
		Role              *string  `json:"role"`
		AllowedRepoGroups []string `json:"allowed_repo_groups"`
		Permissions       *struct {
			CanApprove     *bool `json:"can_approve"`
			CanMerge       *bool `json:"can_merge"`
			CanClose       *bool `json:"can_close"`
			CanReopen      *bool `json:"can_reopen"`
			CanSpam        *bool `json:"can_spam"`
			CanManageQueue *bool `json:"can_manage_queue"`
		} `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Load existing user
	data, err := db.Get(db.BucketUsers, username)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	var user models.User
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	// Update fields
	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		user.PasswordHash = string(hash)
	}
	if req.Role != nil {
		validRoles := map[string]bool{"viewer": true, "operator": true, "admin": true}
		if !validRoles[*req.Role] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role: must be viewer, operator, or admin"})
			return
		}
		user.Role = *req.Role
		// viewer has no extra permissions; admin has all implicitly
		// only operator can have granular permissions configured
		if *req.Role == "viewer" {
			user.Permissions = models.UserPermissions{}
		}
	}
	if req.AllowedRepoGroups != nil {
		user.AllowedRepoGroups = req.AllowedRepoGroups
	}
	if req.Permissions != nil && user.Role == "operator" {
		p := req.Permissions
		if p.CanApprove != nil {
			user.Permissions.CanApprove = *p.CanApprove
		}
		if p.CanMerge != nil {
			user.Permissions.CanMerge = *p.CanMerge
		}
		if p.CanClose != nil {
			user.Permissions.CanClose = *p.CanClose
		}
		if p.CanReopen != nil {
			user.Permissions.CanReopen = *p.CanReopen
		}
		if p.CanSpam != nil {
			user.Permissions.CanSpam = *p.CanSpam
		}
		if p.CanManageQueue != nil {
			user.Permissions.CanManageQueue = *p.CanManageQueue
		}
	}

	data, err = json.Marshal(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := db.Put(db.BucketUsers, username, data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user updated", "username": username})
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

// CreateTempToken handles POST /api/v1/auth/temp-token
// Generates a short-lived token with elevated permissions for temporary access.
func CreateTempToken(c *gin.Context) {
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	if username == nil || role == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Duration    string            `json:"duration"`     // e.g. "30m", "1h"
		Permissions map[string]bool   `json:"permissions"`  // e.g. {"can_merge": true, "can_approve": true}
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Duration == "" {
		req.Duration = "30m"
	}
	d, err := time.ParseDuration(req.Duration)
	if err != nil || d <= 0 || d > 24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid duration: must be between 1m and 24h"})
		return
	}

	if len(req.Permissions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "permissions required"})
		return
	}

	validPerms := map[string]bool{
		"can_approve": true, "can_merge": true, "can_close": true,
		"can_reopen": true, "can_spam": true, "can_manage_queue": true, "can_revert": true,
	}
	for k := range req.Permissions {
		if !validPerms[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid permission: " + k})
			return
		}
	}

	token, err := auth.GenerateTempToken(username.(string), role.(string), d, req.Permissions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":       token,
		"expires_in":  d.Seconds(),
		"permissions": req.Permissions,
	})
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
