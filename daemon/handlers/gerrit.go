package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
)

// FindPRByChangeID handles GET /api/v1/gerrit/change/:change_id
func FindPRByChangeID(c *gin.Context) {
	changeID := c.Param("change_id")
	if changeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "change_id is required"})
		return
	}

	var foundPR *models.PRRecord
	var stopErr error
	err := db.ForEach(db.BucketPRs, func(key, value []byte) error {
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
