package pr

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func BatchMergeHandler(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	var req struct {
		PRIDs  []string `json:"pr_ids"`
		Method string   `json:"method"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if len(req.PRIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_ids is required"})
		return
	}

	results := BatchMergePRs(req.PRIDs, repoGroup, req.Method)
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func BatchCherryPickHandler(c *gin.Context) {
	repoGroup := c.Param("repo_group")

	var req struct {
		PRIDs        []string `json:"pr_ids"`
		TargetBranch string   `json:"target_branch"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if len(req.PRIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pr_ids is required"})
		return
	}

	if strings.TrimSpace(req.TargetBranch) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_branch is required"})
		return
	}

	results := BatchCherryPickPRs(req.PRIDs, repoGroup, req.TargetBranch)
	c.JSON(http.StatusOK, gin.H{"results": results})
}
