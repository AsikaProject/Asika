package models

import (
	"strconv"
	"strings"
	"time"
)

func ParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}

type UserPermissions struct {
	CanApprove          bool `json:"can_approve" toml:"can_approve"`
	CanMerge            bool `json:"can_merge" toml:"can_merge"`
	CanClose            bool `json:"can_close" toml:"can_close"`
	CanReopen           bool `json:"can_reopen" toml:"can_reopen"`
	CanSpam             bool `json:"can_spam" toml:"can_spam"`
	CanManageQueue      bool `json:"can_manage_queue" toml:"can_manage_queue"`
	CanRevert           bool `json:"can_revert" toml:"can_revert"`
	CanComment          bool `json:"can_comment" toml:"can_comment"`
	CanLabel            bool `json:"can_label" toml:"can_label"`
	CanOverrideSecurity bool `json:"can_override_security" toml:"can_override_security"`
}

type SecurityAlert struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	URL      string `json:"url"`
}

type PRComment struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	IsBot     bool      `json:"is_bot"`
}

type SecurityScanConfig struct {
	Enabled         bool     `toml:"enabled" json:"enabled"`
	BlockOnAlerts   bool     `toml:"block_on_alerts" json:"block_on_alerts"`
	OverrideRoles   []string `toml:"override_roles" json:"override_roles"`
	OverridePerms   []string `toml:"override_permissions" json:"override_permissions"`
	CacheTTLSeconds int      `toml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
}

type AISummaryConfig struct {
	Enabled       bool     `toml:"enabled" json:"enabled"`
	Provider      string   `toml:"provider" json:"provider"`
	APIKey        string   `toml:"api_key" json:"api_key"`
	BaseURL       string   `toml:"base_url" json:"base_url"`
	Model         string   `toml:"model" json:"model"`
	BotUsers      []string `toml:"bot_users" json:"bot_users"`
	AutoGenerate  bool     `toml:"auto_generate" json:"auto_generate"`
	MaxDiffLength int      `toml:"max_diff_length" json:"max_diff_length"`
	SystemPrompt  string   `toml:"system_prompt" json:"system_prompt,omitempty"`
	UserPrompt    string   `toml:"user_prompt" json:"user_prompt,omitempty"`
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
	Email             string          `json:"email,omitempty"`
	Role              string          `json:"role"`
	CreatedAt         time.Time       `json:"created_at"`
	AllowedRepoGroups []string        `json:"allowed_repo_groups"`
	AllowedRepos      []string        `json:"allowed_repos"`
	Permissions       UserPermissions `json:"permissions"`
	TOTPSecret        string          `json:"totp_secret,omitempty"`
	TOTPEnabled       bool            `json:"totp_enabled"`
	BackupCodes       []string        `json:"backup_codes,omitempty"`
}

type Session struct {
	ID          string    `json:"id" bson:"id"`
	Username    string    `json:"username" bson:"username"`
	TokenPrefix string    `json:"token_prefix" bson:"token_prefix"`
	IssuedAt    time.Time `json:"issued_at" bson:"issued_at"`
	LastUsedAt  time.Time `json:"last_used_at" bson:"last_used_at"`
	ExpiresAt   time.Time `json:"expires_at" bson:"expires_at"`
	IPAddress   string    `json:"ip_address" bson:"ip_address"`
	UserAgent   string    `json:"user_agent" bson:"user_agent"`
}

type OIDCLink struct {
	Provider   string    `json:"provider" bson:"provider"`
	Subject    string    `json:"subject" bson:"subject"`
	Username   string    `json:"username" bson:"username"`
	CreatedAt  time.Time `json:"created_at" bson:"created_at"`
	LastUsedAt time.Time `json:"last_used_at" bson:"last_used_at"`
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
	ConflictCheck  string           `json:"conflict_check"`
	HookPath       string           `json:"hookpath"`
	CIProvider     string           `json:"ci_provider"`
	MergeQueue     MergeQueueConfig `json:"merge_queue"`
	LabelRules     []LabelRule      `json:"label_rules,omitempty"`
	ReviewRules    []ReviewRule     `json:"review_rules,omitempty"`
	ApprovalRules  []ApprovalRule   `json:"approval_rules,omitempty"`
	PRSizeLimits   PRSizeConfig     `json:"pr_size_limits,omitempty"`
	PRTemplate     PRTemplateConfig `json:"pr_template,omitempty"`
	DraftPR        DraftPRConfig    `json:"draft_pr,omitempty"`
}

type PRBranchInfo struct {
	BaseBranch          string `json:"base_branch"`
	HeadBranch          string `json:"head_branch"`
	HeadSHA             string `json:"head_sha"`
	MaintainerCanModify bool   `json:"maintainer_can_modify"`
}

type PRRecord struct {
	ID              string          `json:"id"`
	RepoGroup       string          `json:"repo_group"`
	Platform        string          `json:"platform"`
	PRNumber        int             `json:"pr_number"`
	Title           string          `json:"title"`
	Author          string          `json:"author"`
	State           string          `json:"state"`
	Labels          []string        `json:"labels"`
	MergeCommitSHA  string          `json:"merge_commit_sha"`
	SpamFlag        bool            `json:"spam_flag"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DiffFiles       []string        `json:"diff_files"`
	Events          []PREvent       `json:"events"`
	IsDraft         bool            `json:"is_draft"`
	HasConflict     bool            `json:"has_conflict"`
	IsApproved      bool            `json:"is_approved"`
	HTMLURL         string          `json:"html_url"`
	MergedAt        time.Time       `json:"merged_at"`
	BranchInfo      *PRBranchInfo   `json:"branch_info,omitempty"`
	CloseReason     string          `json:"close_reason,omitempty"`
	Body            string          `json:"body,omitempty"`
	LinesAdded      int             `json:"lines_added"`
	LinesDeleted    int             `json:"lines_deleted"`
	SecurityBlocked bool            `json:"security_blocked,omitempty"`
	SecurityAlerts  []SecurityAlert `json:"security_alerts,omitempty"`
}

type PREvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Detail    string    `json:"detail"`
}

type PRCommentPayload struct {
	CommentBody   string   `json:"comment_body"`
	CommentAuthor string   `json:"comment_author"`
	Mentions      []string `json:"mentions"`
}

// ApprovalStatus holds the result of replaying all reviews for a PR.
type ApprovalStatus struct {
	Approvers []string
	Blockers  []string
}

// ApprovalState represents the computed LGTM state for a PR.
type ApprovalState string

const (
	ApprovalStatePending ApprovalState = "pending"
	ApprovalStateSuccess ApprovalState = "success"
	ApprovalStateFailure ApprovalState = "failure"
)

// ComputeApprovalStatus is a pure function that computes the LGTM state from approval status.
// It returns the state, a human-readable message, and the desired LGTM label.
func ComputeApprovalStatus(status *ApprovalStatus) (state ApprovalState, message, label string) {
	approvers := status.Approvers
	blockers := status.Blockers

	if len(blockers) > 0 {
		return ApprovalStateFailure, "Blocked by " + strings.Join(blockers, ", "), "lgtm/blocked"
	}

	if len(approvers) == 0 {
		return ApprovalStatePending, "Needs two more approvals", "lgtm/need 2"
	}

	if len(approvers) == 1 {
		return ApprovalStatePending, "Needs one more approval", "lgtm/need 1"
	}

	return ApprovalStateSuccess, "Approved by " + strconv.Itoa(len(approvers)) + " people", "lgtm/done"
}

type QueueItem struct {
	PRID              string        `json:"pr_id"`
	RepoGroup         string        `json:"repo_group"`
	Status            string        `json:"status"`
	AddedAt           time.Time     `json:"added_at"`
	LastChecked       time.Time     `json:"last_checked"`
	FailureReason     string        `json:"failure_reason,omitempty"`
	RetryCount        int           `json:"retry_count,omitempty"`
	NextRetryAt       time.Time     `json:"next_retry_at,omitempty"`
	Criteria          MergeCriteria `json:"criteria"`
	ScheduleAt        time.Time     `json:"schedule_at,omitempty"`
	ValidationStatus  string        `json:"validation_status,omitempty"`
	ValidationStarted time.Time     `json:"validation_started,omitempty"`
	ValidationDetail  string        `json:"validation_detail,omitempty"`
	Space             string        `json:"space,omitempty"`
	Priority          int           `json:"priority,omitempty"`
	NotifyOnComplete  bool          `json:"notify_on_complete,omitempty"`
	SecurityBlocked   bool          `json:"security_blocked,omitempty"`
	SecurityOverride  string        `json:"security_override,omitempty"`
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
	RequiredApprovals         int                  `json:"required_approvals" toml:"required_approvals"`
	CICheckRequired           bool                 `json:"ci_check_required" toml:"ci_check_required"`
	CoreContributors          []string             `json:"core_contributors" toml:"core_contributors"`
	CIProvider                string               `json:"ci_provider" toml:"ci_provider"`
	FastForwardOnly           bool                 `json:"fast_forward_only" toml:"fast_forward_only"`
	Expression                string               `json:"expression" toml:"expression"`
	AllowExpressionOverrideCI bool                 `json:"allow_expression_override_ci" toml:"allow_expression_override_ci"`
	SecurityScan              SecurityScanConfig   `json:"security_scan" toml:"security_scan"`
	MergeStrategyRules        []MergeStrategyRule  `json:"merge_strategy_rules,omitempty" toml:"merge_strategy_rules,omitempty"`
	DefaultMergeMethod        string               `json:"default_merge_method,omitempty" toml:"default_merge_method,omitempty"`
}

type MergeStrategyRule struct {
	Name        string   `json:"name" toml:"name"`
	Pattern     string   `json:"pattern,omitempty" toml:"pattern,omitempty"`
	Labels      []string `json:"labels,omitempty" toml:"labels,omitempty"`
	MergeMethod string   `json:"merge_method" toml:"merge_method"`
	Priority    int      `json:"priority,omitempty" toml:"priority,omitempty"`
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

// DiffFile represents a single file's diff in a PR
type DiffFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added, removed, modified, renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"` // unified diff format
}

// InlineComment represents a comment on a specific line of a diff
type InlineComment struct {
	Body      string `json:"body" binding:"required"`
	CommitSHA string `json:"commit_sha" binding:"required"`
	FilePath  string `json:"file_path" binding:"required"`
	Line      int    `json:"line" binding:"required"`
}

type FeedConfig struct {
	Enabled    bool   `json:"enabled" toml:"enabled"`
	Title      string `json:"title" toml:"title"`
	MaxItems   int    `json:"max_items" toml:"max_items"`
	PublicFeed bool   `json:"public_feed" toml:"public_feed"`
}

// WebhookFilter defines filters for webhook events
type WebhookFilter struct {
	IgnoreEvents  []string `toml:"ignore_events" json:"ignore_events"`   // Event types to ignore (e.g., "label_added", "milestone_changed")
	IgnoreAuthors []string `toml:"ignore_authors" json:"ignore_authors"` // Bot authors to ignore (e.g., "renovate[bot]", "dependabot[bot]")
	IgnoreLabels  []string `toml:"ignore_labels" json:"ignore_labels"`   // PRs with these labels will be ignored
}

// NotifyRule defines notification routing based on PR labels
type NotifyRule struct {
	Labels    []string `toml:"labels" json:"labels"`       // PR labels to match
	Notifiers []string `toml:"notifiers" json:"notifiers"` // Notifier types to use (e.g., ["smtp", "telegram"])
	Always    bool     `toml:"always" json:"always"`       // If true, always notify regardless of other settings
}

// NotifyRulesConfig defines label-based notification routing
type NotifyRulesConfig struct {
	Rules []NotifyRule `toml:"rules" json:"rules"`
}

// AutoRebaseConfig defines automatic rebase settings
type AutoRebaseConfig struct {
	Enabled        bool     `toml:"enabled" json:"enabled"`                 // Enable auto-rebase
	ExcludeLabels  []string `toml:"exclude_labels" json:"exclude_labels"`   // PRs with these labels will not be auto-rebased
	ExcludeAuthors []string `toml:"exclude_authors" json:"exclude_authors"` // PRs from these authors will not be auto-rebased
}

type AutoMergeRule struct {
	Name              string   `toml:"name" json:"name"`
	Labels            []string `toml:"labels" json:"labels"`
	RequiredApprovals int      `toml:"required_approvals" json:"required_approvals"`
	CIRequired        bool     `toml:"ci_required" json:"ci_required"`
	ExcludeLabels     []string `toml:"exclude_labels" json:"exclude_labels"`
	ExcludeAuthors    []string `toml:"exclude_authors" json:"exclude_authors"`
	Enabled           bool     `toml:"enabled" json:"enabled"`
}

type AutoMergeConfig struct {
	Enabled bool            `toml:"enabled" json:"enabled"`
	Rules   []AutoMergeRule `toml:"rules" json:"rules"`
}

type Config struct {
	Server             ServerConfig       `toml:"server" json:"server"`
	Database           DatabaseConfig     `toml:"database" json:"database"`
	Auth               AuthConfig         `toml:"auth" json:"auth"`
	Notify             []NotifyConfig     `toml:"notify" json:"notify"`
	Events             EventsConfig       `toml:"events" json:"events"`
	Git                GitConfig          `toml:"git" json:"git"`
	Tokens             TokensConfig       `toml:"tokens" json:"tokens"`
	LabelRules         []LabelRule        `toml:"label_rules" json:"label_rules"`
	ReviewRules        []ReviewRule       `toml:"review_rules" json:"review_rules"`
	Spam               SpamConfig         `toml:"spam" json:"spam"`
	MergeQueue         MergeQueueConfig   `toml:"merge_queue" json:"merge_queue"`
	HookPath           string             `toml:"hookpath" json:"hookpath"`
	RepoGroups         []RepoGroupConfig  `toml:"repo_groups" json:"repo_groups"`
	SingleRepo         SingleRepoConfig   `toml:"single_repo" json:"single_repo"`
	GitLabBaseURL      string             `toml:"gitlab_base_url" json:"gitlab_base_url"`
	GiteaBaseURL       string             `toml:"gitea_base_url" json:"gitea_base_url"`
	ForgejoBaseURL     string             `toml:"forgejo_base_url" json:"forgejo_base_url"`
	GitHubBaseURL      string             `toml:"github_base_url" json:"github_base_url"`
	Telegram           TelegramConfig     `toml:"telegram" json:"telegram"`
	Feishu             FeishuConfig       `toml:"feishu" json:"feishu"`
	Discord            DiscordConfig      `toml:"discord" json:"discord"`
	Slack              SlackConfig        `toml:"slack" json:"slack"`
	Updates            UpdatesConfig      `toml:"updates" json:"updates"`
	Stale              StaleConfig        `toml:"stale" json:"stale"`
	Reports            ScheduleConfig     `toml:"reports" json:"reports"`
	WorkerPool         WorkerPoolConfig   `toml:"worker_pool" json:"worker_pool"`
	CloseReasons       CloseReasonsConfig `toml:"close_reasons" json:"close_reasons"`
	QuietHours         QuietHoursConfig   `toml:"quiet_hours" json:"quiet_hours"`
	Feed               FeedConfig         `toml:"feed" json:"feed"`
	WebhookFilter      WebhookFilter      `toml:"webhook_filter" json:"webhook_filter"`
	WebhookMaxBodySize int64              `toml:"webhook_max_body_size" json:"webhook_max_body_size"`
	NotifyRules        NotifyRulesConfig  `toml:"notify_rules" json:"notify_rules"`
	AutoRebase         AutoRebaseConfig   `toml:"auto_rebase" json:"auto_rebase"`
	AutoMerge          AutoMergeConfig    `toml:"auto_merge" json:"auto_merge"`
	LDAP               LDAPConfig         `toml:"ldap" json:"ldap"`
	MultiTenant        MultiTenantConfig  `toml:"multi_tenant" json:"multi_tenant"`
	Deployment         DeploymentConfig   `toml:"deployment" json:"deployment"`
	ReviewerLoad       ReviewerLoadConfig `toml:"reviewer_load" json:"reviewer_load"`
	Hooks              []HooksConfig      `toml:"hooks" json:"hooks,omitempty"`
	AISummary          AISummaryConfig    `toml:"ai_summary" json:"ai_summary,omitempty"`
}

type ScheduleConfig struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	Cron       string `toml:"cron" json:"cron"`
	PeriodDays int    `toml:"period_days" json:"period_days"`
}

// ReviewerLoad tracks reviewer workload for load balancing
type ReviewerLoad struct {
	Username     string    `json:"username"`
	PendingCount int       `json:"pending_count"`
	LastReviewAt time.Time `json:"last_review_at"`
}

// ReviewerLoadConfig controls reviewer load balancing behavior
type ReviewerLoadConfig struct {
	Enabled           bool `toml:"enabled" json:"enabled"`
	MaxReviewersPerPR int  `toml:"max_reviewers_per_pr" json:"max_reviewers_per_pr"`
	ActiveDays        int  `toml:"active_days" json:"active_days"`
}

// WebAuthnCredential stores a WebAuthn credential for passkey login
type WebAuthnCredential struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	CredentialID []byte    `json:"credential_id"`
	PublicKey    []byte    `json:"public_key"`
	SignCount    uint32    `json:"sign_count"`
	AAGUID       []byte    `json:"aaguid"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	Name         string    `json:"name"`
}

// WebAuthnSession stores temporary WebAuthn session data during registration/login
type WebAuthnSession struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Challenge string    `json:"challenge"`
	CreatedAt time.Time `json:"created_at"`
}
