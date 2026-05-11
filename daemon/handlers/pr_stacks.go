package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log/slog"

	"asika/common/db"
	"asika/common/models"
)

var prURLPattern = regexp.MustCompile(`https?://[^\s]+/(?:pull|merge_requests|pulls)/(\d+)`)

// DetectStackLinks scans a PR body for cross-platform PR references.
// Returns a list of StackMember for each referenced PR.
func DetectStackLinks(pr *models.PRRecord) []models.StackMember {
	if pr.Body == "" {
		return nil
	}

	matches := prURLPattern.FindAllStringSubmatch(pr.Body, -1)
	if len(matches) == 0 {
		return nil
	}

	var members []models.StackMember
	seen := make(map[string]bool)

	for _, m := range matches {
		url := m[1]
		prNum := 0
		fmt.Sscanf(m[1], "%d", &prNum)

		key := fmt.Sprintf("%s:%s:%d", pr.RepoGroup, pr.Platform, prNum)
		if seen[key] {
			continue
		}
		seen[key] = true

		platform := detectPlatformFromURL(url)
		repoGroup := detectRepoGroupFromURL(url, pr.RepoGroup)

		members = append(members, models.StackMember{
			PRID:      fmt.Sprintf("%s:%s:%d", repoGroup, platform, prNum),
			Platform:  platform,
			PRNumber:  prNum,
			RepoGroup: repoGroup,
			Stage:     0,
			HTMLURL:   url,
		})
	}

	return members
}

func detectPlatformFromURL(url string) string {
	switch {
	case strings.Contains(url, "github.com"):
		return "github"
	case strings.Contains(url, "gitlab.com"), strings.Contains(url, "gitlab"):
		return "gitlab"
	case strings.Contains(url, "gitea"), strings.Contains(url, "forgejo"):
		return "gitea"
	case strings.Contains(url, "bitbucket.org"):
		return "bitbucket"
	case strings.Contains(url, "codeberg.org"):
		return "codeberg"
	default:
		return "unknown"
	}
}

func detectRepoGroupFromURL(url, fallback string) string {
	return fallback
}

func FindStackByPR(prID string) (*models.PRStack, error) {
	stacks, err := db.ListPRStacks()
	if err != nil {
		return nil, err
	}
	for _, stack := range stacks {
		for _, m := range stack.Members {
			if m.PRID == prID {
				return stack, nil
			}
		}
	}
	return nil, nil
}

func CreateStack(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	username, _ := c.Get("username")
	stack := &models.PRStack{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Author:      username.(string),
		State:       "open",
		Members:     []models.StackMember{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := db.PutPRStack(stack); err != nil {
		slog.Error("failed to create stack", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create stack"})
		return
	}

	slog.Info("PR stack created", "id", stack.ID, "name", stack.Name, "by", username)
	c.JSON(http.StatusCreated, stack)
}

func ListStacks(c *gin.Context) {
	stacks, err := db.ListPRStacks()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list stacks"})
		return
	}
	if stacks == nil {
		stacks = []*models.PRStack{}
	}
	c.JSON(http.StatusOK, stacks)
}

func GetStack(c *gin.Context) {
	id := c.Param("id")
	stack, err := db.GetPRStack(id)
	if err != nil || stack == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stack not found"})
		return
	}
	c.JSON(http.StatusOK, stack)
}

func DeleteStack(c *gin.Context) {
	id := c.Param("id")
	if err := db.DeletePRStack(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete stack"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "stack deleted"})
}

func AddStackMember(c *gin.Context) {
	stackID := c.Param("id")
	var req struct {
		PRID      string `json:"pr_id" binding:"required"`
		Platform  string `json:"platform"`
		PRNumber  int    `json:"pr_number"`
		RepoGroup string `json:"repo_group"`
		Stage     int    `json:"stage"`
		HTMLURL   string `json:"html_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	stack, err := db.GetPRStack(stackID)
	if err != nil || stack == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stack not found"})
		return
	}

	member := models.StackMember{
		PRID:      req.PRID,
		Platform:  req.Platform,
		PRNumber:  req.PRNumber,
		RepoGroup: req.RepoGroup,
		Stage:     req.Stage,
		HTMLURL:   req.HTMLURL,
		State:     "open",
	}
	stack.Members = append(stack.Members, member)
	stack.UpdatedAt = time.Now()
	stack.State = calculateStackState(stack)

	if err := db.PutPRStack(stack); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update stack"})
		return
	}

	c.JSON(http.StatusOK, stack)
}

func RemoveStackMember(c *gin.Context) {
	stackID := c.Param("id")
	prID := c.Param("pr_id")

	stack, err := db.GetPRStack(stackID)
	if err != nil || stack == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stack not found"})
		return
	}

	var newMembers []models.StackMember
	for _, m := range stack.Members {
		if m.PRID != prID {
			newMembers = append(newMembers, m)
		}
	}
	stack.Members = newMembers
	stack.UpdatedAt = time.Now()
	stack.State = calculateStackState(stack)

	if err := db.PutPRStack(stack); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update stack"})
		return
	}

	c.JSON(http.StatusOK, stack)
}

func calculateStackState(stack *models.PRStack) string {
	if len(stack.Members) == 0 {
		return "open"
	}
	allMerged := true
	anyFailed := false
	for _, m := range stack.Members {
		if m.State != "merged" {
			allMerged = false
		}
		if m.State == "failed" {
			anyFailed = true
		}
	}
	if allMerged {
		return "merged"
	}
	if anyFailed {
		return "failed"
	}
	return "partial"
}

func UpdateStackMemberState(c *gin.Context) {
	stackID := c.Param("id")
	prID := c.Param("pr_id")
	var req struct {
		State string `json:"state" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	stack, err := db.GetPRStack(stackID)
	if err != nil || stack == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stack not found"})
		return
	}

	for i := range stack.Members {
		if stack.Members[i].PRID == prID {
			stack.Members[i].State = req.State
			break
		}
	}
	stack.UpdatedAt = time.Now()
	stack.State = calculateStackState(stack)

	if err := db.PutPRStack(stack); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update stack"})
		return
	}

	c.JSON(http.StatusOK, stack)
}

func UpdateStackMemberStateOnMerge(pr *models.PRRecord) {
	if pr.ID == "" {
		return
	}
	stack, err := FindStackByPR(pr.ID)
	if err != nil || stack == nil {
		return
	}
	updated := false
	for i := range stack.Members {
		if stack.Members[i].PRID == pr.ID {
			stack.Members[i].State = "merged"
			updated = true
			break
		}
	}
	if !updated {
		return
	}
	stack.UpdatedAt = time.Now()
	stack.State = calculateStackState(stack)
	db.PutPRStack(stack)
	slog.Info("stack member state updated", "stack_id", stack.ID, "pr_id", pr.ID, "new_state", stack.State)
}

func SyncStackFromPR(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	var pr *models.PRRecord
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var record models.PRRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return nil
		}
		if record.RepoGroup == repoGroup && record.ID == prID {
			pr = &record
		}
		return nil
	})

	if pr == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	members := DetectStackLinks(pr)
	if len(members) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "no stack links found in PR description"})
		return
	}

	existingStack, _ := FindStackByPR(pr.ID)
	if existingStack == nil {
		username, _ := c.Get("username")
		existingStack = &models.PRStack{
			ID:        uuid.New().String(),
			Name:      fmt.Sprintf("Stack for PR #%d", pr.PRNumber),
			Author:    username.(string),
			State:     "open",
			Members:   []models.StackMember{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	existingStack.Members = append(existingStack.Members, members...)
	existingStack.UpdatedAt = time.Now()
	existingStack.State = calculateStackState(existingStack)

	if err := db.PutPRStack(existingStack); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save stack"})
		return
	}

	c.JSON(http.StatusOK, existingStack)
}
