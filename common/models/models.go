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
	CanRevert      bool `json:"can_revert" toml:"can_revert"`
}

// APIKey represents a long-lived API key for external integrations.
type APIKey struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`     // human-readable label, e.g. "ci-cd-operator"
	KeyHash           string          `json:"key_hash"` // bcrypt hash of the raw key
	Role              string          `json:"role"`     // "admin" | "operator" | "viewer"
	CreatedAt         time.Time       `json:"created_at"`
	CreatedBy         string          `json:"created_by"` // username who created it
	LastUsedAt        time.Time       `json:"last_used_at"`
	AllowedRepoGroups []string        `json:"allowed_repo_groups"` // empty = all groups
	Permissions       UserPermissions `json:"permissions"`         // only effective when role=operator
}

// User represents an admin user
type User struct {
	Username          string          `json:"username"`
	PasswordHash      string          `json:"password_hash"` // bcrypt
	Role              string          `json:"role"`          // "admin" | "operator" | "viewer"
	CreatedAt         time.Time       `json:"created_at"`
	AllowedRepoGroups []string        `json:"allowed_repo_groups"` // empty = all groups
	Permissions       UserPermissions `json:"permissions"`
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
	Gerrit         string           `json:"gerrit"`
	DefaultBranch  string           `json:"default_branch"`
	HookPath       string           `json:"hookpath"`
	CIProvider     string           `json:"ci_provider"`
	MergeQueue     MergeQueueConfig `json:"merge_queue"`
	LabelRules     []LabelRule      `json:"label_rules,omitempty"`
}

// PRBranchInfo holds branch metadata for rebase operations
type PRBranchInfo struct {
	BaseBranch          string `json:"base_branch"`
	HeadBranch          string `json:"head_branch"`
	HeadSHA             string `json:"head_sha"`
	MaintainerCanModify bool   `json:"maintainer_can_modify"`
}

// PRRecord represents a pull request record
type PRRecord struct {
	ID             string        `json:"id"` // UUID
	RepoGroup      string        `json:"repo_group"`
	Platform       string        `json:"platform"` // "github"|"gitlab"|"gitea"|"forgejo"|"codeberg"|"bitbucket"
	PRNumber       int           `json:"pr_number"`
	Title          string        `json:"title"`
	Author         string        `json:"author"`
	State          string        `json:"state"` // "open"|"closed"|"merged"|"spam"
	Labels         []string      `json:"labels"`
	MergeCommitSHA string        `json:"merge_commit_sha"`
	SpamFlag       bool          `json:"spam_flag"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	DiffFiles      []string      `json:"diff_files"` // changed file list for label rules
	Events         []PREvent     `json:"events"`
	IsDraft        bool          `json:"is_draft"`               // true if PR is a draft (GitHub) or WIP (GitLab)
	HasConflict    bool          `json:"has_conflict"`           // true if PR has merge conflicts
	IsApproved     bool          `json:"is_approved"`            // true if PR has been approved by at least one reviewer
	HTMLURL        string        `json:"html_url"`               // URL to the PR on the platform
	MergedAt       time.Time     `json:"merged_at"`              // when the PR was merged (zero if not merged)
	BranchInfo     *PRBranchInfo `json:"branch_info,omitempty"`  // branch metadata for rebase
	CloseReason    string        `json:"close_reason,omitempty"` // reason for closing (empty, a predefined reason, or custom text)
	Body           string        `json:"body,omitempty"`         // PR description body for issue link parsing
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
	CommentBody   string `json:"comment_body"`
	CommentAuthor string `json:"comment_author"`
}

// QueueItem represents a merge queue item
type QueueItem struct {
	PRID               string        `json:"pr_id"`
	RepoGroup          string        `json:"repo_group"`
	Status             string        `json:"status"` // "waiting"|"checking"|"merging"|"done"|"failed"
	AddedAt            time.Time     `json:"added_at"`
	LastChecked        time.Time     `json:"last_checked"`
	FailureReason      string        `json:"failure_reason,omitempty"`
	Criteria           MergeCriteria `json:"criteria"`
	ScheduleAt         time.Time     `json:"schedule_at,omitempty"` // if set, don't merge until this time
	ValidationStatus   string        `json:"validation_status,omitempty"` // ""|"validating"|"rebooting"|"waiting_ci"|"ci_running"|"ready"|"validation_failed"
	ValidationStarted  time.Time     `json:"validation_started,omitempty"`
	ValidationDetail   string        `json:"validation_detail,omitempty"` // human-readable status detail
	Space              string        `json:"space,omitempty"` // team space name for cross-space tracking
	Priority           int           `json:"priority,omitempty"` // higher = more urgent
	NotifyOnComplete   bool          `json:"notify_on_complete,omitempty"` // send notification when merged
}

// MergeCriteria represents a snapshot of merge conditions
type MergeCriteria struct {
	RequiredApprovals int      `json:"required_approvals"`
	ApprovedBy        []string `json:"approved_by"`
	CIStatus          string   `json:"ci_status"` // "pending"|"success"|"failure"|"none"
}

// AuditLog represents an audit log entry
type AuditLog struct {
	Timestamp  time.Time              `json:"timestamp"`
	Level      string                 `json:"level"` // "info"|"warn"|"error"
	Message    string                 `json:"message"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Category   string                 `json:"category,omitempty"`   // "pr"|"auth"|"config"|"system"
	Actor      string                 `json:"actor,omitempty"`      // username or "system"
	RepoGroup  string                 `json:"repo_group,omitempty"` // repo group name
	PRNumber   int                    `json:"pr_number,omitempty"`  // PR number if applicable
	Platform   string                 `json:"platform,omitempty"`   // platform name
	Action     string                 `json:"action,omitempty"`     // specific action: "approve"|"close"|"merge"|"reopen"|"spam"|"revert"|"login"|"config_change"
	Before     map[string]interface{} `json:"before,omitempty"`    // state before change
	After      map[string]interface{} `json:"after,omitempty"`     // state after change
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
	CIProvider        string   `json:"ci_provider" toml:"ci_provider"`             // per-repo-group override
	FastForwardOnly   bool     `json:"fast_forward_only" toml:"fast_forward_only"` // if true, auto-rebase before merge
	Expression        string   `json:"expression" toml:"expression"`               // merge condition expression (empty = use default logic)
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

// SpamAuthor tracks a known spam author in the database.
type SpamAuthor struct {
	Author    string    `json:"author"`
	Platform  string    `json:"platform"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int       `json:"count"`
}

// PRStack represents a cross-platform PR chain/stack.
type PRStack struct {
	ID          string       `json:"id"`          // UUID
	Name        string       `json:"name"`        // human-readable name, e.g. "feature-auth"
	Description string       `json:"description"` // optional description
	Author      string       `json:"author"`      // creator of the stack
	State       string       `json:"state"`       // "open"|"merged"|"partial"|"failed"
	Members     []StackMember `json:"members"`    // PRs in this stack
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// StackMember represents a single PR within a stack.
type StackMember struct {
	PRID       string `json:"pr_id"`       // PRRecord.ID
	Platform   string `json:"platform"`    // "github"|"gitlab"|...
	PRNumber   int    `json:"pr_number"`   // PR number on the platform
	RepoGroup  string `json:"repo_group"`  // repo group name
	Stage      int    `json:"stage"`       // ordering: 0 = base, 1 = next, etc.
	State      string `json:"state"`       // "open"|"merged"|"failed"
	HTMLURL    string `json:"html_url"`    // link to the PR
}

// IssuePRLink represents a link between an Issue and a PR.
type IssuePRLink struct {
	IssueID    string `json:"issue_id"`    // e.g. "owner/repo#123"
	PRID       string `json:"pr_id"`       // PRRecord.ID
	RepoGroup  string `json:"repo_group"`
	Platform   string `json:"platform"`
	LinkType   string `json:"link_type"`   // "fixes" | "closes" | "resolves" | "related"
}

// PRDependency represents a dependency between two PRs.
type PRDependency struct {
	PRID           string `json:"pr_id"`           // the PR that depends on another
	DependsOnPRID  string `json:"depends_on_pr_id"` // the PR being depended on
	DependsOnURL   string `json:"depends_on_url"`  // original URL in description
	RepoGroup      string `json:"repo_group"`
	Platform       string `json:"platform"`
}

// PRTemplate represents a PR template for a repo group.
type PRTemplate struct {
	RepoGroup  string `json:"repo_group"`
	Platform   string `json:"platform"`
	Content    string `json:"content"`     // template body
	HasChecklist bool  `json:"has_checklist"` // true if template contains checklist items
}

// Config represents the main configuration structure
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
}

// ScheduleConfig defines scheduled report configuration.
type ScheduleConfig struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	Cron       string `toml:"cron" json:"cron"`
	PeriodDays int    `toml:"period_days" json:"period_days"`
}
