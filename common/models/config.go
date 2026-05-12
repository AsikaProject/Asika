package models

// ServerConfig represents server configuration
type ServerConfig struct {
	Listen                 string   `toml:"listen"`
	Mode                   string   `toml:"mode"`
	MinProcs               int      `toml:"min_procs"`
	MaxProcs               int      `toml:"max_procs"`
	EnableWebUpdate        bool     `toml:"enable_web_update"`
	EnablePprof            bool     `toml:"enable_pprof"`
	CORSOrigins            []string `toml:"cors_origins"`
	RateLimitEnabled       bool     `toml:"rate_limit_enabled"`
	RateLimitRPS           int      `toml:"rate_limit_rps"`
	RateLimitBurst         int      `toml:"rate_limit_burst"`
	ReadTimeoutSeconds     int      `toml:"read_timeout_seconds"`
	WriteTimeoutSeconds    int      `toml:"write_timeout_seconds"`
	ShutdownTimeoutSeconds int      `toml:"shutdown_timeout_seconds"`
	MetricsLogInterval     string   `toml:"metrics_log_interval"`
}

// DatabaseConfig represents database configuration.
// Type can be "bbolt" (default) or "mongo".
// For bbolt: Path is the file path to the .db file.
// For mongo: Path is the connection string (e.g. "mongodb://localhost:27017"), Name is the database name.
type DatabaseConfig struct {
	Type string `toml:"type"`
	Path string `toml:"path"`
	Name string `toml:"name"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	JWTSecret   string `toml:"jwt_secret"`
	TokenExpiry string `toml:"token_expiry"`
}

// EventsConfig represents events configuration
type EventsConfig struct {
	Mode                 string `toml:"mode"`
	WebhookSecret        string `toml:"webhook_secret"`
	PollingInterval      string `toml:"polling_interval"`
	HealthCheckInterval  string `toml:"health_check_interval"`
	HealthCheckThreshold string `toml:"health_check_threshold"`
}

// GitConfig represents git configuration
type GitConfig struct {
	WorkDir       string `toml:"workdir"`
	RepoClonePath string `toml:"repo_clone_path"` // optional persistent clone path; empty = use temp dir
}

// TokensConfig represents platform token configuration
type TokensConfig struct {
	GitHub    string     `toml:"github"`
	GitLab    string     `toml:"gitlab"`
	Gitea     string     `toml:"gitea"`
	Forgejo   string     `toml:"forgejo"`
	Codeberg  string     `toml:"codeberg"`
	Bitbucket string     `toml:"bitbucket"`
	Gerrit    GerritAuth `toml:"gerrit"`
}

// GerritAuth holds Gerrit authentication credentials
type GerritAuth struct {
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

// NotifyConfig represents notification configuration
type NotifyConfig struct {
	Type   string                 `toml:"type"`
	Config map[string]interface{} `toml:"config"`
}

// UpdatesConfig represents self-update configuration
type UpdatesConfig struct {
	Check       bool   `toml:"check" json:"check"`
	Interval    string `toml:"interval" json:"interval"`
	NotifyOnNew bool   `toml:"notify_on_new" json:"notify_on_new"`
}

// StaleConfig represents stale PR management configuration
type StaleConfig struct {
	Enabled          bool     `toml:"enabled" json:"enabled"`
	CheckInterval    string   `toml:"check_interval" json:"check_interval"`
	DaysUntilStale   int      `toml:"days_until_stale" json:"days_until_stale"`
	DaysUntilClose   int      `toml:"days_until_close" json:"days_until_close"`
	StaleLabel       string   `toml:"stale_label" json:"stale_label"`
	ExemptLabels     []string `toml:"exempt_labels" json:"exempt_labels"`
	NotifyOnStale    bool     `toml:"notify_on_stale" json:"notify_on_stale"`
	CommentOnStale   string   `toml:"comment_on_stale" json:"comment_on_stale"`
	CommentOnClose   string   `toml:"comment_on_close" json:"comment_on_close"`
	RemoveOnActivity bool     `toml:"remove_stale_on_activity" json:"remove_stale_on_activity"`
	SkipDraftPRs     bool     `toml:"skip_draft_prs" json:"skip_draft_prs"`
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
	Gerrit         string           `toml:"gerrit" json:"gerrit"`
	DefaultBranch  string           `toml:"default_branch" json:"default_branch"`
	BranchSync     string           `toml:"branch_sync" json:"branch_sync"`
	SyncTags       bool             `toml:"sync_tags" json:"sync_tags"`
	HookPath       string           `toml:"hookpath" json:"hookpath"`
	CIProvider     string           `toml:"ci_provider" json:"ci_provider"`
	MergeQueue     MergeQueueConfig `toml:"merge_queue" json:"merge_queue"`
	LabelRules     []LabelRule      `toml:"label_rules" json:"label_rules,omitempty"`
	ReviewRules    []ReviewRule     `toml:"review_rules" json:"review_rules,omitempty"`
}

// SingleRepoConfig represents single repository configuration
type SingleRepoConfig struct {
	Platform      string `toml:"platform"` // mirror_platform in tasks.md, "github"|"gitlab"|"gitea"|"forgejo"|"codeberg"
	Repo          string `toml:"repo"`
	DefaultBranch string `toml:"default_branch"`
	HookPath      string `toml:"hookpath"`
	CIProvider    string `toml:"ci_provider"`
}

// WorkerPoolConfig controls the dynamic worker pool sizing.
type WorkerPoolConfig struct {
	MinWorkers    int    `toml:"min_workers" json:"min_workers"`
	MaxWorkers    int    `toml:"max_workers" json:"max_workers"`
	ScaleUpPct    int    `toml:"scale_up_pct" json:"scale_up_pct"`
	ScaleDownPct  int    `toml:"scale_down_pct" json:"scale_down_pct"`
	CooldownSecs  int    `toml:"cooldown_secs" json:"cooldown_secs"`
	StatsInterval string `toml:"stats_interval" json:"stats_interval"`
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
	Token       string   `toml:"token" json:"token"`         // Bot User OAuth Token (xoxb-...)
	AppToken    string   `toml:"app_token" json:"app_token"` // App-Level Token (xapp-...) for Socket Mode
	AdminIDs    []string `toml:"admin_ids" json:"admin_ids"`
	OperatorIDs []string `toml:"operator_ids" json:"operator_ids"`
	ViewerIDs   []string `toml:"viewer_ids" json:"viewer_ids"`
}
