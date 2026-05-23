package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/i18n"
	"asika/common/models"
)

// Login handles POST /api/v1/auth/login (8.1)
func Login(c *gin.Context) {
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		TOTPCode  string `json:"totp_code"`
		SessionID string `json:"session_id"`
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

	var user models.User
	data, err := db.Get(db.BucketUsers, req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if data == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err := json.Unmarshal(data, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	needsTOTP := user.TOTPEnabled || cfg.Auth.TOTPRequired
	if needsTOTP && req.SessionID == "" && req.TOTPCode == "" {
		sessionID := auth.GenerateSessionID()
		totpSession := models.Session{
			ID:         sessionID,
			Username:   user.Username,
			IssuedAt:   time.Now(),
			LastUsedAt: time.Now(),
			ExpiresAt:  time.Now().Add(5 * time.Minute),
			IPAddress:  c.ClientIP(),
			UserAgent:  c.Request.UserAgent(),
		}
		if err := db.PutSession(&totpSession); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"two_factor_required": true,
			"session_id":          sessionID,
		})
		return
	}

	if needsTOTP && req.SessionID != "" && req.TOTPCode != "" {
		session, err := db.GetSession(req.SessionID)
		if err != nil || session == nil || session.Username != user.Username {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session"})
			return
		}
		if session.ExpiresAt.Before(time.Now()) {
			db.DeleteSession(session.ID)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}
		if !validateTOTPCode(user.TOTPSecret, req.TOTPCode) {
			if !consumeBackupCode(user.Username, req.TOTPCode, user.BackupCodes) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid TOTP code"})
				return
			}
		}
		db.DeleteSession(session.ID)
	}

	sessionID := auth.GenerateSessionID()
	now := time.Now()
	expiry := config.GenerateTokenExpiry(cfg.Auth.TokenExpiry)
	session := &models.Session{
		ID:          sessionID,
		Username:    user.Username,
		TokenPrefix: "",
		IssuedAt:    now,
		LastUsedAt:  now,
		ExpiresAt:   now.Add(expiry),
		IPAddress:   c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	}
	if err := db.PutSession(session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	token, err := auth.GenerateJWTWithSession(user.Username, user.Role, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"asika_token", token,
		int(expiry.Seconds()),
		"/", "", true, true,
	)

	c.JSON(http.StatusOK, gin.H{"token": token, "username": user.Username, "role": user.Role})
}

// Logout handles POST /api/v1/auth/logout (8.1)
func Logout(c *gin.Context) {
	token := extractLogoutToken(c)
	if token != "" {
		auth.BlacklistToken(token)
		if claims, err := auth.ValidateJWT(token); err == nil {
			if sid, ok := claims["sid"].(string); ok && sid != "" {
				db.DeleteSession(sid)
			}
		}
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("asika_token", "", -1, "/", "", true, true)
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
	limit := 0
	offset := 0
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if o := c.Query("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
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
	start := offset
	if start > len(users) {
		start = len(users)
	}
	end := len(users)
	if limit > 0 {
		end = start + limit
		if end > len(users) {
			end = len(users)
		}
	}
	c.JSON(http.StatusOK, users[start:end])
}

// CreateUser handles POST /api/v1/users (8.1)
func CreateUser(c *gin.Context) {
	var req struct {
		Username          string   `json:"username"`
		Password          string   `json:"password"`
		Role              string   `json:"role"`
		AllowedRepoGroups []string `json:"allowed_repo_groups"`
		AllowedRepos      []string `json:"allowed_repos"`
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

	existing, err := db.Get(db.BucketUsers, req.Username)
	if err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "user already exists"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	perms := models.UserPermissions{}
	if req.Role == "operator" {
	}

	user := models.User{
		Username:          req.Username,
		PasswordHash:      string(hash),
		Role:              req.Role,
		AllowedRepoGroups: req.AllowedRepoGroups,
		AllowedRepos:      req.AllowedRepos,
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
		AllowedRepos      []string `json:"allowed_repos"`
		Permissions       *struct {
			CanApprove     *bool `json:"can_approve"`
			CanMerge       *bool `json:"can_merge"`
			CanClose       *bool `json:"can_close"`
			CanReopen      *bool `json:"can_reopen"`
			CanSpam        *bool `json:"can_spam"`
			CanManageQueue *bool `json:"can_manage_queue"`
			CanRevert      *bool `json:"can_revert"`
			CanComment     *bool `json:"can_comment"`
			CanLabel       *bool `json:"can_label"`
		} `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

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
		if user.Role != *req.Role {
			user.Role = *req.Role
			if err := db.DeleteUserSessions(username); err != nil {
				slog.Warn("failed to revoke sessions on role change", "username", username, "error", err)
			}
		}
		if *req.Role == "viewer" {
			user.Permissions = models.UserPermissions{}
		}
	}
	if req.AllowedRepoGroups != nil {
		user.AllowedRepoGroups = req.AllowedRepoGroups
	}
	if req.AllowedRepos != nil {
		user.AllowedRepos = req.AllowedRepos
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
		if p.CanRevert != nil {
			user.Permissions.CanRevert = *p.CanRevert
		}
		if p.CanComment != nil {
			user.Permissions.CanComment = *p.CanComment
		}
		if p.CanLabel != nil {
			user.Permissions.CanLabel = *p.CanLabel
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

	if err := db.DeleteUserSessions(username); err != nil {
		slog.Warn("failed to delete user sessions", "username", username, "error", err)
	}

	keys, err := db.ListAPIKeys(0, 0)
	if err != nil {
		slog.Warn("failed to list API keys for user deletion", "username", username, "error", err)
	} else {
		for _, k := range keys {
			if k.CreatedBy == username {
				if err := db.DeleteAPIKey(k.ID); err != nil {
					slog.Warn("failed to delete API key", "key_id", k.ID, "error", err)
				}
			}
		}
	}

	links, err := db.ListOIDCLinks(username)
	if err != nil {
		slog.Warn("failed to list OIDC links for user deletion", "username", username, "error", err)
	} else {
		for _, l := range links {
			if err := db.DeleteOIDCLink(l.Provider, l.Subject); err != nil {
				slog.Warn("failed to delete OIDC link", "provider", l.Provider, "subject", l.Subject, "error", err)
			}
		}
	}

	if err := db.Delete(db.BucketUsers, username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

var permRoleRequirement = map[string]string{
	"can_approve":      "operator",
	"can_merge":        "operator",
	"can_close":        "operator",
	"can_reopen":       "operator",
	"can_spam":         "operator",
	"can_manage_queue": "operator",
	"can_revert":       "operator",
	"can_comment":      "viewer",
	"can_label":        "operator",
}

// CreateTempToken handles POST /api/v1/auth/temp-token
func CreateTempToken(c *gin.Context) {
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	if username == nil || role == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Duration    string          `json:"duration"`
		Permissions map[string]bool `json:"permissions"`
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
		"can_reopen": true, "can_spam": true, "can_manage_queue": true, "can_revert": true, "can_comment": true, "can_label": true,
	}
	for k := range req.Permissions {
		if !validPerms[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid permission: " + k})
			return
		}
	}

	currentRole := role.(string)
	roleLevel := map[string]int{"viewer": 1, "operator": 2, "admin": 3}
	userLevel := roleLevel[currentRole]
	for perm := range req.Permissions {
		if !req.Permissions[perm] {
			continue
		}
		requiredRole := permRoleRequirement[perm]
		if userLevel < roleLevel[requiredRole] {
			c.JSON(http.StatusForbidden, gin.H{"error": "cannot grant permission " + perm + ": requires " + requiredRole + " role"})
			return
		}
	}

	token, sessionID, err := auth.GenerateTempToken(username.(string), currentRole, d, req.Permissions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	now := time.Now()
	session := &models.Session{
		ID:          sessionID,
		Username:    username.(string),
		TokenPrefix: "temp",
		IssuedAt:    now,
		LastUsedAt:  now,
		ExpiresAt:   now.Add(d),
		IPAddress:   c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
	}
	if err := db.PutSession(session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store temp session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":       token,
		"expires_in":  d.Seconds(),
		"permissions": req.Permissions,
	})
}

func validateTOTP(secret string, code string, backupCodes []string) bool {
	if validateTOTPCode(secret, code) {
		return true
	}
	for _, hash := range backupCodes {
		if hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
			return true
		}
	}
	return false
}

func consumeBackupCode(username string, code string, backupCodes []string) bool {
	for i, hash := range backupCodes {
		if hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
			backupCodes[i] = ""
			data, err := db.Get(db.BucketUsers, username)
			if err != nil {
				return true
			}
			var user models.User
			if err := json.Unmarshal(data, &user); err != nil {
				return true
			}
			var cleaned []string
			for _, h := range backupCodes {
				if h != "" {
					cleaned = append(cleaned, h)
				}
			}
			user.BackupCodes = cleaned
			updated, _ := json.Marshal(user)
			if err := db.Put(db.BucketUsers, username, updated); err != nil {
				slog.Warn("failed to save consumed backup code", "username", username, "error", err)
			}
			return true
		}
	}
	return false
}

// SetLocale handles POST /api/v1/locale
func SetLocale(c *gin.Context) {
	var req struct {
		Locale string `json:"locale"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Locale == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "locale required"})
		return
	}
	i18n.SetLocale(req.Locale)
	c.SetCookie("asika_lang", req.Locale, 86400*365, "/", "", true, false)
	c.JSON(http.StatusOK, gin.H{"message": "locale set", "locale": req.Locale})
}
