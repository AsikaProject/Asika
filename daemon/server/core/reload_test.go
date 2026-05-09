package core

import (
	"os"
	"path/filepath"
	"testing"

	"asika/common/config"
	"asika/common/models"
	"asika/common/platforms"
)

func TestReloadConfigAfterUpdate_FileNotFound(t *testing.T) {
	originalPath := config.ConfigPath
	config.ConfigPath = filepath.Join(t.TempDir(), "nonexistent.toml")
	t.Cleanup(func() { config.ConfigPath = originalPath })

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	ReloadConfigAfterUpdate(nil, clients)
}

func TestReloadConfigAfterUpdate_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte("{invalid toml"), 0644)

	originalPath := config.ConfigPath
	config.ConfigPath = path
	t.Cleanup(func() { config.ConfigPath = originalPath })

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	ReloadConfigAfterUpdate(nil, clients)
}

func TestReloadConfigAfterUpdate_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[server]
listen = ":8080"

[database]
path = "` + dir + `/asika.db"

[auth]
jwt_secret = "test-secret-for-reload"

[tokens]
github = "ghp_test"

[[repo_groups]]
name = "default"
github = "org/repo"
`
	os.WriteFile(path, []byte(content), 0644)

	originalPath := config.ConfigPath
	config.ConfigPath = path
	t.Cleanup(func() { config.ConfigPath = originalPath })

	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	ReloadConfigAfterUpdate(nil, clients)

	cfg := config.Current()
	if cfg == nil {
		t.Fatal("config should be loaded")
	}
	if len(cfg.RepoGroups) != 1 || cfg.RepoGroups[0].Name != "default" {
		t.Errorf("unexpected config: %+v", cfg.RepoGroups)
	}
}

func TestReloadConfigAfterUpdate_UnusedParam(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `[server]
listen = ":9090"

[database]
path = "` + dir + `/asika.db"

[auth]
jwt_secret = "test-secret-unused"

[[repo_groups]]
name = "default"
github = "org/repo"
`
	os.WriteFile(path, []byte(content), 0644)

	originalPath := config.ConfigPath
	config.ConfigPath = path
	t.Cleanup(func() { config.ConfigPath = originalPath })

	cfg := &models.Config{}
	clients := make(map[platforms.PlatformType]platforms.PlatformClient)
	ReloadConfigAfterUpdate(cfg, clients)
}
