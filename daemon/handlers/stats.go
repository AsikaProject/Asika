package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
)

// StatsResponse holds DORA metrics and general stats
type StatsResponse struct {
	DeploymentFrequency  float64            `json:"deployment_frequency"`   // merges per day
	LeadTimeHours        float64            `json:"lead_time_hours"`        // avg hours from PR open to merge
	ChangeFailureRate    float64            `json:"change_failure_rate"`   // failed / total merges
	MTTRHours            float64            `json:"mttr_hours"`             // avg hours from failure to recovery
	TotalPRs             int                `json:"total_prs"`
	MergedPRs            int                `json:"merged_prs"`
	OpenPRs              int                `json:"open_prs"`
	ClosedPRs            int                `json:"closed_prs"`
	SpamPRs              int                `json:"spam_prs"`
	QueueItems           int                `json:"queue_items"`
	FailedQueueItems     int                `json:"failed_queue_items"`
	SyncFailures         int                `json:"sync_failures"`
	PeriodDays           int                `json:"period_days"`
	PRsByRepoGroup       map[string]int     `json:"prs_by_repo_group"`
	PRsByPlatform        map[string]int     `json:"prs_by_platform"`
	MergesByDay          map[string]int     `json:"merges_by_day"` // YYYY-MM-DD -> count
}

// GetStats handles GET /api/v1/stats
func GetStats(c *gin.Context) {
	periodDays := 30
	if p := c.Query("period"); p != "" {
		if n, err := fmt.Sscanf(p, "%d", &periodDays); err != nil || n != 1 || periodDays <= 0 {
			periodDays = 30
		}
	}

	cutoff := time.Now().AddDate(0, 0, -periodDays)
	var allPRs []models.PRRecord
	var mergedPRs []models.PRRecord
	var failedQueueItems int
	var syncFailures int
	prsByRepoGroup := make(map[string]int)
	prsByPlatform := make(map[string]int)
	mergesByDay := make(map[string]int)

	// Scan PRs
	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		allPRs = append(allPRs, pr)
		prsByRepoGroup[pr.RepoGroup]++
		prsByPlatform[pr.Platform]++
		return nil
	})

	// Scan queue items
	db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		if item.Status == "failed" {
			failedQueueItems++
		}
		return nil
	})

	// Scan sync history
	db.ForEach(db.BucketSyncHistory, func(key, value []byte) error {
		var record models.SyncRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return nil
		}
		if record.Status == "failed" {
			syncFailures++
		}
		return nil
	})

	leadTimeSum := 0.0
	leadTimeCount := 0
	openCount, closedCount, spamCount := 0, 0, 0

	for _, pr := range allPRs {
		switch pr.State {
		case "open":
			openCount++
		case "closed":
			closedCount++
		case "spam":
			spamCount++
		case "merged":
			mergedPRs = append(mergedPRs, pr)
			if !pr.MergedAt.IsZero() && pr.MergedAt.After(cutoff) {
				day := pr.MergedAt.Format("2006-01-02")
				mergesByDay[day]++
			}
			if !pr.CreatedAt.IsZero() && !pr.MergedAt.IsZero() {
				leadTimeSum += pr.MergedAt.Sub(pr.CreatedAt).Hours()
				leadTimeCount++
			}
		}
	}

	// Deployment frequency: merges per day over the period
	totalMergesInPeriod := 0
	for _, count := range mergesByDay {
		totalMergesInPeriod +=
			count
	}
	deploymentFreq := 0.0
	if periodDays > 0 {
		deploymentFreq = float64(totalMergesInPeriod) / float64(periodDays)
	}

	// Lead time
	avgLeadTime := 0.0
	if leadTimeCount > 0 {
		avgLeadTime = leadTimeSum / float64(leadTimeCount)
	}

	// Change failure rate: failed queue items / (failed + merged)
	totalAttempts := failedQueueItems + len(mergedPRs)
	failureRate := 0.0
	if totalAttempts > 0 {
		failureRate = float64(failedQueueItems) / float64(totalAttempts)
	}

	// MTTR: average time from a failed queue item to the next successful merge
	// Simplified: use audit logs to find error -> recovery patterns
	mttr := 0.0
	{
		var errorTimes []time.Time
		var recoveryTimes []time.Time
		db.ForEach(db.BucketLogs, func(key, value []byte) error {
			var log models.AuditLog
			if err := json.Unmarshal(value, &log); err != nil {
				return nil
			}
			if log.Level == "error" {
				errorTimes = append(errorTimes, log.Timestamp)
			}
			if log.Level == "info" && log.Message == "merge succeeded" {
				recoveryTimes = append(recoveryTimes, log.Timestamp)
			}
			return nil
		})
		if len(errorTimes) > 0 && len(recoveryTimes) > 0 {
			var totalRestore time.Duration
			var restoreCount int
			for _, et := range errorTimes {
				for _, rt := range recoveryTimes {
					if rt.After(et) {
						totalRestore += rt.Sub(et)
						restoreCount++
						break
					}
				}
			}
			if restoreCount > 0 {
				mttr = totalRestore.Hours() / float64(restoreCount)
			}
		}
	}

	resp := StatsResponse{
		DeploymentFrequency: deploymentFreq,
		LeadTimeHours:       avgLeadTime,
		ChangeFailureRate:   failureRate,
		MTTRHours:           mttr,
		TotalPRs:            len(allPRs),
		MergedPRs:           len(mergedPRs),
		OpenPRs:             openCount,
		ClosedPRs:           closedCount,
		SpamPRs:             spamCount,
		QueueItems:          0, // filled below
		FailedQueueItems:    failedQueueItems,
		SyncFailures:        syncFailures,
		PeriodDays:          periodDays,
		PRsByRepoGroup:      prsByRepoGroup,
		PRsByPlatform:       prsByPlatform,
		MergesByDay:         mergesByDay,
	}

	// Count total queue items
	db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		resp.QueueItems++
		return nil
	})

	c.JSON(http.StatusOK, resp)
}
