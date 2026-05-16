package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"asika/common/auth"
	"asika/common/config"
	"asika/common/db"
	"asika/common/i18n"
	"asika/common/models"
	"asika/daemon/handlers"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

// Logger is a custom logger middleware
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()

		slog.Info("request",
			"method", c.Request.Method,
			"path", path,
			"status", statusCode,
			"latency", latency,
			"ip", c.ClientIP(),
		)
	}
}

// AuthMiddleware creates an authentication middleware
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		skipPaths := []string{"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/wizard", "/login", "/wizard"}
		skip := false
		for _, p := range skipPaths {
			if strings.HasPrefix(path, p) {
				skip = true
				break
			}
		}
		if path == "/health" || skip {
			c.Next()
			return
		}

		token := extractToken(c)
		if token != "" {
			claims, err := auth.ValidateJWT(token)
			if err == nil {
				c.Set("username", auth.GetUsername(claims))
				c.Set("role", auth.GetUserRole(claims))
				c.Set("claims", claims)
				c.Next()
				return
			}
		}

		// Try API Key authentication
		apiKey := extractAPIKey(c)
		if apiKey != "" {
			key := handlers.ValidateAPIKey(apiKey)
			if key != nil {
				key.LastUsedAt = time.Now()
				db.PutAPIKey(key)
				c.Set("username", fmt.Sprintf("apikey:%s", key.Name))
				c.Set("role", key.Role)
				c.Set("api_key_id", key.ID)
				c.Set("allowed_repo_groups", key.AllowedRepoGroups)
				c.Set("allowed_repos", key.AllowedRepos)
				c.Next()
				return
			}
		}

		// No valid auth
		if strings.HasPrefix(path, "/api/") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid token/API key", "code": 401})
		} else {
			c.Redirect(http.StatusFound, "/login")
		}
		c.Abort()
	}
}

// LocaleMiddleware detects the user's preferred locale from Accept-Language header
// or cookie and sets the i18n locale for the request.
func LocaleMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		locale := i18n.Locale()
		// Check cookie first (user preference override)
		if lang, err := c.Cookie("asika_lang"); err == nil && lang != "" {
			locale = lang
		} else {
			locale = i18n.ParseAcceptLanguage(c.GetHeader("Accept-Language"))
		}
		c.Set("locale", locale)
		c.Next()
	}
}

// RequireAuth requires authentication
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}
		c.Next()
	}
}

// SSRAuthRequired redirects to login if not authenticated (for browser pages)
func SSRAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, exists := c.Get("username")
		if !exists || username == nil || username.(string) == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}

func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		if !auth.HasPermission(userRole.(string), role) {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不够", "code": 403})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireRepoGroupAccess checks if the user has access to the requested repo group.
// Admins bypass this check. For non-admins, the repo_group URL param must be in
// the user's AllowedRepoGroups list.
func RequireRepoGroupAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, _ := c.Get("role")
		if userRole != nil && userRole.(string) == "admin" {
			c.Next()
			return
		}

		repoGroup := c.Param("repo_group")
		if repoGroup == "" {
			c.Next()
			return
		}

		username, _ := c.Get("username")
		if username == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		// API key auth: check AllowedRepoGroups on the key
		allowedGroups, _ := c.Get("allowed_repo_groups")
		if allowedGroups != nil {
			groups, ok := allowedGroups.([]string)
			if ok && len(groups) > 0 {
				for _, g := range groups {
					if g == repoGroup {
						c.Next()
						return
					}
				}
				c.JSON(http.StatusForbidden, gin.H{"error": "权限不够: 无权访问仓库组 " + repoGroup, "code": 403})
				c.Abort()
				return
			}
		}

		data, err := db.Get(db.BucketUsers, username.(string))
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": 403})
			c.Abort()
			return
		}
		var user models.User
		if err := json.Unmarshal(data, &user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			c.Abort()
			return
		}

		if len(user.AllowedRepoGroups) == 0 {
			c.Next()
			return
		}

		for _, g := range user.AllowedRepoGroups {
			if g == repoGroup {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "权限不够: 无权访问仓库组 " + repoGroup, "code": 403})
		c.Abort()
	}
}

// RequireRepoAccess checks if the user has access to the specific repo within a repo group.
// This is a finer-grained check on top of RequireRepoGroupAccess.
func RequireRepoAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, _ := c.Get("role")
		if userRole != nil && userRole.(string) == "admin" {
			c.Next()
			return
		}

		username, _ := c.Get("username")
		if username == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		// API key auth: check AllowedRepos on the key
		allowedRepos, _ := c.Get("allowed_repos")
		if allowedRepos != nil {
			repos, ok := allowedRepos.([]string)
			if ok && len(repos) > 0 {
				resolvedRepo := resolveRepoFromRequest(c)
				if resolvedRepo == "" {
					c.Next()
					return
				}
				for _, r := range repos {
					if r == resolvedRepo {
						c.Next()
						return
					}
				}
				c.JSON(http.StatusForbidden, gin.H{"error": "权限不够: 无权访问仓库 " + resolvedRepo, "code": 403})
				c.Abort()
				return
			}
		}

		data, err := db.Get(db.BucketUsers, username.(string))
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": 403})
			c.Abort()
			return
		}
		var user models.User
		if err := json.Unmarshal(data, &user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			c.Abort()
			return
		}

		if len(user.AllowedRepos) == 0 {
			c.Next()
			return
		}

		resolvedRepo := resolveRepoFromRequest(c)
		if resolvedRepo == "" {
			c.Next()
			return
		}

		for _, r := range user.AllowedRepos {
			if r == resolvedRepo {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "权限不够: 无权访问仓库 " + resolvedRepo, "code": 403})
		c.Abort()
	}
}

// resolveRepoFromRequest resolves the owner/repo string from the request context.
func resolveRepoFromRequest(c *gin.Context) string {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")
	if repoGroup == "" || prID == "" {
		return ""
	}

	cfg := config.Current()
	if cfg == nil {
		return ""
	}

	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		return ""
	}

	var platform string
	if prID != "" {
		var prRecord *models.PRRecord
		db.ForEach(db.BucketPRs, func(key, value []byte) error {
			var rec models.PRRecord
			if json.Unmarshal(value, &rec) == nil && rec.ID == prID && rec.RepoGroup == repoGroup {
				prRecord = &rec
				return errStopForEach
			}
			return nil
		})
		if prRecord != nil {
			platform = prRecord.Platform
		}
	}

	if platform == "" {
		platform = config.GetPlatformForGroup(group)
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, platform)
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

var errStopForEach = fmt.Errorf("__stop__")

// RequireSpaceAccess checks if the user has access to the space that owns the
// requested repo group. Admins bypass this check.
func RequireSpaceAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, _ := c.Get("role")
		if userRole != nil && userRole.(string) == "admin" {
			c.Next()
			return
		}

		username, _ := c.Get("username")
		if username == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		repoGroup := c.Param("repo_group")
		if repoGroup == "" {
			c.Next()
			return
		}

		cfg := config.Current()
		if cfg == nil {
			c.Next()
			return
		}

		group := config.GetRepoGroupByName(cfg, repoGroup)
		if group == nil {
			c.Next()
			return
		}

		spaces, err := db.ListTeamSpaces()
		if err != nil || len(spaces) == 0 {
			c.Next()
			return
		}

		for _, space := range spaces {
			for _, sg := range space.RepoGroups {
				if sg == repoGroup {
					members, err := db.GetSpaceMembers(space.Name)
					if err != nil {
						continue
					}
					for _, m := range members {
						if m.Username == username.(string) {
							c.Set("space_name", space.Name)
							c.Set("space_role", m.Role)
							c.Next()
							return
						}
					}
					c.JSON(http.StatusForbidden, gin.H{"error": "权限不够: 不属于空间 " + space.Name, "code": 403})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

// RequireAnyRole requires any of the specified roles
func RequireAnyRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		for _, r := range roles {
			if auth.HasPermission(userRole.(string), r) {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": 403})
		c.Abort()
	}
}

// RequirePermission checks if the user has a specific granular permission.
// Admins always pass. For non-admins, checks the user's Permissions field.
// Supports both JWT users (loaded from DB) and API Keys (permissions stored in key).
func RequirePermission(permField string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, _ := c.Get("role")
		if userRole != nil && userRole.(string) == "admin" {
			c.Next()
			return
		}

		// Check if this is an API key request
		if apiKeyName, _ := c.Get("username"); apiKeyName != nil {
			uname := apiKeyName.(string)
			if strings.HasPrefix(uname, "apikey:") {
				apiKeyID, _ := c.Get("api_key_id")
				if apiKeyID != nil {
					key, err := db.GetAPIKey(apiKeyID.(string))
					if err == nil && key != nil {
						var hasPerm bool
						switch permField {
						case "approve":
							hasPerm = key.Permissions.CanApprove
						case "merge":
							hasPerm = key.Permissions.CanMerge
						case "close":
							hasPerm = key.Permissions.CanClose
						case "reopen":
							hasPerm = key.Permissions.CanReopen
						case "spam":
							hasPerm = key.Permissions.CanSpam
						case "manage_queue":
							hasPerm = key.Permissions.CanManageQueue
		case "revert":
			hasPerm = key.Permissions.CanRevert
		case "comment":
			hasPerm = key.Permissions.CanComment
		case "label":
			hasPerm = key.Permissions.CanLabel
		}
						if hasPerm {
							c.Next()
							return
						}
					}
				}
				c.JSON(http.StatusForbidden, gin.H{"error": "权限不够", "code": 403})
				c.Abort()
				return
			}
		}

		username, _ := c.Get("username")
		if username == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": 401})
			c.Abort()
			return
		}

		claims, _ := c.Get("claims")
		if claims != nil {
			if jwtClaims, ok := claims.(jwt.MapClaims); ok && auth.IsTempToken(jwtClaims) {
				tempPerms := auth.GetTempPermissions(jwtClaims)
				if tempPerms != nil {
					if enabled, exists := tempPerms[permField]; exists && enabled {
						c.Next()
						return
					}
					c.JSON(http.StatusForbidden, gin.H{"error": "权限不够", "code": 403})
					c.Abort()
					return
				}
			}
		}

		data, err := db.Get(db.BucketUsers, username.(string))
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": 403})
			c.Abort()
			return
		}
		var user models.User
		if err := json.Unmarshal(data, &user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			c.Abort()
			return
		}

		var hasPerm bool
		switch permField {
		case "approve":
			hasPerm = user.Permissions.CanApprove
		case "merge":
			hasPerm = user.Permissions.CanMerge
		case "close":
			hasPerm = user.Permissions.CanClose
		case "reopen":
			hasPerm = user.Permissions.CanReopen
		case "spam":
			hasPerm = user.Permissions.CanSpam
		case "manage_queue":
			hasPerm = user.Permissions.CanManageQueue
		case "revert":
			hasPerm = user.Permissions.CanRevert
		case "comment":
			hasPerm = user.Permissions.CanComment
		case "label":
			hasPerm = user.Permissions.CanLabel
		default:
			hasPerm = false
		}

		if !hasPerm {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不够", "code": 403})
			c.Abort()
			return
		}
		c.Next()
	}
}

// extractToken extracts the JWT token from Authorization header or cookie
func extractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return parts[1]
		}
	}
	if token, err := c.Cookie("asika_token"); err == nil {
		return token
	}
	return ""
}

// extractAPIKey extracts the API key from X-API-Key header
func extractAPIKey(c *gin.Context) string {
	return c.GetHeader("X-API-Key")
}

// FingerprintMiddleware creates a middleware that validates fingerprint tokens.
// It looks for the token in X-Fingerprint-Token header or Authorization: Fingerprint <token>.
// If fingerprint auth is not configured, the middleware is a no-op.
func FingerprintMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !c.GetBool("fingerprint_enabled") {
			c.Next()
			return
		}

		token := extractFingerprintToken(c)
		if token == "" {
			c.Next()
			return
		}

		username, err := auth.VerifyFingerprintToken(token)
		if err != nil {
			slog.Warn("fingerprint verification failed", "error", err, "ip", c.ClientIP())
			c.Next()
			return
		}

		c.Set("fingerprint_user", username)
		c.Set("fingerprint_verified", true)
		c.Next()
	}
}

func extractFingerprintToken(c *gin.Context) string {
	if token := c.GetHeader("X-Fingerprint-Token"); token != "" {
		return token
	}

	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "fingerprint") {
			return parts[1]
		}
	}

	return ""
}
