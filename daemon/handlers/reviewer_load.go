package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"asika/daemon/reviewer"
)

// GetReviewerLoads handles GET /api/v1/reviewers/load
func GetReviewerLoads(c *gin.Context) {
	loads, err := reviewer.GetAllReviewerLoads()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, loads)
}

// GetReviewerLoad handles GET /api/v1/reviewers/load/:username
func GetReviewerLoad(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
		return
	}

	load, err := reviewer.GetReviewerLoad(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, load)
}
