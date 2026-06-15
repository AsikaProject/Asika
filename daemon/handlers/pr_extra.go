package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"log/slog"

	"asika/common/config"
	"asika/common/db"
	"asika/common/models"
	"asika/daemon/handlers/pr"
)

// GetApprovalStatus handles GET /api/v1/repos/:repo_group/prs/:pr_id/approval-status
// Returns detailed approval requirements for a PR, including path-based rules.
func GetApprovalStatus(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var prRecord models.PRRecord
	if json.Unmarshal(data, &prRecord) != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	client := pr.GetClientForGroup(group, prRecord.Platform)
	if client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "platform client not available"})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot resolve repo"})
		return
	}

	ctx := context.Background()
	approvalStatus, err := client.GetApprovals(ctx, owner, repo, prRecord.PRNumber)
	if err != nil {
		slog.Warn("failed to fetch approvals", "error", err, "pr_id", prID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch approvals"})
		return
	}

	approvers := approvalStatus.Approvers
	blockers := approvalStatus.Blockers

	result := gin.H{
		"pr_id":              prID,
		"repo_group":         repoGroup,
		"platform":           prRecord.Platform,
		"required_approvals": group.MergeQueue.RequiredApprovals,
		"current_approvals":  len(approvers),
		"approvers":          approvers,
		"blockers":           blockers,
		"path_rules":         make([]gin.H, 0),
	}

	if len(group.ApprovalRules) > 0 {
		files, diffErr := client.GetDiffFiles(ctx, owner, repo, prRecord.PRNumber)
		if diffErr != nil {
			slog.Error("failed to get diff files for approval check", "error", diffErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get diff files for approval check"})
			return
		}

		approvalSet := make(map[string]bool, len(approvers))
		for _, a := range approvers {
			approvalSet[a] = true
		}

		for _, rule := range group.ApprovalRules {
			ruleResult := gin.H{
				"pattern":   rule.Pattern,
				"approvers": rule.Approvers,
				"min_count": rule.MinCount,
				"reason":    rule.Reason,
			}

			if files != nil {
				matched := false
				for _, f := range files {
					if matchFilePattern(rule.Pattern, f) {
						matched = true
						break
					}
				}
				ruleResult["matched"] = matched

				if matched {
					minCount := rule.MinCount
					if minCount <= 0 {
						minCount = 1
					}
					matchedApprovers := 0
					for _, approver := range rule.Approvers {
						if approvalSet[approver] {
							matchedApprovers++
						}
					}
					ruleResult["matched_approvers"] = matchedApprovers
					ruleResult["satisfied"] = matchedApprovers >= minCount
				} else {
					ruleResult["satisfied"] = true
				}
			}

			result["path_rules"] = append(result["path_rules"].([]gin.H), ruleResult)
		}
	}

	c.JSON(http.StatusOK, result)
}

// CheckTemplate handles GET /api/v1/repos/:repo_group/prs/:pr_id/template-check
// Checks PR body against template enforcement rules.
func CheckTemplate(c *gin.Context) {
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	if !group.PRTemplate.Enabled {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "message": "template enforcement not enabled"})
		return
	}

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var prRecord models.PRRecord
	if json.Unmarshal(data, &prRecord) != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	body := prRecord.Body
	if body == "" {
		client := pr.GetClientForGroup(group, prRecord.Platform)
		if client != nil {
			owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)
			if owner != "" && repo != "" {
				if fetched, err := client.GetPRBody(c.Request.Context(), owner, repo, prRecord.PRNumber); err == nil {
					body = fetched
				}
			}
		}
	}

	tpl := group.PRTemplate
	result := gin.H{
		"enabled":           true,
		"require_body":      tpl.RequireBody,
		"require_checklist": tpl.RequireChecklist,
		"block_merge":       tpl.BlockMerge,
	}

	exempt := false
	for _, label := range prRecord.Labels {
		for _, ex := range tpl.ExemptLabels {
			if label == ex {
				exempt = true
				break
			}
		}
		if exempt {
			break
		}
	}
	result["exempt"] = exempt

	if tpl.RequireBody {
		bodyOK := body != "" && len(body) >= 10
		result["body_ok"] = bodyOK
		result["body_length"] = len(body)
	}

	if tpl.RequireChecklist {
		complete, total, unchecked := validateChecklistInline(body)
		result["checklist_complete"] = complete
		result["checklist_total"] = total
		result["checklist_unchecked"] = unchecked
	}

	c.JSON(http.StatusOK, result)
}

// MarkReady handles POST /api/v1/repos/:repo_group/prs/:pr_id/ready
// MarkReady handles POST /api/v1/repos/:repo_group/prs/:pr_id/ready
// Marks a draft PR as ready for review and optionally enqueues it.
func MarkReady(c *gin.Context) {
	username := c.GetString("username")
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var prRecord models.PRRecord
	if json.Unmarshal(data, &prRecord) != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	if !prRecord.IsDraft {
		c.JSON(http.StatusBadRequest, gin.H{"error": "PR is not a draft"})
		return
	}

	client := pr.GetClientForGroup(group, prRecord.Platform)
	if client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "platform client not available"})
		return
	}

	owner, repo := config.GetOwnerRepoFromGroup(group, prRecord.Platform)
	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot resolve repo"})
		return
	}

	prRecord.IsDraft = false
	updated, marshalErr := json.Marshal(prRecord)
	if marshalErr != nil {
		slog.Error("failed to marshal PR record", "error", marshalErr, "pr_id", prID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal PR record"})
		return
	}
	dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, prRecord.Platform, prRecord.PRNumber)
	if putErr := db.PutPRWithIndex(dbKey, updated, prRecord.ID, prRecord.RepoGroup, prRecord.PRNumber); putErr != nil {
		slog.Error("failed to update PR in database", "error", putErr, "pr_id", prID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update PR in database"})
		return
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   "PR marked as ready for review",
		Actor:     username,
		RepoGroup: repoGroup,
		PRNumber:  prRecord.PRNumber,
		Platform:  prRecord.Platform,
		Action:    "mark_ready",
	})

	enqueued := false
	if group.DraftPR.AutoEnqueueOnReady && !group.DraftPR.SkipQueue {
		queueMgr := pr.GetQueueMgr()
		if queueMgr != nil {
			if err := queueMgr.AddToQueue(&prRecord); err != nil {
				slog.Warn("failed to auto-enqueue PR after mark ready", "error", err, "pr_id", prID)
			} else {
				enqueued = true
				pr.TriggerQueueCheck()
			}
		}
	}

	slog.Info("PR marked as ready", "pr_id", prID, "repo_group", repoGroup, "enqueued", enqueued, "by", username)
	c.JSON(http.StatusOK, gin.H{
		"message":  "PR marked as ready for review",
		"pr_id":    prID,
		"enqueued": enqueued,
	})

	_ = client
	_ = owner
	_ = repo
}

// MarkDraft handles POST /api/v1/repos/:repo_group/prs/:pr_id/draft
// Marks a PR as draft (work in progress).
func MarkDraft(c *gin.Context) {
	username := c.GetString("username")
	repoGroup := c.Param("repo_group")
	prID := c.Param("pr_id")

	cfg := config.Current()
	group := config.GetRepoGroupByName(cfg, repoGroup)
	if group == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo group not found"})
		return
	}

	data, err := db.GetPRByIndex(prID, repoGroup, 0)
	if err != nil || data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}
	var prRecord models.PRRecord
	if json.Unmarshal(data, &prRecord) != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "PR not found"})
		return
	}

	if prRecord.IsDraft {
		c.JSON(http.StatusBadRequest, gin.H{"error": "PR is already a draft"})
		return
	}

	prRecord.IsDraft = true
	updated, marshalErr := json.Marshal(prRecord)
	if marshalErr != nil {
		slog.Error("failed to marshal PR record", "error", marshalErr, "pr_id", prID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal PR record"})
		return
	}
	dbKey := fmt.Sprintf("%s#%s#%d", repoGroup, prRecord.Platform, prRecord.PRNumber)
	if putErr := db.PutPRWithIndex(dbKey, updated, prRecord.ID, prRecord.RepoGroup, prRecord.PRNumber); putErr != nil {
		slog.Error("failed to update PR in database", "error", putErr, "pr_id", prID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update PR in database"})
		return
	}

	db.AppendAuditLogEx(models.AuditLog{
		Level:     "info",
		Message:   "PR marked as draft",
		Actor:     username,
		RepoGroup: repoGroup,
		PRNumber:  prRecord.PRNumber,
		Platform:  prRecord.Platform,
		Action:    "mark_draft",
	})

	removedFromQueue := false
	queueMgr := pr.GetQueueMgr()
	if queueMgr != nil {
		if err := queueMgr.RemoveFromQueue(repoGroup, prID); err != nil {
			slog.Warn("failed to remove PR from queue after mark draft", "error", err, "pr_id", prID)
		} else {
			removedFromQueue = true
		}
	}

	slog.Info("PR marked as draft", "pr_id", prID, "repo_group", repoGroup, "by", username, "removed_from_queue", removedFromQueue)
	c.JSON(http.StatusOK, gin.H{
		"message":            "PR marked as draft",
		"pr_id":              prID,
		"removed_from_queue": removedFromQueue,
	})
}

func matchFilePattern(pattern, file string) bool {
	if matched, err := globMatch(pattern, file); err == nil && matched {
		return true
	}
	return false
}

func globMatch(pattern, name string) (bool, error) {
	if pattern == "" {
		return false, nil
	}
	pIdx := 0
	nIdx := 0
	pLen := len(pattern)
	nLen := len(name)

	for pIdx < pLen && nIdx < nLen {
		switch pattern[pIdx] {
		case '*':
			for pIdx < pLen && pattern[pIdx] == '*' {
				pIdx++
			}
			if pIdx >= pLen {
				return true, nil
			}
			for i := nIdx; i < nLen; i++ {
				if m, _ := globMatch(pattern[pIdx:], name[i:]); m {
					return true, nil
				}
			}
			return false, nil
		case '?':
			pIdx++
			nIdx++
		case '[':
			pIdx++
			if pIdx >= pLen {
				return false, nil
			}
			negate := false
			if pattern[pIdx] == '!' || pattern[pIdx] == '^' {
				negate = true
				pIdx++
			}
			found := false
			for pIdx < pLen && pattern[pIdx] != ']' {
				if pIdx+2 < pLen && pattern[pIdx+1] == '-' {
					if name[nIdx] >= pattern[pIdx] && name[nIdx] <= pattern[pIdx+2] {
						found = true
					}
					pIdx += 3
				} else {
					if name[nIdx] == pattern[pIdx] {
						found = true
					}
					pIdx++
				}
			}
			if pIdx < pLen {
				pIdx++
			}
			if found == negate {
				return false, nil
			}
			nIdx++
		default:
			if pattern[pIdx] != name[nIdx] {
				return false, nil
			}
			pIdx++
			nIdx++
		}
	}
	for pIdx < pLen && pattern[pIdx] == '*' {
		pIdx++
	}
	return pIdx >= pLen && nIdx >= nLen, nil
}

var checklistInlinePattern = regexp.MustCompile(`(?m)^\s*[-*]\s+\[([ x])\]`)

func validateChecklistInline(body string) (complete bool, total int, unchecked int) {
	matches := checklistInlinePattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return true, 0, 0
	}
	total = len(matches)
	for _, m := range matches {
		if m[1] != "x" && m[1] != "X" {
			unchecked++
		}
	}
	return unchecked == 0, total, unchecked
}
