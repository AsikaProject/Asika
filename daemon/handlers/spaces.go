package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/db"
	"asika/common/models"
)

func ListSpaces(c *gin.Context) {
	spaces, err := db.ListTeamSpaces()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list spaces"})
		return
	}
	if spaces == nil {
		spaces = []*models.TeamSpace{}
	}
	c.JSON(http.StatusOK, spaces)
}

func CreateSpace(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	username, _ := c.Get("username")
	space := &models.TeamSpace{
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now(),
		CreatedBy:   username.(string),
		RepoGroups:  []string{},
	}

	if err := db.PutTeamSpace(space); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create space"})
		return
	}

	db.PutSpaceMember(req.Name, username.(string), "space_admin")
	slog.Info("team space created", "name", req.Name, "by", username)
	c.JSON(http.StatusCreated, space)
}

func GetSpace(c *gin.Context) {
	name := c.Param("name")
	space, err := db.GetTeamSpace(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get space"})
		return
	}
	if space == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "space not found"})
		return
	}

	members, _ := db.GetSpaceMembers(name)
	space.Members = members
	c.JSON(http.StatusOK, space)
}

func DeleteSpace(c *gin.Context) {
	name := c.Param("name")
	if err := db.DeleteTeamSpace(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete space"})
		return
	}
	slog.Info("team space deleted", "name", name)
	c.JSON(http.StatusOK, gin.H{"message": "space deleted"})
}

func AddSpaceMember(c *gin.Context) {
	name := c.Param("name")
	var req struct {
		Username string `json:"username" binding:"required"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Role == "" {
		req.Role = "space_viewer"
	}

	space, err := db.GetTeamSpace(name)
	if err != nil || space == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "space not found"})
		return
	}

	if err := db.PutSpaceMember(name, req.Username, req.Role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add member"})
		return
	}
	slog.Info("space member added", "space", name, "user", req.Username, "role", req.Role)
	c.JSON(http.StatusOK, gin.H{"message": "member added"})
}

func RemoveSpaceMember(c *gin.Context) {
	name := c.Param("name")
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username required"})
		return
	}

	if err := db.RemoveSpaceMember(name, username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove member"})
		return
	}
	slog.Info("space member removed", "space", name, "user", username)
	c.JSON(http.StatusOK, gin.H{"message": "member removed"})
}

func GetSpaceMembers(c *gin.Context) {
	name := c.Param("name")
	members, err := db.GetSpaceMembers(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get members"})
		return
	}
	if members == nil {
		members = []models.SpaceMember{}
	}
	c.JSON(http.StatusOK, members)
}

func UpdateSpaceRepoGroups(c *gin.Context) {
	name := c.Param("name")
	var req struct {
		RepoGroups []string `json:"repo_groups"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	space, err := db.GetTeamSpace(name)
	if err != nil || space == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "space not found"})
		return
	}

	space.RepoGroups = req.RepoGroups
	if err := db.PutTeamSpace(space); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update space"})
		return
	}
	c.JSON(http.StatusOK, space)
}

func CheckSpaceAccess(c *gin.Context) {
	username, _ := c.Get("username")
	spaces, _ := db.GetUserSpaces(username.(string))
	spaceMap := make(map[string]bool)
	for _, s := range spaces {
		spaceMap[s] = true
	}

	var req struct {
		SpaceName string `json:"space_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if !spaceMap[req.SpaceName] {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this space"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "access granted"})
}
