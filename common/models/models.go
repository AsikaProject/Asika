package models

import "time"

// UserPermissions defines granular per-user permissions.
// When Role is "admin", all permissions are implicitly granted.
// When Role is "operator" or "viewer", these fields control specific actions.
type UserPermissions struct {
	CanApprove     bool `json:"can_approve" toml:"can_approve"`
	CanMerge       bool `json:"can_merge" toml:"can_merge"`
	CanClose       bool `json:"can_close" toml:"can_close"`
	CanReopen      bool `json:"can_reopen" toml:"can_reopen"`
	CanSpam        bool `json:"can_spam" toml:"can_spam"`
	CanManageQueue bool `json:"can_manage_queue" toml:"can_manage_queue"`
}

// User represents an admin user
type User struct {
	Username          string           `json:"username"`
	PasswordHash      string           `json:"password_hash"` // bcrypt
	Role              string           `json:"role"`          // "admin" | "operator" | "viewer"
	CreatedAt         time.Time        `json:"created_at"`
	AllowedRepoGroups []string         `json:"allowed_repo_groups"` // empty = all groups
	Permissions       UserPermissions  `json:"permissions"`
}

// RepoGroup represents a repository group
type RepoGroup struct {
    Name           string           `json:"name"`
    Mode           string           `json:"mode"`            // "multi" | "single"
    MirrorPlatform string           `json:"mirror_platform"` // single mode source platform, e.g. "github"
    GitHub         string           `json:"github"`
    GitLab         string           `json:"gitlab"`
    Gitea          string           `json:"gitea"`
    Forgejo        string           `json:"forgejo"`
	Codeberg       string           `json:"codeberg"`
	Bitbucket      string           `json:"bitbucket"`
	DefaultBranch  string           `json:"default_branch"`
    HookPath       string           `json:"hookpath"`
    CIProvider     string           `json:"ci_provider"`
    MergeQueue     MergeQueueConfig  `json:"merge_queue"`
}

// PRBranchInfo holds branch metadata for rebase operations
type PRBranchInfo struct {
	BaseBranch         string `json:"base_branch"`
	HeadBranch         string `json:"head_branch"`
	HeadSHA            string `json:"head_sha"`
	MaintainerCanModify bool  `json:"maintainer_can_modify"`
}

// PRRecord represents a pull request record
type PRRecord struct {
	ID                 string       `json:"id"` // UUID
	RepoGroup          string       `json:"repo_group"`
    Platform           string       `json:"platform"` // "github"|"gitlab"|"gitea"|"forgejo"|"codeberg"|"bitbucket"
	PRNumber           int          `json:"pr_number"`
	Title              string       `json:"title"`
	Author             string       `json:"author"`
	State              string       `json:"state"` // "open"|"closed"|"merged"|"spam"
	Labels             []string     `json:"labels"`
	MergeCommitSHA     string       `json:"merge_commit_sha"`
	SpamFlag           bool         `json:"spam_flag"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	DiffFiles          []string     `json:"diff_files"` // changed file list for label rules
	Events             []PREvent    `json:"events"`
	IsDraft            bool         `json:"is_draft"` // true if PR is a draft (GitHub) or WIP (GitLab)
	HasConflict        bool         `json:"has_conflict"` // true if PR has merge conflicts
	IsApproved         bool         `json:"is_approved"` // true if PR has been approved by at least one reviewer
	HTMLURL            string       `json:"html_url"`    // URL to the PR on the platform
	MergedAt           time.Time    `json:"merged_at"`   // when the PR was merged (zero if not merged)
	BranchInfo         *PRBranchInfo `json:"branch_info,omitempty"` // branch metadata for rebase
}

// PREvent represents a pull request event
type PREvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"` // "opened"|"closed"|"merged"|"approved"|"labeled"|"synced"|"cherry_picked"|"comment"|...
	Actor     string    `json:"actor"`
	Detail    string    `json:"detail"`
}

// PRCommentPayload carries comment event data through the event bus
type PRCommentPayload struct {
	CommentBody string `json:"comment_body"`
	CommentAuthor string `json:"comment_author"`
}

// QueueItem represents a merge queue item
type QueueItem struct {
    PRID          string         `json:"pr_id"`
    RepoGroup     string         `json:"repo_group"`
    Status        string         `json:"status"` // "waiting"|"checking"|"merging"|"done"|"failed"
    AddedAt       time.Time      `json:"added_at"`
    LastChecked   time.Time      `json:"last_checked"`
    FailureReason string         `json:"failure_reason,omitempty"`
    Criteria      MergeCriteria  `json:"criteria"`
}

// MergeCriteria represents a snapshot of merge conditions
type MergeCriteria struct {
    RequiredApprovals int      `json:"required_approvals"`
    ApprovedBy        []string `json:"approved_by"`
    CIStatus          string   `json:"ci_status"` // "pending"|"success"|"failure"|"none"
}

// AuditLog represents an audit log entry
type AuditLog struct {
    Timestamp time.Time              `json:"timestamp"`
    Level     string                 `json:"level"` // "info"|"warn"|"error"
    Message   string                 `json:"message"`
    Context   map[string]interface{} `json:"context,omitempty"`
}

// SyncRecord represents a sync history record
type SyncRecord struct {
    ID             string    `json:"id"`
    PRID           string    `json:"pr_id"`
    RepoGroup      string    `json:"repo_group"`
    SourcePlatform string    `json:"source_platform"`
    TargetPlatform string    `json:"target_platform"`
    Branch         string    `json:"branch"`
    CommitSHA      string    `json:"commit_sha"`
    Status         string    `json:"status"` // "success"|"failed"
    ErrorMessage   string    `json:"error_message,omitempty"`
    Timestamp      time.Time `json:"timestamp"`
}

// MergeQueueConfig represents merge queue configuration
type MergeQueueConfig struct {
    RequiredApprovals int      `json:"required_approvals" toml:"required_approvals"`
    CICheckRequired   bool     `json:"ci_check_required" toml:"ci_check_required"`
    CoreContributors  []string `json:"core_contributors" toml:"core_contributors"`
    CIProvider        string   `json:"ci_provider" toml:"ci_provider"`    // per-repo-group override
    FastForwardOnly   bool     `json:"fast_forward_only" toml:"fast_forward_only"` // if true, auto-rebase before merge
}

// LabelCondition represents a single condition within a compound rule.
type LabelCondition struct {
	Pattern string `json:"pattern" toml:"pattern"`
}

// LabelRule represents a label rule.
// For simple rules, use Pattern + Label.
// For compound rules, use Conditions + Logic + Label.
type LabelRule struct {
	Pattern     string           `json:"pattern,omitempty" toml:"pattern"`
	Label       string           `json:"label" toml:"label"`
	Color       string           `json:"color,omitempty" toml:"color"`
	Description string           `json:"description,omitempty" toml:"description"`
	Conditions  []LabelCondition `json:"conditions,omitempty" toml:"conditions"`
	Logic       string           `json:"logic,omitempty" toml:"logic"` // "and" or "or", default "and"
}

// ReviewRule represents an automatic reviewer assignment rule.
// Pattern matches against file paths (default), title (title:), or author (author:).
type ReviewRule struct {
	Pattern    string   `json:"pattern" toml:"pattern"`
	Reviewers  []string `json:"reviewers" toml:"reviewers"`
}

// SpamConfig represents spam detection configuration
type SpamConfig struct {
	Enabled               bool     `json:"enabled" toml:"enabled"`
	TimeWindow            string   `json:"time_window" toml:"time_window"`
	Threshold             int      `json:"threshold" toml:"threshold"`
	TriggerOnAuthor      bool     `json:"trigger_on_author" toml:"trigger_on_author"`
	TriggerOnTitleKw     []string `json:"trigger_on_title_kw" toml:"trigger_on_title_kw"`
	AutoCleanEnabled     bool     `json:"auto_clean_enabled" toml:"auto_clean_enabled"`
	AutoCleanInterval    string   `json:"auto_clean_interval" toml:"auto_clean_interval"`
}

// NotifyConfig represents notification configuration
type NotifyConfig struct {
    Type   string                 `toml:"type"`
    Config map[string]interface{} `toml:"config"`
}

// ServerConfig represents server configuration
type ServerConfig struct {
	Listen              string   `toml:"listen"`
	Mode                string   `toml:"mode"`
	EnableWebUpdate     bool     `toml:"enable_web_update"`
	EnablePprof         bool     `toml:"enable_pprof"`
	CORSOrigins         []string `toml:"cors_origins"`
	RateLimitEnabled    bool     `toml:"rate_limit_enabled"`
	RateLimitRPS        int      `toml:"rate_limit_rps"`
	RateLimitBurst      int      `toml:"rate_limit_burst"`
	ReadTimeoutSeconds  int      `toml:"read_timeout_seconds"`
	WriteTimeoutSeconds int      `toml:"write_timeout_seconds"`
	ShutdownTimeoutSeconds int   `toml:"shutdown_timeout_seconds"`
	MetricsLogInterval  string   `toml:"metrics_log_interval"`
}

// UpdatesConfig represents self-update configuration
type UpdatesConfig struct {
    Check       bool   `toml:"check" json:"check"`
    Interval    string `toml:"interval" json:"interval"`
    NotifyOnNew bool   `toml:"notify_on_new" json:"notify_on_new"`
}

// StaleConfig represents stale PR management configuration
type StaleConfig struct {
    Enabled              bool     `toml:"enabled" json:"enabled"`
    CheckInterval        string   `toml:"check_interval" json:"check_interval"`
    DaysUntilStale       int      `toml:"days_until_stale" json:"days_until_stale"`
    DaysUntilClose       int      `toml:"days_until_close" json:"days_until_close"`
    StaleLabel           string   `toml:"stale_label" json:"stale_label"`
    ExemptLabels         []string `toml:"exempt_labels" json:"exempt_labels"`
    NotifyOnStale        bool     `toml:"notify_on_stale" json:"notify_on_stale"`
    CommentOnStale       string   `toml:"comment_on_stale" json:"comment_on_stale"`
    CommentOnClose       string   `toml:"comment_on_close" json:"comment_on_close"`
    RemoveOnActivity     bool     `toml:"remove_stale_on_activity" json:"remove_stale_on_activity"`
    SkipDraftPRs         bool     `toml:"skip_draft_prs" json:"skip_draft_prs"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
    Path string `toml:"path"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
    JWTSecret   string `toml:"jwt_secret"`
    TokenExpiry string `toml:"token_expiry"`
}

// EventsConfig represents events configuration
type EventsConfig struct {
    Mode            string `toml:"mode"`
    WebhookSecret   string `toml:"webhook_secret"`
    PollingInterval string `toml:"polling_interval"`
}

// GitConfig represents git configuration
type GitConfig struct {
    WorkDir      string `toml:"workdir"`
    RepoClonePath string `toml:"repo_clone_path"` // optional persistent clone path; empty = use temp dir
}

// TokensConfig represents platform token configuration
type TokensConfig struct {
    GitHub    string `toml:"github"`
    GitLab    string `toml:"gitlab"`
    Gitea     string `toml:"gitea"`
    Forgejo   string `toml:"forgejo"`
    Codeberg  string `toml:"codeberg"`
    Bitbucket string `toml:"bitbucket"`
}

// RepoGroupConfig represents repository group configuration (TOML mapping)
type RepoGroupConfig struct {
	Name           string           `toml:"name" json:"name"`
	Mode           string           `toml:"mode" json:"mode"`
	MirrorPlatform string           `toml:"mirror_platform" json:"mirror_platform"`
	GitHub         string           `toml:"github" json:"github"`
	GitLab         string           `toml:"gitlab" json:"gitlab"`
	Gitea          string           `toml:"gitea" json:"gitea"`
	Forgejo        string           `toml:"forgejo" json:"forgejo"`
	Codeberg       string           `toml:"codeberg" json:"codeberg"`
	Bitbucket      string           `toml:"bitbucket" json:"bitbucket"`
	DefaultBranch  string           `toml:"default_branch" json:"default_branch"`
	HookPath       string           `toml:"hookpath" json:"hookpath"`
	CIProvider     string           `toml:"ci_provider" json:"ci_provider"`
	MergeQueue     MergeQueueConfig `toml:"merge_queue" json:"merge_queue"`
}

// SingleRepoConfig represents single repository configuration
type SingleRepoConfig struct {
    Platform      string `toml:"platform"` // mirror_platform in tasks.md, "github"|"gitlab"|"gitea"|"forgejo"|"codeberg"
    Repo          string `toml:"repo"`
    DefaultBranch string `toml:"default_branch"`
    HookPath      string `toml:"hookpath"`
    CIProvider    string `toml:"ci_provider"`
}

// Config represents the main configuration structure
type Config struct {
	Server      ServerConfig      `toml:"server" json:"server"`
	Database    DatabaseConfig    `toml:"database" json:"database"`
	Auth        AuthConfig        `toml:"auth" json:"auth"`
	Notify      []NotifyConfig    `toml:"notify" json:"notify"`
	Events      EventsConfig      `toml:"events" json:"events"`
	Git         GitConfig         `toml:"git" json:"git"`
	Tokens      TokensConfig      `toml:"tokens" json:"tokens"`
	LabelRules  []LabelRule       `toml:"label_rules" json:"label_rules"`
	ReviewRules []ReviewRule      `toml:"review_rules" json:"review_rules"`
	Spam        SpamConfig        `toml:"spam" json:"spam"`
	MergeQueue  MergeQueueConfig  `toml:"merge_queue" json:"merge_queue"`
	HookPath    string            `toml:"hookpath" json:"hookpath"`
	RepoGroups   []RepoGroupConfig `toml:"repo_groups" json:"repo_groups"`
	SingleRepo   SingleRepoConfig  `toml:"single_repo" json:"single_repo"`
	GitLabBaseURL  string          `toml:"gitlab_base_url" json:"gitlab_base_url"`
	GiteaBaseURL   string          `toml:"gitea_base_url" json:"gitea_base_url"`
	ForgejoBaseURL  string          `toml:"forgejo_base_url" json:"forgejo_base_url"`
	GitHubBaseURL  string          `toml:"github_base_url" json:"github_base_url"`
	Telegram        TelegramConfig  `toml:"telegram" json:"telegram"`
	Feishu        FeishuConfig     `toml:"feishu" json:"feishu"`
	Discord       DiscordConfig    `toml:"discord" json:"discord"`
	Slack         SlackConfig      `toml:"slack" json:"slack"`
	Updates       UpdatesConfig    `toml:"updates" json:"updates"`
	Stale         StaleConfig      `toml:"stale" json:"stale"`
	Reports       ScheduleConfig   `toml:"reports" json:"reports"`
}

// ScheduleConfig defines scheduled report configuration.
type ScheduleConfig struct {
	Enabled  bool   `toml:"enabled" json:"enabled"`
	Cron     string `toml:"cron" json:"cron"`
}

// TelegramConfig represents Telegram bot configuration
type TelegramConfig struct {
	Enabled     bool     `toml:"enabled" json:"enabled"`
	Token       string   `toml:"token" json:"token"`
	AdminIDs    []int64  `toml:"admin_ids" json:"admin_ids"`
	OperatorIDs []int64  `toml:"operator_ids" json:"operator_ids"`
	ViewerIDs   []int64  `toml:"viewer_ids" json:"viewer_ids"`
	ChatIDs     []string `toml:"chat_ids" json:"chat_ids"`
}

// FeishuConfig represents Feishu/Lark bot configuration
type FeishuConfig struct {
	Enabled           bool     `toml:"enabled" json:"enabled"`
	AppID             string   `toml:"app_id" json:"app_id"`
	AppSecret         string   `toml:"app_secret" json:"app_secret"`
	WebhookURL        string   `toml:"webhook_url" json:"webhook_url"`
	VerificationToken string   `toml:"verification_token" json:"verification_token"`
	EncryptKey        string   `toml:"encrypt_key" json:"encrypt_key"`
	AdminIDs          []string `toml:"admin_ids" json:"admin_ids"`
	OperatorIDs       []string `toml:"operator_ids" json:"operator_ids"`
	ViewerIDs         []string `toml:"viewer_ids" json:"viewer_ids"`
}

// WebhookRetry represents a failed webhook that needs retry
type WebhookRetry struct {
    ID         string    `json:"id"`
    RepoGroup  string    `json:"repo_group"`
    Platform   string    `json:"platform"`
    Body       []byte    `json:"body"`
    FailCount  int       `json:"fail_count"`
    LastError  string    `json:"last_error"`
    LastFailed time.Time `json:"last_failed"`
    NextRetry  time.Time `json:"next_retry"`
}

// DiscordConfig represents Discord bot configuration
type DiscordConfig struct {
	Enabled     bool     `toml:"enabled" json:"enabled"`
	Token       string   `toml:"token" json:"token"`
	AdminIDs    []string `toml:"admin_ids" json:"admin_ids"`
	OperatorIDs []string `toml:"operator_ids" json:"operator_ids"`
	ViewerIDs   []string `toml:"viewer_ids" json:"viewer_ids"`
	ChannelID   string   `toml:"channel_id" json:"channel_id"`
}

// SlackConfig represents Slack bot configuration
type SlackConfig struct {
	Enabled     bool     `toml:"enabled" json:"enabled"`
	Token       string   `toml:"token" json:"token"`       // Bot User OAuth Token (xoxb-...)
	AppToken    string   `toml:"app_token" json:"app_token"` // App-Level Token (xapp-...) for Socket Mode
	AdminIDs    []string `toml:"admin_ids" json:"admin_ids"`
	OperatorIDs []string `toml:"operator_ids" json:"operator_ids"`
	ViewerIDs   []string `toml:"viewer_ids" json:"viewer_ids"`
}