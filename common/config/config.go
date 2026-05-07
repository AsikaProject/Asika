package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/google/uuid"

	"asika/common/db"
	"asika/common/models"
)

var (
    current    atomic.Value
    ConfigPath string
)

// Store stores the configuration atomically
func Store(cfg *models.Config) {
    current.Store(cfg)
}

// Current returns the current configuration
func Current() *models.Config {
    v := current.Load()
    if v == nil {
        return nil
    }
    return v.(*models.Config)
}

// Load loads configuration from the TOML file
func Load(path string) (*models.Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file: %w", err)
    }

	cfg := &models.Config{
		Server: models.ServerConfig{
			Listen:                 ":8080",
			Mode:                   "release",
			CORSOrigins:            []string{},
			RateLimitEnabled:       true,
			RateLimitRPS:           10,
			RateLimitBurst:         20,
			ReadTimeoutSeconds:     30,
			WriteTimeoutSeconds:    30,
			ShutdownTimeoutSeconds: 30,
			MetricsLogInterval:     "5m",
		},
		MergeQueue: models.MergeQueueConfig{
			RequiredApprovals: 1,
			CICheckRequired:   true,
		},
		Updates: models.UpdatesConfig{
			Check:       false,
			Interval:    "24h",
			NotifyOnNew: false,
		},
		Stale: models.StaleConfig{
			Enabled:          false,
			CheckInterval:    "6h",
			DaysUntilStale:   21,
			DaysUntilClose:   0,
			StaleLabel:       "stale",
			ExemptLabels:     []string{"long-term"},
			NotifyOnStale:    true,
			RemoveOnActivity: true,
			SkipDraftPRs:     true,
		},
	}

    if err := toml.Unmarshal(data, cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config file: %w", err)
    }

    // Apply environment variable overrides for tokens
    if token := os.Getenv("ASIKA_GITHUB_TOKEN"); token != "" {
        cfg.Tokens.GitHub = token
    }
    if token := os.Getenv("ASIKA_GITLAB_TOKEN"); token != "" {
        cfg.Tokens.GitLab = token
    }
    if token := os.Getenv("ASIKA_GITEA_TOKEN"); token != "" {
        cfg.Tokens.Gitea = token
    }

    // Validate configuration
    if err := validate(cfg); err != nil {
        return nil, err
    }

    Store(cfg)
    ConfigPath = path
    return cfg, nil
}

// validate validates the configuration
func validate(cfg *models.Config) error {
	// Check repo groups
	if len(cfg.RepoGroups) == 0 {
		return fmt.Errorf("at least one repo_groups entry is required")
	}

	for _, rg := range cfg.RepoGroups {
		if rg.Mode != "single" && rg.Mode != "multi" && rg.Mode != "" {
			return fmt.Errorf("invalid mode for repo group %s: %s (must be 'single' or 'multi')", rg.Name, rg.Mode)
		}
		mode := rg.Mode
		if mode == "" {
			mode = "multi" // default
		}
		if mode == "single" {
			if rg.GitHub == "" && rg.GitLab == "" && rg.Gitea == "" && rg.Forgejo == "" && rg.Codeberg == "" && rg.Bitbucket == "" {
				return fmt.Errorf("single mode repo group %s requires at least one platform repo to be set", rg.Name)
			}
			if rg.MirrorPlatform == "" {
				return fmt.Errorf("single mode repo group %s requires mirror_platform to be set", rg.Name)
			}
		}
	}

	if cfg.Database.Path == "" {
		return fmt.Errorf("database.path is required")
	}

	if cfg.Auth.JWTSecret == "" {
		return fmt.Errorf("auth.jwt_secret is required")
	}

	if cfg.Spam.Enabled {
		if cfg.Spam.Threshold <= 0 {
			return fmt.Errorf("spam.threshold must be greater than 0 when spam is enabled")
		}
		if cfg.Spam.TimeWindow == "" {
			return fmt.Errorf("spam.time_window is required when spam is enabled")
		}
		if _, err := time.ParseDuration(cfg.Spam.TimeWindow); err != nil {
			return fmt.Errorf("invalid spam.time_window: %w", err)
		}
		if !cfg.Spam.TriggerOnAuthor && len(cfg.Spam.TriggerOnTitleKw) == 0 {
			return fmt.Errorf("spam requires at least one trigger: trigger_on_author or trigger_on_title_kw")
		}
	}

	return nil
}

// GetRepoGroups returns all repo groups
func GetRepoGroups(cfg *models.Config) []models.RepoGroup {
    groups := make([]models.RepoGroup, len(cfg.RepoGroups))
	for i, rg := range cfg.RepoGroups {
		mode := rg.Mode
		if mode == "" {
			mode = "multi" // default
		}
		groups[i] = models.RepoGroup{
			Name:           rg.Name,
			Mode:           mode,
			MirrorPlatform: rg.MirrorPlatform,
			GitHub:         rg.GitHub,
			GitLab:         rg.GitLab,
			Gitea:          rg.Gitea,
			Forgejo:        rg.Forgejo,
		Codeberg:       rg.Codeberg,
		Bitbucket:      rg.Bitbucket,
		DefaultBranch:  rg.DefaultBranch,
			HookPath:       rg.HookPath,
			CIProvider:     rg.CIProvider,
			MergeQueue:     rg.MergeQueue,
		}
	}
	return groups
}

// GetRepoGroupByName finds a repo group by name
func GetRepoGroupByName(cfg *models.Config, name string) *models.RepoGroup {
    var defaultGroup *models.RepoGroup
    for i := range cfg.RepoGroups {
        rg := &cfg.RepoGroups[i]
        mode := rg.Mode
        if mode == "" {
            mode = "multi"
        }
          if rg.Name == name {
             return &models.RepoGroup{
                 Name:           rg.Name,
                 Mode:           mode,
                 MirrorPlatform: rg.MirrorPlatform,
                 GitHub:         rg.GitHub,
                 GitLab:         rg.GitLab,
                 Gitea:          rg.Gitea,
                 Forgejo:        rg.Forgejo,
                 Codeberg:       rg.Codeberg,
                 Bitbucket:      rg.Bitbucket,
                 DefaultBranch:  rg.DefaultBranch,
                 HookPath:       rg.HookPath,
                 CIProvider:     rg.CIProvider,
                 MergeQueue:     rg.MergeQueue,
             }
         }
         if rg.Name == "default" {
             defaultGroup = &models.RepoGroup{
                 Name:           rg.Name,
                 Mode:           mode,
                 MirrorPlatform: rg.MirrorPlatform,
                 GitHub:         rg.GitHub,
                 GitLab:         rg.GitLab,
                 Gitea:          rg.Gitea,
                 Forgejo:        rg.Forgejo,
                 Codeberg:       rg.Codeberg,
                 Bitbucket:      rg.Bitbucket,
                 DefaultBranch:  rg.DefaultBranch,
                 HookPath:       rg.HookPath,
                 CIProvider:     rg.CIProvider,
                 MergeQueue:     rg.MergeQueue,
             }
        }
    }
    if defaultGroup != nil {
        slog.Info("repo group not found, falling back to default", "requested", name)
        return defaultGroup
    }
    return nil
}

// GetOwnerRepoFromGroup returns the owner/repo for a platform in a repo group
func GetOwnerRepoFromGroup(group *models.RepoGroup, platform string) (owner, repo string) {
    var repoPath string
    switch platform {
    case "github":
        repoPath = group.GitHub
    case "gitlab":
        repoPath = group.GitLab
    case "gitea":
        repoPath = group.Gitea
    case "forgejo":
        repoPath = group.Forgejo
    case "codeberg":
        repoPath = group.Codeberg
    case "bitbucket":
        repoPath = group.Bitbucket
    }
    if repoPath == "" {
        return "", ""
    }
    idx := strings.LastIndex(repoPath, "/")
    if idx < 0 {
        return "", repoPath
    }
    return repoPath[:idx], repoPath[idx+1:]
}

// GetPlatformForGroup determines the platform for a repo group.
// In single mode, it returns the MirrorPlatform (the authoritative source).
// In multi mode, it returns the first configured platform in priority order
// (github > gitlab > gitea > forgejo > codeberg > bitbucket).
func GetPlatformForGroup(group *models.RepoGroup) string {
	if group.Mode == "single" && group.MirrorPlatform != "" {
		return group.MirrorPlatform
	}
	if group.GitHub != "" {
		return "github"
	}
	if group.GitLab != "" {
		return "gitlab"
	}
	if group.Gitea != "" {
		return "gitea"
	}
	if group.Forgejo != "" {
		return "forgejo"
	}
	if group.Codeberg != "" {
		return "codeberg"
	}
	if group.Bitbucket != "" {
		return "bitbucket"
	}
	return ""
}

// GetCloneURL builds the HTTPS clone URL for a platform repository.
func GetCloneURL(platform, owner, repo string) string {
	var baseURL string
	switch platform {
	case "github":
		cfg := Current()
		if cfg != nil && cfg.GitHubBaseURL != "" {
			baseURL = strings.TrimSuffix(cfg.GitHubBaseURL, "/")
		} else {
			baseURL = "https://github.com"
		}
	case "gitlab":
		cfg := Current()
		if cfg != nil && cfg.GitLabBaseURL != "" {
			baseURL = strings.TrimSuffix(cfg.GitLabBaseURL, "/")
		} else {
			slog.Warn("gitlab_base_url not configured, defaulting to https://gitlab.com")
			baseURL = "https://gitlab.com"
		}
	case "gitea":
		cfg := Current()
		if cfg != nil && cfg.GiteaBaseURL != "" {
			baseURL = strings.TrimSuffix(cfg.GiteaBaseURL, "/")
		} else {
			slog.Warn("gitea_base_url not configured, defaulting to https://gitea.com")
			baseURL = "https://gitea.com"
		}
	case "forgejo", "codeberg":
		cfg := Current()
		if cfg != nil && cfg.ForgejoBaseURL != "" {
			baseURL = strings.TrimSuffix(cfg.ForgejoBaseURL, "/")
		} else {
			baseURL = "https://codeberg.org"
		}
	case "bitbucket":
		baseURL = "https://bitbucket.org"
	}
	return fmt.Sprintf("%s/%s/%s", baseURL, owner, repo)
}

// GetToken returns the API token for a given platform.
func GetToken(cfg *models.Config, platform string) string {
	if cfg == nil {
		return ""
	}
	switch platform {
	case "github":
		return cfg.Tokens.GitHub
	case "gitlab":
		return cfg.Tokens.GitLab
	case "gitea":
		return cfg.Tokens.Gitea
	case "forgejo":
		return cfg.Tokens.Forgejo
	case "codeberg":
		return cfg.Tokens.Codeberg
	case "bitbucket":
		return cfg.Tokens.Bitbucket
	}
	return ""
}

// GenerateTokenExpiry parses the token expiry duration
func GenerateTokenExpiry(expiry string) time.Duration {
    d, err := time.ParseDuration(expiry)
    if err != nil {
        slog.Warn("invalid token_expiry, using default 72h", "error", err)
        d = 72 * time.Hour
    }
    return d
}

// GenerateUUID generates a new UUID string
func GenerateUUID() string {
    return uuid.New().String()
}

// SaveToFile writes the config to the configured file path
func SaveToFile(cfg models.Config) error {
	path := ConfigPath
	if path == "" {
		path = os.Getenv("ASIKA_CONFIG")
		if path == "" {
			path = "/etc/asika_config.toml"
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	ConfigPath = path
	return nil
}

const configVersionKey = "__config_version__"

// currentConfigVersion returns the latest stored config version number.
func currentConfigVersion() int {
	data, err := db.Get(db.BucketConfig, configVersionKey)
	if err != nil || data == nil {
		return 0
	}
	var v int
	fmt.Sscanf(string(data), "%d", &v)
	return v
}

// incrementConfigVersion bumps the version counter and returns the new value.
func incrementConfigVersion() int {
	v := currentConfigVersion() + 1
	db.Put(db.BucketConfig, configVersionKey, []byte(fmt.Sprintf("%d", v)))
	return v
}

// SaveConfigSnapshot stores the current config as a versioned snapshot in bbolt.
// Keeps up to maxSnapshots (default 20) entries, pruning oldest.
func SaveConfigSnapshot() error {
	cfg := Current()
	if cfg == nil {
		return fmt.Errorf("no config loaded")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	v := incrementConfigVersion()
	if err := db.PutConfigSnapshot(v, data); err != nil {
		return fmt.Errorf("failed to store snapshot: %w", err)
	}
	pruneConfigSnapshots(20)
	slog.Info("config snapshot saved", "version", v)
	return nil
}

// pruneConfigSnapshots keeps only the latest N snapshots.
func pruneConfigSnapshots(keep int) {
	snapshots, err := db.ListConfigSnapshots(0)
	if err != nil {
		return
	}
	for i := keep; i < len(snapshots); i++ {
		key := fmt.Sprintf("%06d", snapshots[i].Version)
		db.Delete(db.BucketConfigHistory, key)
	}
}

// ListConfigVersions returns the latest N config version snapshots.
func ListConfigVersions(limit int) ([]ConfigSnapshot, error) {
	raw, err := db.ListConfigSnapshots(limit)
	if err != nil {
		return nil, err
	}
	result := make([]ConfigSnapshot, 0, len(raw))
	for _, r := range raw {
		var cfg models.Config
		if err := json.Unmarshal(r.Data, &cfg); err != nil {
			continue
		}
		result = append(result, ConfigSnapshot{
			Version:   r.Version,
			Config:    &cfg,
			Timestamp: cfgTime(r.Data),
		})
	}
	return result, nil
}

// cfgTime extracts a timestamp from the stored JSON for display.
func cfgTime(data []byte) time.Time {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return time.Time{}
	}
	// Use current time as fallback; precise time isn't critical for snapshots
	return time.Now()
}

// RollbackConfig restores a config from a versioned snapshot and writes it to disk.
func RollbackConfig(version int) error {
	data, err := db.GetConfigSnapshot(version)
	if err != nil {
		return fmt.Errorf("snapshot %d not found: %w", version, err)
	}
	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}
 	if err := SaveToFile(cfg); err != nil {
 		return fmt.Errorf("failed to write config: %w", err)
 	}
 	Store(&cfg)
 	slog.Info("config rolled back", "version", version)
	return nil
}

// ConfigSnapshot represents a stored config version.
type ConfigSnapshot struct {
	Version   int            `json:"version"`
	Config    *models.Config `json:"config"`
	Timestamp time.Time      `json:"timestamp"`
}