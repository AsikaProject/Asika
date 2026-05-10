package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"asika/common/db"
	"asika/common/models"
)

// StatsResponse holds DORA metrics and general stats
type StatsResponse struct {
	DeploymentFrequency float64        `json:"deployment_frequency"`
	LeadTimeHours       float64        `json:"lead_time_hours"`
	ChangeFailureRate   float64        `json:"change_failure_rate"`
	MTTRHours           float64        `json:"mttr_hours"`
	TotalPRs            int            `json:"total_prs"`
	MergedPRs           int            `json:"merged_prs"`
	OpenPRs             int            `json:"open_prs"`
	ClosedPRs           int            `json:"closed_prs"`
	SpamPRs             int            `json:"spam_prs"`
	QueueItems          int            `json:"queue_items"`
	FailedQueueItems    int            `json:"failed_queue_items"`
	SyncFailures        int            `json:"sync_failures"`
	PeriodDays          int            `json:"period_days"`
	PRsByRepoGroup      map[string]int `json:"prs_by_repo_group"`
	PRsByPlatform       map[string]int `json:"prs_by_platform"`
	MergesByDay         map[string]int `json:"merges_by_day"`
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

	prsByRepoGroup := make(map[string]int)
	prsByPlatform := make(map[string]int)
	mergesByDay := make(map[string]int)

	var allPRs []models.PRRecord
	var mergedPRs []models.PRRecord
	var failedQueueItems, totalQueueItems, syncFailures int
	openCount, closedCount, spamCount := 0, 0, 0

	// Single pass: scan PRs
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

	// Single pass: scan queue items (count total + failed)
	db.ForEach(db.BucketQueueItems, func(key, value []byte) error {
		var item models.QueueItem
		if err := json.Unmarshal(value, &item); err != nil {
			return nil
		}
		totalQueueItems++
		if item.Status == "failed" {
			failedQueueItems++
		}
		return nil
	})

	// Single pass: scan sync history
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

	// Single pass: scan logs for MTTR
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

	leadTimeSum := 0.0
	leadTimeCount := 0

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
				d := pr.MergedAt.Sub(pr.CreatedAt)
				if d > 0 {
					leadTimeSum += d.Hours()
					leadTimeCount++
				}
			}
		}
	}

	totalMergesInPeriod := 0
	for _, count := range mergesByDay {
		totalMergesInPeriod += count
	}
	deploymentFreq := 0.0
	if periodDays > 0 {
		deploymentFreq = float64(totalMergesInPeriod) / float64(periodDays)
	}

	avgLeadTime := 0.0
	if leadTimeCount > 0 {
		avgLeadTime = leadTimeSum / float64(leadTimeCount)
	}

	totalAttempts := failedQueueItems + len(mergedPRs)
	failureRate := 0.0
	if totalAttempts > 0 {
		failureRate = float64(failedQueueItems) / float64(totalAttempts)
	}

	mttr := 0.0
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
		QueueItems:          totalQueueItems,
		FailedQueueItems:    failedQueueItems,
		SyncFailures:        syncFailures,
		PeriodDays:          periodDays,
		PRsByRepoGroup:      prsByRepoGroup,
		PRsByPlatform:       prsByPlatform,
		MergesByDay:         mergesByDay,
	}

	c.Header("Cache-Control", "private, max-age=30")
	c.JSON(http.StatusOK, resp)
}

// GetTeamStats handles GET /api/v1/stats/team
func GetTeamStats(c *gin.Context) {
	periodDays := 30
	if p := c.Query("period"); p != "" {
		if n, err := fmt.Sscanf(p, "%d", &periodDays); err != nil || n != 1 || periodDays <= 0 {
			periodDays = 30
		}
	}
	cutoff := time.Now().AddDate(0, 0, -periodDays)

	type authorData struct {
		opened       int
		merged       int
		leadTimeSum  float64
		leadTimeCnt  int
		reviewEvents int
	}

	authors := make(map[string]*authorData)

	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		if pr.Author == "" {
			return nil
		}
		ad, ok := authors[pr.Author]
		if !ok {
			ad = &authorData{}
			authors[pr.Author] = ad
		}
		if pr.CreatedAt.After(cutoff) {
			ad.opened++
		}
		if pr.State == "merged" && !pr.MergedAt.IsZero() && pr.MergedAt.After(cutoff) {
			ad.merged++
			if !pr.CreatedAt.IsZero() {
				d := pr.MergedAt.Sub(pr.CreatedAt)
				if d > 0 {
					ad.leadTimeSum += d.Hours()
					ad.leadTimeCnt++
				}
			}
		}
		for _, ev := range pr.Events {
			if ev.Action == "approved" && ev.Timestamp.After(cutoff) {
				reviewerAd, ok := authors[ev.Actor]
				if !ok {
					reviewerAd = &authorData{}
					authors[ev.Actor] = reviewerAd
				}
				reviewerAd.reviewEvents++
			}
		}
		return nil
	})

	stats := make([]models.AuthorStats, 0, len(authors))
	for name, ad := range authors {
		avgLead := 0.0
		if ad.leadTimeCnt > 0 {
			avgLead = ad.leadTimeSum / float64(ad.leadTimeCnt)
		}
		stats = append(stats, models.AuthorStats{
			Author:           name,
			PRsOpened:        ad.opened,
			PRsMerged:        ad.merged,
			PRsReviewed:      ad.reviewEvents,
			AvgLeadTimeHrs:   avgLead,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].PRsMerged > stats[j].PRsMerged
	})

	topN := 5
	if len(stats) < topN {
		topN = len(stats)
	}

	c.Header("Cache-Control", "private, max-age=60")
	c.JSON(http.StatusOK, models.TeamStats{
		PeriodDays:      periodDays,
		TotalAuthors:    len(stats),
		Authors:         stats,
		TopContributors: stats[:topN],
	})
}

// GetReportHistory handles GET /api/v1/reports
func GetReportHistory(c *gin.Context) {
	limit := 20
	if l := c.Query("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err != nil || n != 1 || limit <= 0 {
			limit = 20
		}
	}
	entries, err := db.ListReportHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list report history"})
		return
	}
	c.Header("Cache-Control", "private, max-age=30")
	c.JSON(http.StatusOK, entries)
}
