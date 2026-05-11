package models

// AuthorStats holds per-author contribution metrics.
type AuthorStats struct {
	Author           string  `json:"author"`
	PRsOpened        int     `json:"prs_opened"`
	PRsMerged        int     `json:"prs_merged"`
	PRsReviewed      int     `json:"prs_reviewed"`
	AvgLeadTimeHrs   float64 `json:"avg_lead_time_hours"`
	AvgReviewTimeHrs float64 `json:"avg_review_time_hours"`
}

// TeamStats holds aggregated team metrics.
type TeamStats struct {
	PeriodDays      int           `json:"period_days"`
	TotalAuthors    int           `json:"total_authors"`
	Authors         []AuthorStats `json:"authors"`
	TopContributors []AuthorStats `json:"top_contributors"`
}

// BottleneckStats holds identified bottleneck PRs.
type BottleneckStats struct {
	PeriodDays        int            `json:"period_days"`
	ReopenedPRs       []BottleneckPR `json:"reopened_prs"`
	LongReviewPRs     []BottleneckPR `json:"long_review_prs"`
	StalePRs          []BottleneckPR `json:"stale_prs"`
	FrequentRejectPRs []BottleneckPR `json:"frequent_reject_prs"`
	AvgLeadTimeHrs    float64        `json:"avg_lead_time_hours"`
	P90LeadTimeHrs    float64        `json:"p90_lead_time_hours"`
	P95LeadTimeHrs    float64        `json:"p95_lead_time_hours"`
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
