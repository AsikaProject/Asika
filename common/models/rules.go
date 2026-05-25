package models

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
	Logic       string           `json:"logic,omitempty" toml:"logic"`         // "and" or "or", default "and"
	Priority    int              `json:"priority,omitempty" toml:"priority"`   // higher = evaluated first
	Exclusive   bool             `json:"exclusive,omitempty" toml:"exclusive"` // if true, stop after this rule matches
}

// ReviewRule represents an automatic reviewer assignment rule.
// Pattern matches against file paths (default), title (title:), or author (author:).
type ReviewRule struct {
	Pattern   string   `json:"pattern" toml:"pattern"`
	Reviewers []string `json:"reviewers" toml:"reviewers"`
	Priority  int      `json:"priority,omitempty" toml:"priority"`
}

// ApprovalRule represents a file-path-based approval requirement.
// When files matching Pattern are changed, at least one of the Approvers must approve.
type ApprovalRule struct {
	Pattern   string   `json:"pattern" toml:"pattern"`
	Approvers []string `json:"approvers" toml:"approvers"`
	MinCount  int      `json:"min_count,omitempty" toml:"min_count"`
	Priority  int      `json:"priority,omitempty" toml:"priority"`
	Reason    string   `json:"reason,omitempty" toml:"reason"`
}

// PRSizeConfig controls PR size warnings and merge blocking.
// ChangesExceedThreshold blocks merge; LinesChangedWarn adds a label.
type PRSizeConfig struct {
	Enabled           bool     `json:"enabled" toml:"enabled"`
	LinesChangedWarn  int      `json:"lines_changed_warn" toml:"lines_changed_warn"`
	LinesChangedBlock int      `json:"lines_changed_block" toml:"lines_changed_block"`
	FilesChangedWarn  int      `json:"files_changed_warn" toml:"files_changed_warn"`
	FilesChangedBlock int      `json:"files_changed_block" toml:"files_changed_block"`
	BlockMerge        bool     `json:"block_merge" toml:"block_merge"`
	BigPRLabel        string   `json:"big_pr_label" toml:"big_pr_label"`
	WarnPRLabel       string   `json:"warn_pr_label" toml:"warn_pr_label"`
	NotifyOnBigPR     bool     `json:"notify_on_big_pr" toml:"notify_on_big_pr"`
	ExemptLabels      []string `json:"exempt_labels" toml:"exempt_labels"`
}

// PRTemplateConfig controls PR description template enforcement.
// When enabled, PRs must have a non-empty body matching the checklist.
type PRTemplateConfig struct {
	Enabled          bool     `json:"enabled" toml:"enabled"`
	RequireBody      bool     `json:"require_body" toml:"require_body"`
	RequireChecklist bool     `json:"require_checklist" toml:"require_checklist"`
	BlockMerge       bool     `json:"block_merge" toml:"block_merge"`
	IncompleteLabel  string   `json:"incomplete_label" toml:"incomplete_label"`
	WarnMessage      string   `json:"warn_message" toml:"warn_message"`
	ExemptLabels     []string `json:"exempt_labels" toml:"exempt_labels"`
}

// DraftPRConfig controls draft PR workflow.
type DraftPRConfig struct {
	AutoEnqueueOnReady bool `json:"auto_enqueue_on_ready" toml:"auto_enqueue_on_ready"`
	SkipQueue          bool `json:"skip_queue" toml:"skip_queue"`
	SkipEscalation     bool `json:"skip_escalation" toml:"skip_escalation"`
}

// LDAPConfig represents LDAP/Active Directory authentication configuration.
type LDAPConfig struct {
	Enabled        bool     `json:"enabled" toml:"enabled"`
	Host           string   `json:"host" toml:"host"`
	Port           int      `json:"port" toml:"port"`
	UseTLS         bool     `json:"use_tls" toml:"use_tls"`
	StartTLS       bool     `json:"start_tls" toml:"start_tls"`
	BindDN         string   `json:"bind_dn" toml:"bind_dn"`
	BindPassword   string   `json:"bind_password" toml:"bind_password"`
	BaseDN         string   `json:"base_dn" toml:"base_dn"`
	UserFilter     string   `json:"user_filter" toml:"user_filter"`
	GroupFilter    string   `json:"group_filter" toml:"group_filter"`
	GroupDN        string   `json:"group_dn" toml:"group_dn"`
	AllowedGroups  []string `json:"allowed_groups" toml:"allowed_groups"`
	AutoCreate     bool     `json:"auto_create" toml:"auto_create"`
	DefaultRole    string   `json:"default_role" toml:"default_role"`
	EmailAttribute string   `json:"email_attribute" toml:"email_attribute"`
	NameAttribute  string   `json:"name_attribute" toml:"name_attribute"`
}

// MultiTenantConfig controls multi-tenancy behavior.
type MultiTenantConfig struct {
	Enabled            bool `json:"enabled" toml:"enabled"`
	SpaceDataIsolation bool `json:"space_data_isolation" toml:"space_data_isolation"`
}

// DeploymentConfig controls deployment tracking integration.
type DeploymentConfig struct {
	Enabled      bool   `json:"enabled" toml:"enabled"`
	WebhookURL   string `json:"webhook_url" toml:"webhook_url"`
	StatusURL    string `json:"status_url" toml:"status_url"`
	AutoTrack    bool   `json:"auto_track" toml:"auto_track"`
	NotifyOnFail bool   `json:"notify_on_fail" toml:"notify_on_fail"`
}

// SpamConfig represents spam detection configuration
type SpamConfig struct {
	Enabled           bool     `json:"enabled" toml:"enabled"`
	TimeWindow        string   `json:"time_window" toml:"time_window"`
	Threshold         int      `json:"threshold" toml:"threshold"`
	TriggerOnAuthor   bool     `json:"trigger_on_author" toml:"trigger_on_author"`
	TriggerOnTitleKw  []string `json:"trigger_on_title_kw" toml:"trigger_on_title_kw"`
	AutoCleanEnabled  bool     `json:"auto_clean_enabled" toml:"auto_clean_enabled"`
	AutoCleanInterval string   `json:"auto_clean_interval" toml:"auto_clean_interval"`
}
