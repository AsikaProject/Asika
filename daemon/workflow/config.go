package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"asika/common/models"
)

func LoadConfig(workDir string) (*models.WorkflowConfig, error) {
	configPath := filepath.Join(workDir, "asika_workflow_config.toml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read asika_workflow_config.toml: %w", err)
	}

	var cfg models.WorkflowConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

func validateConfig(cfg *models.WorkflowConfig) error {
	for i, rule := range cfg.Workflow.Labels {
		if rule.Action != "add" && rule.Action != "remove" {
			return fmt.Errorf("label rule %d: action must be 'add' or 'remove', got '%s'", i, rule.Action)
		}
		if rule.Label == "" {
			return fmt.Errorf("label rule %d: label cannot be empty", i)
		}
		if rule.Condition == "" {
			return fmt.Errorf("label rule %d: condition cannot be empty", i)
		}
	}

	if cfg.Workflow.Merge.Enabled {
		if cfg.Workflow.Merge.Condition == "" {
			return fmt.Errorf("merge rule: condition cannot be empty when enabled")
		}
		method := cfg.Workflow.Merge.MergeMethod
		if method != "" && method != "merge" && method != "squash" && method != "rebase" {
			return fmt.Errorf("merge rule: merge_method must be 'merge', 'squash', or 'rebase', got '%s'", method)
		}
	}

	if cfg.Workflow.Close.Enabled && cfg.Workflow.Close.Condition == "" {
		return fmt.Errorf("close rule: condition cannot be empty when enabled")
	}

	return nil
}
