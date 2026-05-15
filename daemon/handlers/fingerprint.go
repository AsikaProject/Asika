package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/auth"
)

// RegisterFingerprint handles POST /api/v1/auth/fingerprint
// Generates a new fingerprint token for the authenticated user.
func RegisterFingerprint(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	token, err := auth.GenerateFingerprintToken(username.(string))
	if err != nil {
		slog.Error("failed to generate fingerprint", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate fingerprint"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"fingerprint": token,
	})
}

// VerifyFingerprintHandler handles POST /api/v1/auth/fingerprint/verify
// Verifies a fingerprint token and returns the associated username.
func VerifyFingerprintHandler(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token required"})
		return
	}

	username, err := auth.VerifyFingerprintToken(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid fingerprint"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":    true,
		"username": username,
	})
}

// ListFingerprints handles GET /api/v1/auth/fingerprints
// Lists all active fingerprint token IDs for the current user.
func ListFingerprints(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ids := auth.ListUserFingerprints(username.(string))
	c.JSON(http.StatusOK, gin.H{
		"fingerprints": ids,
		"count":        len(ids),
	})
}

// RevokeFingerprint handles DELETE /api/v1/auth/fingerprints/:id
// Revokes a specific fingerprint token.
func RevokeFingerprint(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint ID required"})
		return
	}

	ids := auth.ListUserFingerprints(username.(string))
	found := false
	for _, fid := range ids {
		if fid == id {
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "fingerprint not found"})
		return
	}

	auth.InvalidateFingerprint(id)
	c.JSON(http.StatusOK, gin.H{"message": "fingerprint revoked"})
}

// RevokeAllFingerprints handles DELETE /api/v1/auth/fingerprints
// Revokes all fingerprint tokens for the current user.
func RevokeAllFingerprints(c *gin.Context) {
	username, _ := c.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	auth.InvalidateUserFingerprints(username.(string))
	c.JSON(http.StatusOK, gin.H{"message": "all fingerprints revoked"})
}
