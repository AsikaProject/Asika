package server

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"github.com/gin-gonic/gin"
)

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
		// Try index-based lookup first
		data, err := db.GetPRByIndex(prID, repoGroup, 0)
		if err == nil && data != nil {
			var rec models.PRRecord
			if json.Unmarshal(data, &rec) == nil {
				platform = rec.Platform
			}
		}
		// Also try by repo_group:prNumber if prID looks like a number
		if platform == "" {
			if prNum := parsePRNumber(prID); prNum > 0 {
				data, err = db.GetPRByIndex("", repoGroup, prNum)
				if err == nil && data != nil {
					var rec models.PRRecord
					if json.Unmarshal(data, &rec) == nil {
						platform = rec.Platform
					}
				}
			}
		}
		// Fallback to full scan only if index miss
		if platform == "" {
			db.ForEach(db.BucketPRs, func(key, value []byte) error {
				var rec models.PRRecord
				if json.Unmarshal(value, &rec) == nil && rec.ID == prID && rec.RepoGroup == repoGroup {
					platform = rec.Platform
					return errStopForEach
				}
				return nil
			})
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

func parsePRNumber(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
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
