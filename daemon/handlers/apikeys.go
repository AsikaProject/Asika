package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"asika/common/auth"
	"asika/common/db"
	"asika/common/models"
)

// apiKeyResponse is the safe representation (raw key only shown once at creation).
type apiKeyResponse struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Role              string                 `json:"role"`
	CreatedAt         time.Time              `json:"created_at"`
	CreatedBy         string                 `json:"created_by"`
	LastUsedAt        time.Time              `json:"last_used_at"`
	AllowedRepoGroups []string               `json:"allowed_repo_groups"`
	AllowedRepos      []string               `json:"allowed_repos"`
	Permissions       models.UserPermissions `json:"permissions"`
	KeyHMAC           string                 `json:"key_hmac"`
	RawKey            string                 `json:"key,omitempty"` // only on creation
}

func toAPIKeyResponse(key *models.APIKey, rawKey string) apiKeyResponse {
	return apiKeyResponse{
		ID:                key.ID,
		Name:              key.Name,
		Role:              key.Role,
		CreatedAt:         key.CreatedAt,
		CreatedBy:         key.CreatedBy,
		LastUsedAt:        key.LastUsedAt,
		AllowedRepoGroups: key.AllowedRepoGroups,
		AllowedRepos:      key.AllowedRepos,
		Permissions:       key.Permissions,
		KeyHMAC:           key.KeyHMAC,
		RawKey:            rawKey,
	}
}

// CreateAPIKey handles POST /api/v1/apikeys
func CreateAPIKey(c *gin.Context) {
	username := c.GetString("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Name              string   `json:"name"`
		Role              string   `json:"role"`
		AllowedRepoGroups []string `json:"allowed_repo_groups"`
		AllowedRepos      []string `json:"allowed_repos"`
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

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if req.Role == "" {
		req.Role = "operator"
	}
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[req.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role: must be admin, operator, or viewer"})
		return
	}

	rawKey := generateAPIKey()
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate key"})
		return
	}

	keyHMAC := computeAPIKeyHMAC(rawKey)

	perms := models.UserPermissions{}
	if req.Role == "operator" && req.Permissions != nil {
		p := req.Permissions
		if p.CanApprove != nil {
			perms.CanApprove = *p.CanApprove
		}
		if p.CanMerge != nil {
			perms.CanMerge = *p.CanMerge
		}
		if p.CanClose != nil {
			perms.CanClose = *p.CanClose
		}
		if p.CanReopen != nil {
			perms.CanReopen = *p.CanReopen
		}
		if p.CanSpam != nil {
			perms.CanSpam = *p.CanSpam
		}
		if p.CanManageQueue != nil {
			perms.CanManageQueue = *p.CanManageQueue
		}
	}

	apiKey := &models.APIKey{
		ID:                generateAPIKeyID(),
		Name:              req.Name,
		KeyHash:           string(hash),
		KeyHMAC:           keyHMAC,
		Role:              req.Role,
		CreatedAt:         time.Now(),
		CreatedBy:         username,
		AllowedRepoGroups: req.AllowedRepoGroups,
		AllowedRepos:      req.AllowedRepos,
		Permissions:       perms,
	}

	if err := db.PutAPIKey(apiKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save API key"})
		return
	}

	resp := toAPIKeyResponse(apiKey, rawKey)
	c.JSON(http.StatusOK, resp)
}

// ListAPIKeys handles GET /api/v1/apikeys
func ListAPIKeys(c *gin.Context) {
	keys, err := db.ListAPIKeys()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list API keys"})
		return
	}
	resp := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, toAPIKeyResponse(k, ""))
	}
	c.JSON(http.StatusOK, resp)
}

// RevokeAPIKey handles DELETE /api/v1/apikeys/:id
func RevokeAPIKey(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key ID required"})
		return
	}
	if err := db.DeleteAPIKey(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete API key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "API key revoked"})
}

var hmacSecret []byte

// SetHMACSecret sets the secret used for API key HMAC pre-check.
func SetHMACSecret(secret string) {
	hmacSecret = []byte(secret)
}

func computeAPIKeyHMAC(rawKey string) string {
	mac := hmac.New(sha256.New, hmacSecret)
	mac.Write([]byte(rawKey))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateAPIKey checks a raw API key against stored hashes.
// Uses HMAC pre-check to avoid O(n) bcrypt comparisons.
func ValidateAPIKey(rawKey string) *models.APIKey {
	keys, err := db.ListAPIKeys()
	if err != nil {
		return nil
	}
	rawHMAC := computeAPIKeyHMAC(rawKey)
	for _, k := range keys {
		if !hmac.Equal([]byte(k.KeyHMAC), []byte(rawHMAC)) {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(rawKey)); err == nil {
			return k
		}
	}
	return nil
}

// APIKeyAuth returns a gin middleware that authenticates via X-API-Key header.
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("username") != "" {
			c.Next()
			return
		}

		rawKey := c.GetHeader("X-API-Key")
		if rawKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "API key required (X-API-Key header)"})
			return
		}

		apiKey := ValidateAPIKey(rawKey)
		if apiKey == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
			return
		}

		apiKey.LastUsedAt = time.Now()
		db.PutAPIKey(apiKey)

		c.Set("username", fmt.Sprintf("apikey:%s", apiKey.Name))
		c.Set("role", apiKey.Role)
		c.Set("api_key_id", apiKey.ID)
		c.Set("allowed_repo_groups", apiKey.AllowedRepoGroups)
		c.Set("allowed_repos", apiKey.AllowedRepos)
		c.Set("permissions", apiKey.Permissions)
		c.Next()
	}
}

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "ak_" + hex.EncodeToString(b)
}

func generateAPIKeyID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func init() {
	_ = auth.HasPermission
	SetHMACSecret("asika-apikey-hmac-v1")
}
