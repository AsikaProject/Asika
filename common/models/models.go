package models

import "time"

func ParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}

type UserPermissions struct {
	CanApprove     bool `json:"can_approve" toml:"can_approve"`
	CanMerge       bool `json:"can_merge" toml:"can_merge"`
	CanClose       bool `json:"can_close" toml:"can_close"`
	CanReopen      bool `json:"can_reopen" toml:"can_reopen"`
	CanSpam        bool `json:"can_spam" toml:"can_spam"`
	CanManageQueue bool `json:"can_manage_queue" toml:"can_manage_queue"`
	CanRevert      bool `json:"can_revert" toml:"can_revert"`
}

type APIKey struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	KeyHash           string          `json:"key_hash"`
	KeyHMAC           string          `json:"key_hmac"`
	Role              string          `json:"role"`
	CreatedAt         time.Time       `json:"created_at"`
	CreatedBy         string          `json:"created_by"`
	LastUsedAt        time.Time       `json:"last_used_at"`
	AllowedRepoGroups []string        `json:"allowed_repo_groups"`
	AllowedRepos      []string        `json:"allowed_repos"`
	Permissions       UserPermissions `json:"permissions"`
}

type User struct {
	Username          string          `json:"username"`
	PasswordHash      string          `json:"password_hash"`
	Role              string          `json:"role"`
	CreatedAt         time.Time       `json:"created_at"`
	AllowedRepoGroups []string        `json:"allowed_repo_groups"`
	AllowedRepos      []string        `json:"allowed_repos"`
	Permissions       UserPermissions `json:"permissions"`
}

type RepoGroup struct {
	Name           string           `json:"name"`
	Mode           string           `json:"mode"`
	MirrorPlatform string           `json:"mirror_platform"`
	GitHub         string           `json:"github"`
	GitLab         string           `json:"gitlab"`
	Gitea          string           `json:"gitea"`
	Forgejo        string           `json:"forgejo"`
	Codeberg       string           `json:"codeberg"`
	Bitbucket      string           `json:"bitbucket"`
	Gerrit         string           `json:"gerrit"`
	DefaultBranch  string           `json:"default_branch"`
	BranchSync     string           `json:"branch_sync"`
	SyncTags       bool             `json:"sync_tags"`
	SyncPRState    bool             `json:"sync_pr_state"`
	HookPath       string           `json:"hookpath"`
	CIProvider     string           `json:"ci_provider"`
	MergeQueue     MergeQueueConfig `json:"merge_queue"`
	LabelRules     []LabelRule      `json:"label_rules,omitempty"`
	ReviewRules    []ReviewRule     `json:"review_rules,omitempty"`
}

type PRBranchInfo struct {
	BaseBranch          string `json:"base_branch"`
	HeadBranch          string `json:"head_branch"`
	HeadSHA             string `json:"head_sha"`
	MaintainerCanModify bool   `json:"maintainer_can_modify"`
}

type PRRecord struct {
	ID             string        `json:"id"`
	RepoGroup      string        `json:"repo_group"`
	Platform       string        `json:"platform"`
	PRNumber       int           `json:"pr_number"`
	Title          string        `json:"title"`
	Author         string        `json:"author"`
	State          string        `json:"state"`
	Labels         []string      `json:"labels"`
	MergeCommitSHA string        `json:"merge_commit_sha"`
	SpamFlag       bool          `json:"spam_flag"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	DiffFiles      []string      `json:"diff_files"`
	Events         []PREvent     `json:"events"`
	IsDraft        bool          `json:"is_draft"`
	HasConflict    bool          `json:"has_conflict"`
	IsApproved     bool          `json:"is_approved"`
	HTMLURL        string        `json:"html_url"`
	MergedAt       time.Time     `json:"merged_at"`
	BranchInfo     *PRBranchInfo `json:"branch_info,omitempty"`
	CloseReason    string        `json:"close_reason,omitempty"`
	Body           string        `json:"body,omitempty"`
}

type PREvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Detail    string    `json:"detail"`
}

type PRCommentPayload struct {
	CommentBody   string `json:"comment_body"`
	CommentAuthor string `json:"comment_author"`
}

type QueueItem struct {
	PRID              string        `json:"pr_id"`
	RepoGroup         string        `json:"repo_group"`
	Status            string        `json:"status"`
	AddedAt           time.Time     `json:"added_at"`
	LastChecked       time.Time     `json:"last_checked"`
	FailureReason     string        `json:"failure_reason,omitempty"`
	Criteria          MergeCriteria `json:"criteria"`
	ScheduleAt        time.Time     `json:"schedule_at,omitempty"`
	ValidationStatus  string        `json:"validation_status,omitempty"`
	ValidationStarted time.Time     `json:"validation_started,omitempty"`
	ValidationDetail  string        `json:"validation_detail,omitempty"`
	Space             string        `json:"space,omitempty"`
	Priority          int           `json:"priority,omitempty"`
	NotifyOnComplete  bool          `json:"notify_on_complete,omitempty"`
}

type MergeCriteria struct {
	RequiredApprovals int      `json:"required_approvals"`
	ApprovedBy        []string `json:"approved_by"`
	CIStatus          string   `json:"ci_status"`
}

type AuditLog struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Context   map[string]interface{} `json:"context,omitempty"`
	Category  string                 `json:"category,omitempty"`
	Actor     string                 `json:"actor,omitempty"`
	RepoGroup string                 `json:"repo_group,omitempty"`
	PRNumber  int                    `json:"pr_number,omitempty"`
	Platform  string                 `json:"platform,omitempty"`
	Action    string                 `json:"action,omitempty"`
	Before    map[string]interface{} `json:"before,omitempty"`
	After     map[string]interface{} `json:"after,omitempty"`
}

type SyncRecord struct {
	ID             string    `json:"id"`
	PRID           string    `json:"pr_id"`
	RepoGroup      string    `json:"repo_group"`
	SourcePlatform string    `json:"source_platform"`
	TargetPlatform string    `json:"target_platform"`
	Branch         string    `json:"branch"`
	CommitSHA      string    `json:"commit_sha"`
	Status         string    `json:"status"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

type MergeQueueConfig struct {
	RequiredApprovals int      `json:"required_approvals" toml:"required_approvals"`
	CICheckRequired   bool     `json:"ci_check_required" toml:"ci_check_required"`
	CoreContributors  []string `json:"core_contributors" toml:"core_contributors"`
	CIProvider        string   `json:"ci_provider" toml:"ci_provider"`
	FastForwardOnly   bool     `json:"fast_forward_only" toml:"fast_forward_only"`
	Expression        string   `json:"expression" toml:"expression"`
}

type WebhookRetry struct {
	ID         string    `json:"id"`
	DeliveryID string    `json:"delivery_id"`
	RepoGroup  string    `json:"repo_group"`
	Platform   string    `json:"platform"`
	Body       []byte    `json:"body"`
	FailCount  int       `json:"fail_count"`
	LastError  string    `json:"last_error"`
	LastFailed time.Time `json:"last_failed"`
	NextRetry  time.Time `json:"next_retry"`
}

type SpamAuthor struct {
	Author    string    `json:"author"`
	Platform  string    `json:"platform"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int       `json:"count"`
}

type PRStack struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Author      string        `json:"author"`
	State       string        `json:"state"`
	Members     []StackMember `json:"members"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type StackMember struct {
	PRID      string `json:"pr_id"`
	Platform  string `json:"platform"`
	PRNumber  int    `json:"pr_number"`
	RepoGroup string `json:"repo_group"`
	Stage     int    `json:"stage"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
}

type IssuePRLink struct {
	IssueID   string `json:"issue_id"`
	PRID      string `json:"pr_id"`
	RepoGroup string `json:"repo_group"`
	Platform  string `json:"platform"`
	LinkType  string `json:"link_type"`
}

type PRDependency struct {
	PRID          string `json:"pr_id"`
	DependsOnPRID string `json:"depends_on_pr_id"`
	DependsOnURL  string `json:"depends_on_url"`
	RepoGroup     string `json:"repo_group"`
	Platform      string `json:"platform"`
}

type PRTemplate struct {
	RepoGroup    string `json:"repo_group"`
	Platform     string `json:"platform"`
	Content      string `json:"content"`
	HasChecklist bool   `json:"has_checklist"`
}

type FeedConfig struct {
	Enabled    bool   `json:"enabled" toml:"enabled"`
	Title      string `json:"title" toml:"title"`
	MaxItems   int    `json:"max_items" toml:"max_items"`
	PublicFeed bool   `json:"public_feed" toml:"public_feed"`
}

type Config struct {
	Server         ServerConfig       `toml:"server" json:"server"`
	Database       DatabaseConfig     `toml:"database" json:"database"`
	Auth           AuthConfig         `toml:"auth" json:"auth"`
	Notify         []NotifyConfig     `toml:"notify" json:"notify"`
	Events         EventsConfig       `toml:"events" json:"events"`
	Git            GitConfig          `toml:"git" json:"git"`
	Tokens         TokensConfig       `toml:"tokens" json:"tokens"`
	LabelRules     []LabelRule        `toml:"label_rules" json:"label_rules"`
	ReviewRules    []ReviewRule       `toml:"review_rules" json:"review_rules"`
	Spam           SpamConfig         `toml:"spam" json:"spam"`
	MergeQueue     MergeQueueConfig   `toml:"merge_queue" json:"merge_queue"`
	HookPath       string             `toml:"hookpath" json:"hookpath"`
	RepoGroups     []RepoGroupConfig  `toml:"repo_groups" json:"repo_groups"`
	SingleRepo     SingleRepoConfig   `toml:"single_repo" json:"single_repo"`
	GitLabBaseURL  string             `toml:"gitlab_base_url" json:"gitlab_base_url"`
	GiteaBaseURL   string             `toml:"gitea_base_url" json:"gitea_base_url"`
	ForgejoBaseURL string             `toml:"forgejo_base_url" json:"forgejo_base_url"`
	GitHubBaseURL  string             `toml:"github_base_url" json:"github_base_url"`
	Telegram       TelegramConfig     `toml:"telegram" json:"telegram"`
	Feishu         FeishuConfig       `toml:"feishu" json:"feishu"`
	Discord        DiscordConfig      `toml:"discord" json:"discord"`
	Slack          SlackConfig        `toml:"slack" json:"slack"`
	Updates        UpdatesConfig      `toml:"updates" json:"updates"`
	Stale          StaleConfig        `toml:"stale" json:"stale"`
	Reports        ScheduleConfig     `toml:"reports" json:"reports"`
	WorkerPool     WorkerPoolConfig   `toml:"worker_pool" json:"worker_pool"`
	CloseReasons   CloseReasonsConfig `toml:"close_reasons" json:"close_reasons"`
	QuietHours     QuietHoursConfig   `toml:"quiet_hours" json:"quiet_hours"`
	Feed           FeedConfig         `toml:"feed" json:"feed"`
}

type ScheduleConfig struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	Cron       string `toml:"cron" json:"cron"`
	PeriodDays int    `toml:"period_days" json:"period_days"`
}
