package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
)

// FindPRByChangeID handles GET /api/v1/repos/:repo_group/prs/gerrit-change/:change_id
//
// Lookup is scoped to the authenticated user's repo group (enforced by the
// RequireRepoGroupAccess middleware on the prs group) to prevent cross-tenant
// information disclosure. Only PRs whose key starts with "<repoGroup>#" are
// scanned.
func FindPRByChangeID(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	changeID := c.Param("change_id")
	if changeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "change_id is required"})
		return
	}
	if repoGroup == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_group is required"})
		return
	}

	var foundPR *models.PRRecord
	var stopErr error
	prefix := fmt.Sprintf("%s#", repoGroup)
	err := db.BucketForEachPrefix(db.BucketPRs, prefix, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.GerritChangeID != "" && strings.EqualFold(pr.GerritChangeID, changeID) {
			foundPR = &pr
			stopErr = http.ErrAbortHandler
			return stopErr
		}
		return nil
	})

	if err != nil && err != stopErr {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search failed"})
		return
	}

	if foundPR == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found for change_id"})
		return
	}

	c.JSON(http.StatusOK, foundPR)
}
