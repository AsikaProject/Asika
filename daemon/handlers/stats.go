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

// BottleneckStats holds identified bottleneck PRs.
type BottleneckStats struct {
	PeriodDays          int              `json:"period_days"`
	ReopenedPRs         []BottleneckPR   `json:"reopened_prs"`
	LongReviewPRs       []BottleneckPR   `json:"long_review_prs"`
	StalePRs            []BottleneckPR   `json:"stale_prs"`
	FrequentRejectPRs   []BottleneckPR   `json:"frequent_reject_prs"`
	AvgLeadTimeHrs      float64          `json:"avg_lead_time_hours"`
	P90LeadTimeHrs      float64          `json:"p90_lead_time_hours"`
	P95LeadTimeHrs      float64          `json:"p95_lead_time_hours"`
}

// BottleneckPR represents a single bottleneck PR entry.
type BottleneckPR struct {
	PRID        string  `json:"pr_id"`
	Title       string  `json:"title"`
	Author      string  `json:"author"`
	RepoGroup   string  `json:"repo_group"`
	LeadTimeHrs float64 `json:"lead_time_hours"`
	ReopenCount int     `json:"reopen_count,omitempty"`
	RejectCount int     `json:"reject_count,omitempty"`
	AgeDays     float64 `json:"age_days,omitempty"`
}

// GetBottleneckStats handles GET /api/v1/stats/bottlenecks
func GetBottleneckStats(c *gin.Context) {
	periodDays := 30
	if p := c.Query("period"); p != "" {
		if n, err := fmt.Sscanf(p, "%d", &periodDays); err != nil || n != 1 || periodDays <= 0 {
			periodDays = 30
		}
	}
	cutoff := time.Now().AddDate(0, 0, -periodDays)

	type prAnalysis struct {
		pr          models.PRRecord
		reopenCount int
		rejectCount int
		reviewReqCount int
	}

	analyses := make(map[string]*prAnalysis)
	var leadTimes []float64

	db.ForEach(db.BucketPRs, func(key, value []byte) error {
		var pr models.PRRecord
		if err := json.Unmarshal(value, &pr); err != nil {
			return nil
		}
		pa := &prAnalysis{pr: pr}
		for _, ev := range pr.Events {
			if ev.Timestamp.Before(cutoff) {
				continue
			}
			switch ev.Action {
			case "reopened":
				pa.reopenCount++
			case "changes_requested", "review_rejected":
				pa.rejectCount++
			case "review_requested":
				pa.reviewReqCount++
			}
		}
		analyses[pr.ID] = pa

		if pr.State == "merged" && !pr.CreatedAt.IsZero() && !pr.MergedAt.IsZero() {
			lt := pr.MergedAt.Sub(pr.CreatedAt).Hours()
			if lt > 0 {
				leadTimes = append(leadTimes, lt)
			}
		}
		return nil
	})

	avgLead := 0.0
	p90Lead := 0.0
	p95Lead := 0.0
	if len(leadTimes) > 0 {
		sort.Float64s(leadTimes)
		sum := 0.0
		for _, v := range leadTimes {
			sum += v
		}
		avgLead = sum / float64(len(leadTimes))
		p90Lead = leadTimes[int(float64(len(leadTimes))*0.9)]
		p95Lead = leadTimes[int(float64(len(leadTimes))*0.95)]
	}

	var reopenedPRs, longReviewPRs, stalePRs, frequentRejectPRs []BottleneckPR
	nowTime := time.Now()

	for _, pa := range analyses {
		pr := pa.pr
		lt := 0.0
		if !pr.CreatedAt.IsZero() && !pr.MergedAt.IsZero() && pr.MergedAt.After(pr.CreatedAt) {
			lt = pr.MergedAt.Sub(pr.CreatedAt).Hours()
		}

		bp := BottleneckPR{
			PRID:      pr.ID,
			Title:     pr.Title,
			Author:    pr.Author,
			RepoGroup: pr.RepoGroup,
		}

		if pa.reopenCount > 0 {
			bp.ReopenCount = pa.reopenCount
			bp.LeadTimeHrs = lt
			reopenedPRs = append(reopenedPRs, bp)
		}

		if pa.rejectCount >= 2 {
			bp.RejectCount = pa.rejectCount
			bp.LeadTimeHrs = lt
			frequentRejectPRs = append(frequentRejectPRs, bp)
		}

		if pr.State == "open" && !pr.CreatedAt.IsZero() {
			age := nowTime.Sub(pr.CreatedAt).Hours()
			if age > 48 {
				bp.AgeDays = age / 24
				bp.LeadTimeHrs = lt
				longReviewPRs = append(longReviewPRs, bp)
			}
		}

		if pr.State == "open" && !pr.CreatedAt.IsZero() {
			age := nowTime.Sub(pr.CreatedAt).Hours() / 24
			if age > float64(periodDays)*0.5 && pa.reviewReqCount > 0 {
				found := false
				for _, existing := range longReviewPRs {
					if existing.PRID == pr.ID {
						found = true
						break
					}
				}
				if !found {
					stalePRs = append(stalePRs, BottleneckPR{
						PRID:      pr.ID,
						Title:     pr.Title,
						Author:    pr.Author,
						RepoGroup: pr.RepoGroup,
						AgeDays:   age,
					})
				}
			}
		}
	}

	sort.Slice(reopenedPRs, func(i, j int) bool {
		return reopenedPRs[i].ReopenCount > reopenedPRs[j].ReopenCount
	})
	sort.Slice(longReviewPRs, func(i, j int) bool {
		return longReviewPRs[i].LeadTimeHrs > longReviewPRs[j].LeadTimeHrs
	})
	sort.Slice(stalePRs, func(i, j int) bool {
		return stalePRs[i].AgeDays > stalePRs[j].AgeDays
	})
	sort.Slice(frequentRejectPRs, func(i, j int) bool {
		return frequentRejectPRs[i].RejectCount > frequentRejectPRs[j].RejectCount
	})

	c.Header("Cache-Control", "private, max-age=60")
	c.JSON(http.StatusOK, BottleneckStats{
		PeriodDays:        periodDays,
		ReopenedPRs:       reopenedPRs,
		LongReviewPRs:     longReviewPRs,
		StalePRs:          stalePRs,
		FrequentRejectPRs: frequentRejectPRs,
		AvgLeadTimeHrs:    avgLead,
		P90LeadTimeHrs:    p90Lead,
		P95LeadTimeHrs:    p95Lead,
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
