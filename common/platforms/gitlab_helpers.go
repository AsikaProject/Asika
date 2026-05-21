package platforms

import (
	"fmt"
	"strings"
	"time"

	"gitlab.com/gitlab-org/api/client-go"

	"asika/common/models"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func gitLabMRToRecord(mr *gitlab.MergeRequest) *models.PRRecord {
	labels := make([]string, 0)
	for _, l := range mr.Labels {
		labels = append(labels, l)
	}

	var author string
	if mr.Author != nil {
		author = mr.Author.Username
	}

	var createdAt, updatedAt time.Time
	if mr.CreatedAt != nil {
		createdAt = *mr.CreatedAt
	}
	if mr.UpdatedAt != nil {
		updatedAt = *mr.UpdatedAt
	}

	var mergedAt time.Time
	if mr.MergedAt != nil {
		mergedAt = *mr.MergedAt
	}

	return &models.PRRecord{
		ID:             fmt.Sprintf("%d", mr.ID),
		Platform:       "gitlab",
		PRNumber:       int(mr.IID),
		Title:          mr.Title,
		Author:         author,
		State:          gitLabState(mr.State),
		Labels:         labels,
		MergeCommitSHA: mr.MergeCommitSHA,
		SpamFlag:       false,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Events:         []models.PREvent{},
		IsDraft:        mr.Draft,
		HasConflict:    gitLabHasConflict(mr),
		HTMLURL:        mr.WebURL,
		MergedAt:       mergedAt,
		BranchInfo: &models.PRBranchInfo{
			HeadBranch: mr.SourceBranch,
			BaseBranch: mr.TargetBranch,
		},
	}
}

func gitLabBasicMRToRecord(mr *gitlab.BasicMergeRequest) *models.PRRecord {
	labels := make([]string, 0)
	for _, l := range mr.Labels {
		labels = append(labels, l)
	}

	var author string
	if mr.Author != nil {
		author = mr.Author.Username
	}

	var createdAt, updatedAt time.Time
	if mr.CreatedAt != nil {
		createdAt = *mr.CreatedAt
	}
	if mr.UpdatedAt != nil {
		updatedAt = *mr.UpdatedAt
	}

	return &models.PRRecord{
		ID:             fmt.Sprintf("%d", mr.ID),
		Platform:       "gitlab",
		PRNumber:       int(mr.IID),
		Title:          mr.Title,
		Author:         author,
		State:          gitLabState(mr.State),
		Labels:         labels,
		MergeCommitSHA: mr.MergeCommitSHA,
		SpamFlag:       false,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Events:         []models.PREvent{},
		IsDraft:        mr.Draft,
		HTMLURL:        mr.WebURL,
		BranchInfo: &models.PRBranchInfo{
			HeadBranch: mr.SourceBranch,
			BaseBranch: mr.TargetBranch,
		},
	}
}

func gitLabHasConflict(mr *gitlab.MergeRequest) bool {
	return mr.HasConflicts
}

func gitLabState(state string) string {
	switch strings.ToLower(state) {
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	case "opened":
		return "open"
	default:
		return state
	}
}
