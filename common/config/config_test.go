package config

import (
	"os"
	"path/filepath"
	"testing"

	"asika/common/models"
)

func TestConfigLoadAndStore(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test_config.toml")
	oldPath := ConfigPath
	defer func() { ConfigPath = oldPath }()

	configToml := `
[server]
listen = ":9090"
mode = "debug"

[database]
path = "./test.db"

[auth]
jwt_secret = "my-secret-key"
token_expiry = "24h"

[events]
mode = "webhook"
webhook_secret = "wh-secret"
polling_interval = "60s"

[git]
workdir = "./_work"

[spam]
enabled = true
time_window = "5m"
threshold = 5
trigger_on_author = true
trigger_on_title_kw = ["spam", "click here"]

[merge_queue]
required_approvals = 2
ci_check_required = true
core_contributors = ["admin", "lead"]

[tokens]
github = "ghp_test1234567890abcdefghijklmnop"
gitlab = "glpat_test9876543210"
gitea = "gitea_token_11223344"

[[notify]]
type = "smtp"
[notify.config]

[[label_rules]]
pattern = "*.go"
label = "go-code"

[[repo_groups]]
name = "main"
mode = "multi"
github = "org/repo"
gitlab = "group/repo"
gitea = "user/repo"
default_branch = "main"
hookpath = "hooks/test-hooks"
`
	os.WriteFile(configPath, []byte(configToml), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	t.Run("server config", func(t *testing.T) {
		if cfg.Server.Listen != ":9090" {
			t.Errorf("Listen = %q, want :9090", cfg.Server.Listen)
		}
		if cfg.Server.Mode != "debug" {
			t.Errorf("Mode = %q, want debug", cfg.Server.Mode)
		}
	})

	t.Run("database config", func(t *testing.T) {
		if cfg.Database.Path != "./test.db" {
			t.Errorf("Path = %q, want ./test.db", cfg.Database.Path)
		}
	})

	t.Run("auth config", func(t *testing.T) {
		if cfg.Auth.JWTSecret != "my-secret-key" {
			t.Errorf("JWTSecret = %q, want my-secret-key", cfg.Auth.JWTSecret)
		}
		if cfg.Auth.TokenExpiry != "24h" {
			t.Errorf("TokenExpiry = %q, want 24h", cfg.Auth.TokenExpiry)
		}
	})

	t.Run("events config", func(t *testing.T) {
		if cfg.Events.Mode != "webhook" {
			t.Errorf("Mode = %q, want webhook", cfg.Events.Mode)
		}
		if cfg.Events.WebhookSecret != "wh-secret" {
			t.Errorf("WebhookSecret mismatch")
		}
	})

	t.Run("spam config", func(t *testing.T) {
		if !cfg.Spam.Enabled {
			t.Error("Spam should be enabled")
		}
		if cfg.Spam.TimeWindow != "5m" {
			t.Errorf("TimeWindow = %q, want 5m", cfg.Spam.TimeWindow)
		}
		if cfg.Spam.Threshold != 5 {
			t.Errorf("Threshold = %d, want 5", cfg.Spam.Threshold)
		}
	})

	t.Run("repo groups", func(t *testing.T) {
		if len(cfg.RepoGroups) != 1 {
			t.Fatalf("expected 1 repo group, got %d", len(cfg.RepoGroups))
		}
		rg := cfg.RepoGroups[0]
		if rg.Name != "main" {
			t.Errorf("Name = %q, want main", rg.Name)
		}
		if rg.Mode != "multi" {
			t.Errorf("Mode = %q, want multi", rg.Mode)
		}
	})

	t.Run("tokens", func(t *testing.T) {
		if cfg.Tokens.GitHub != "ghp_test1234567890abcdefghijklmnop" {
			t.Error("GitHub token mismatch")
		}
	})

	t.Run("atomic store", func(t *testing.T) {
		stored := Current()
		if stored == nil {
			t.Fatal("Current() returned nil after Load()")
		}
		if stored.Server.Listen != ":9090" {
			t.Errorf("Stored config mismatch: Listen = %q", stored.Server.Listen)
		}
	})
}

func TestConfigLoadWithEnvTokens(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "env_config.toml")
	oldPath := ConfigPath
	defer func() { ConfigPath = oldPath }()

	configToml := `
[server]
listen = ":8080"

[database]
path = "./test.db"

[auth]
jwt_secret = "env-secret"
token_expiry = "72h"

[events]
mode = "polling"
polling_interval = "30s"

[[repo_groups]]
name = "main"
mode = "multi"
github = "org/repo"
default_branch = "main"
`
	os.WriteFile(configPath, []byte(configToml), 0644)

	t.Setenv("ASIKA_GITHUB_TOKEN", "ghp_env_token_1234567890")
	t.Setenv("ASIKA_GITLAB_TOKEN", "glpat_env_token_987654")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Tokens.GitHub != "ghp_env_token_1234567890" {
		t.Errorf("GitHub token from env not applied: %q", cfg.Tokens.GitHub)
	}

	if cfg.Tokens.GitLab != "glpat_env_token_987654" {
		t.Errorf("GitLab token from env not applied: %q", cfg.Tokens.GitLab)
	}
}

func TestConfigValidate_MissingDatabase(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", Mode: "multi", GitHub: "org/repo"},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected validation error for missing database.path")
	}
}

func TestConfigValidate_MissingJWT(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", Mode: "multi", GitHub: "org/repo"},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected validation error for missing auth.jwt_secret")
	}
}

func TestConfigValidate_NoRepoGroups(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected validation error for missing repo_groups")
	}
}

func TestConfigValidate_InvalidMode(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", Mode: "invalid-mode", GitHub: "org/repo"},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected validation error for invalid mode")
	}
}

func TestConfigValidate_SingleModeNoMirror(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "docs", Mode: "single", GitHub: "org/docs"},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected validation error for single mode without mirror_platform")
	}
}

func TestConfigValidate_SingleModeWithMirror(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "docs", Mode: "single", MirrorPlatform: "github", GitHub: "org/docs"},
		},
	}

	err := validate(cfg)
	if err != nil {
		t.Errorf("expected no error for valid single mode, got %v", err)
	}
}

func TestConfigValidate_SpamEnabledNoThreshold(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", GitHub: "org/repo"},
		},
		Spam: models.SpamConfig{
			Enabled: true,
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Error("expected validation error for spam enabled without threshold")
	}
}

func TestConfigValidate_SpamEnabledValid(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", GitHub: "org/repo"},
		},
		Spam: models.SpamConfig{
			Enabled:          true,
			TimeWindow:       "10m",
			Threshold:        3,
			TriggerOnAuthor:  true,
			TriggerOnTitleKw: []string{"spam"},
		},
	}

	err := validate(cfg)
	if err != nil {
		t.Errorf("expected no error for valid spam config, got %v", err)
	}
}

func TestConfigHotReload_Store(t *testing.T) {
	cfg1 := &models.Config{
		Server: models.ServerConfig{Listen: ":8080", Mode: "debug"},
	}

	Store(cfg1)
	stored := Current()
	if stored == nil {
		t.Fatal("Current() returned nil")
	}
	if stored.Server.Listen != ":8080" {
		t.Errorf("First store: Listen = %q", stored.Server.Listen)
	}

	cfg2 := &models.Config{
		Server: models.ServerConfig{Listen: ":9090", Mode: "release"},
	}

	Store(cfg2)
	stored = Current()
	if stored.Server.Listen != ":9090" {
		t.Errorf("After hot reload: Listen = %q, want :9090", stored.Server.Listen)
	}
}

func TestConfigHotReload_AtomicSafety(t *testing.T) {
	cfg := &models.Config{
		LabelRules: []models.LabelRule{
			{Pattern: "*.go", Label: "go-code"},
			{Pattern: "*.md", Label: "documentation"},
		},
	}

	Store(cfg)
	stored := Current()
	if len(stored.LabelRules) != 2 {
		t.Errorf("expected 2 label rules, got %d", len(stored.LabelRules))
	}

	newRules := []models.LabelRule{
		{Pattern: "*.py", Label: "python"},
	}
	stored.LabelRules = newRules
	Store(stored)

	stored2 := Current()
	if len(stored2.LabelRules) != 1 {
		t.Errorf("after hot reload: expected 1 label rule, got %d", len(stored2.LabelRules))
	}
}

func TestDefaultConfigValues(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "minimal.toml")
	oldPath := ConfigPath
	defer func() { ConfigPath = oldPath }()

	configToml := `
[database]
path = "./db/minimal.db"

[auth]
jwt_secret = "minimal-secret"

[[repo_groups]]
name = "minimal"
mode = "multi"
github = "owner/repo"
default_branch = "main"
`
	os.WriteFile(configPath, []byte(configToml), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Server.Listen != ":8080" {
		t.Errorf("default Listen: got %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Server.Mode != "release" {
		t.Errorf("default Mode: got %q, want release", cfg.Server.Mode)
	}
	if cfg.MergeQueue.RequiredApprovals != 1 {
		t.Errorf("default RequiredApprovals: got %d, want 1", cfg.MergeQueue.RequiredApprovals)
	}
	if !cfg.MergeQueue.CICheckRequired {
		t.Error("default CICheckRequired should be true")
	}
}

func TestGenerateTokenExpiry(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"72h", "72h0m0s"},
		{"24h", "24h0m0s"},
		{"1h30m", "1h30m0s"},
		{"invalid", "72h0m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := GenerateTokenExpiry(tt.input)
			if d.String() != tt.want {
				t.Errorf("GenerateTokenExpiry(%q) = %q, want %q", tt.input, d.String(), tt.want)
			}
		})
	}
}

func TestSaveToFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "saved_config.toml")
	oldPath := ConfigPath
	defer func() { ConfigPath = oldPath }()

	ConfigPath = configPath

	cfg := models.Config{
		Server:   models.ServerConfig{Listen: ":8080", Mode: "debug"},
		Database: models.DatabaseConfig{Path: "./db/test.db"},
		Auth:     models.AuthConfig{JWTSecret: "saved-secret", TokenExpiry: "72h"},
		RepoGroups: []models.RepoGroupConfig{
			{Name: "main", Mode: "multi", GitHub: "org/repo", DefaultBranch: "main"},
		},
	}

	err := SaveToFile(cfg)
	if err != nil {
		t.Fatalf("SaveToFile() failed: %v", err)
	}

	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload saved config: %v", err)
	}
	if reloaded.Server.Listen != ":8080" {
		t.Errorf("reloaded Listen: got %q", reloaded.Server.Listen)
	}
}

func TestGetRepoGroup_MultiMode(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "multi-repo", Mode: "multi", GitHub: "org/github", GitLab: "group/gitlab", Gitea: "user/gitea"},
		},
	}

	groups := GetRepoGroups(cfg)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if g.Mode != "multi" {
		t.Errorf("Mode = %q, want multi", g.Mode)
	}
	if g.MirrorPlatform != "" {
		t.Errorf("multi mode should have empty MirrorPlatform")
	}
	if g.GitHub != "org/github" {
		t.Errorf("GitHub = %q", g.GitHub)
	}
}

func TestGetRepoGroup_SingleMode(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "single-repo", Mode: "single", MirrorPlatform: "github", GitHub: "org/docs"},
		},
	}

	groups := GetRepoGroups(cfg)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if g.Mode != "single" {
		t.Errorf("Mode = %q, want single", g.Mode)
	}
	if g.MirrorPlatform != "github" {
		t.Errorf("MirrorPlatform = %q, want github", g.MirrorPlatform)
	}
	if g.GitHub != "org/docs" {
		t.Errorf("GitHub = %q", g.GitHub)
	}
}

func TestGetRepoGroup_DefaultMode(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "default-mode", GitHub: "org/repo"},
		},
	}

	groups := GetRepoGroups(cfg)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	g := groups[0]
	if g.Mode != "multi" {
		t.Errorf("default Mode = %q, want multi", g.Mode)
	}
}

func TestGetRepoGroupByName_SingleMode(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "docs-only", Mode: "single", MirrorPlatform: "gitlab", GitLab: "group/docs"},
		},
	}

	g := GetRepoGroupByName(cfg, "docs-only")
	if g == nil {
		t.Fatal("GetRepoGroupByName returned nil")
	}
	if g.MirrorPlatform != "gitlab" {
		t.Errorf("MirrorPlatform = %q, want gitlab", g.MirrorPlatform)
	}
}

func TestGetOwnerRepo_SingleMode(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "single-gh", Mode: "single", MirrorPlatform: "github", GitHub: "myorg/myrepo"},
		},
	}

	g := GetRepoGroupByName(cfg, "single-gh")
	if g == nil {
		t.Fatal("group not found")
	}

	owner, repo := GetOwnerRepoFromGroup(g, "github")
	if owner != "myorg" {
		t.Errorf("owner = %q, want myorg", owner)
	}
	if repo != "myrepo" {
		t.Errorf("repo = %q, want myrepo", repo)
	}

	// Verify no repos for non-mirror platforms
	owner, repo = GetOwnerRepoFromGroup(g, "gitlab")
	if owner != "" || repo != "" {
		t.Errorf("single mode should return empty for non-configured platforms, got owner=%q repo=%q", owner, repo)
	}
}

func TestGetOwnerRepo_InvalidFormat(t *testing.T) {
	cfg := &models.Config{
		RepoGroups: []models.RepoGroupConfig{
			{Name: "invalid", Mode: "multi", GitHub: "no-slash-here"},
		},
	}

	g := GetRepoGroupByName(cfg, "invalid")
	owner, repo := GetOwnerRepoFromGroup(g, "github")
	if owner != "" || repo != "no-slash-here" {
		t.Errorf("invalid format: owner=%q repo=%q", owner, repo)
	}
}

func TestValidate_SingleModeValidWithMirrorPlatform(t *testing.T) {
	cfg := &models.Config{
		Database: models.DatabaseConfig{Path: "./test.db"},
		Auth:     models.AuthConfig{JWTSecret: "secret"},
		RepoGroups: []models.RepoGroupConfig{
			{
				Name:           "single-valid",
				Mode:           "single",
				MirrorPlatform: "github",
				GitHub:         "org/repo",
			},
		},
	}

	err := validate(cfg)
	if err != nil {
		t.Errorf("expected valid single mode config, got error: %v", err)
	}
}
