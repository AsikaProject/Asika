package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"asika/common/db"
	"asika/common/models"
)

const configVersionKey = "__config_version__"

func CurrentCfgVersion() int {
	return currentConfigVersion()
}

func currentConfigVersion() int {
	data, err := db.Get(db.BucketConfig, configVersionKey)
	if err != nil || data == nil {
		return 0
	}
	var v int
	fmt.Sscanf(string(data), "%d", &v)
	return v
}

func incrementConfigVersion() int {
	v := currentConfigVersion() + 1
	db.Put(db.BucketConfig, configVersionKey, []byte(fmt.Sprintf("%d", v)))
	return v
}

func SaveConfigSnapshot() error {
	cfg := Current()
	if cfg == nil {
		return fmt.Errorf("no config loaded")
	}
	masked := maskConfigForStorage(cfg)
	data, err := json.Marshal(masked)
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

func maskConfigForStorage(cfg *models.Config) *models.Config {
	masked := *cfg
	masked.Tokens = models.TokensConfig{
		GitHub:    "***",
		GitLab:    "***",
		Gitea:     "***",
		Forgejo:   "***",
		Codeberg:  "***",
		Bitbucket: "***",
		Gerrit: models.GerritAuth{
			URL:      cfg.Tokens.Gerrit.URL,
			Username: cfg.Tokens.Gerrit.Username,
			Password: "***",
		},
	}
	masked.Auth.JWTSecret = "***"
	masked.Auth.FingerprintSecret = "***"
	masked.Events.WebhookSecret = "***"
	masked.Feishu.AppSecret = "***"
	masked.Feishu.EncryptKey = "***"
	masked.Telegram.Token = "***"
	masked.Discord.Token = "***"
	masked.Slack.Token = "***"
	masked.Slack.AppToken = "***"
	for i := range masked.Notify {
		if masked.Notify[i].Config != nil {
			for k := range masked.Notify[i].Config {
				masked.Notify[i].Config[k] = "***"
			}
		}
	}
	if masked.Database.Type == "mongo" {
		masked.Database.Path = "***"
	}
	return &masked
}

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

func cfgTime(data []byte) time.Time {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return time.Time{}
	}
	return time.Now()
}

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

type ConfigSnapshot struct {
	Version   int            `json:"version"`
	Config    *models.Config `json:"config"`
	Timestamp time.Time      `json:"timestamp"`
}
