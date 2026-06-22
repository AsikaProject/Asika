package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `
[workflow]
enabled = true

[[workflow.labels]]
condition = "ci_passed"
action = "add"
label = "ready"

[workflow.merge]
enabled = true
condition = "ci_passed && approved"
auto_merge = true
merge_method = "squash"
delete_branch = true

[workflow.close]
enabled = false
condition = "ci_failed"
comment = "CI failed"
add_label = "failed"
`

	configPath := filepath.Join(tmpDir, "asika_workflow_config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !cfg.Workflow.Enabled {
		t.Error("expected enabled=true")
	}

	if len(cfg.Workflow.Labels) != 1 {
		t.Errorf("expected 1 label rule, got %d", len(cfg.Workflow.Labels))
	}

	if cfg.Workflow.Merge.MergeMethod != "squash" {
		t.Errorf("expected merge_method=squash, got %s", cfg.Workflow.Merge.MergeMethod)
	}
}

func TestLoadConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected error when config not found")
	}
}

func TestValidateConfig(t *testing.T) {
	tmpDir := t.TempDir()

	invalidConfig := `
[workflow]
enabled = true

[[workflow.labels]]
condition = "ci_passed"
action = "invalid_action"
label = ""
`

	configPath := filepath.Join(tmpDir, "asika_workflow_config.toml")
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Error("expected validation error for invalid action")
	}
}
