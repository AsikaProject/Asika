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
