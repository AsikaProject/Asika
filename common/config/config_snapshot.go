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
	maskedData, err := json.Marshal(masked)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	v := incrementConfigVersion()
	snapshot := struct {
		Config    json.RawMessage `json:"config"`
		CreatedAt time.Time       `json:"created_at"`
	}{
		Config:    json.RawMessage(maskedData),
		CreatedAt: time.Now(),
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}
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
		var wrapper struct {
			Config    json.RawMessage `json:"config"`
			CreatedAt time.Time       `json:"created_at"`
		}
		if err := json.Unmarshal(r.Data, &wrapper); err != nil {
			var cfg models.Config
			if err := json.Unmarshal(r.Data, &cfg); err != nil {
				continue
			}
			result = append(result, ConfigSnapshot{
				Version:   r.Version,
				Config:    &cfg,
				Timestamp: time.Time{},
			})
			continue
		}
		var cfg models.Config
		if err := json.Unmarshal(wrapper.Config, &cfg); err != nil {
			continue
		}
		result = append(result, ConfigSnapshot{
			Version:   r.Version,
			Config:    &cfg,
			Timestamp: wrapper.CreatedAt,
		})
	}
	return result, nil
}

func RollbackConfig(version int) error {
	data, err := db.GetConfigSnapshot(version)
	if err != nil {
		return fmt.Errorf("snapshot %d not found: %w", version, err)
	}
	var cfg models.Config
	var wrapper struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Config != nil {
		if err := json.Unmarshal(wrapper.Config, &cfg); err != nil {
			return fmt.Errorf("failed to unmarshal snapshot config: %w", err)
		}
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	current := Current()
	if current != nil {
		if cfg.Auth.JWTSecret == "***" || cfg.Auth.JWTSecret == "" {
			cfg.Auth.JWTSecret = current.Auth.JWTSecret
		}
		if cfg.Auth.FingerprintSecret == "***" || cfg.Auth.FingerprintSecret == "" {
			cfg.Auth.FingerprintSecret = current.Auth.FingerprintSecret
		}
		if cfg.Events.WebhookSecret == "***" || cfg.Events.WebhookSecret == "" {
			cfg.Events.WebhookSecret = current.Events.WebhookSecret
		}
		if cfg.Feishu.AppSecret == "***" || cfg.Feishu.AppSecret == "" {
			cfg.Feishu.AppSecret = current.Feishu.AppSecret
		}
		if cfg.Feishu.EncryptKey == "***" || cfg.Feishu.EncryptKey == "" {
			cfg.Feishu.EncryptKey = current.Feishu.EncryptKey
		}
		if cfg.Telegram.Token == "***" || cfg.Telegram.Token == "" {
			cfg.Telegram.Token = current.Telegram.Token
		}
		if cfg.Discord.Token == "***" || cfg.Discord.Token == "" {
			cfg.Discord.Token = current.Discord.Token
		}
		if cfg.Slack.Token == "***" || cfg.Slack.Token == "" {
			cfg.Slack.Token = current.Slack.Token
		}
		if cfg.Slack.AppToken == "***" || cfg.Slack.AppToken == "" {
			cfg.Slack.AppToken = current.Slack.AppToken
		}
		if cfg.Database.Type == "mongo" && (cfg.Database.Path == "***" || cfg.Database.Path == "") {
			cfg.Database.Path = current.Database.Path
		}
		mergeMaskedTokens(&cfg.Tokens, &current.Tokens)
		mergeMaskedNotify(&cfg.Notify, &current.Notify)
	}

	if err := SaveToFile(cfg); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	Store(&cfg)
	slog.Info("config rolled back", "version", version)
	return nil
}

func mergeMaskedTokens(cfg, current *models.TokensConfig) {
	if cfg.GitHub == "***" || cfg.GitHub == "" {
		cfg.GitHub = current.GitHub
	}
	if cfg.GitLab == "***" || cfg.GitLab == "" {
		cfg.GitLab = current.GitLab
	}
	if cfg.Gitea == "***" || cfg.Gitea == "" {
		cfg.Gitea = current.Gitea
	}
	if cfg.Forgejo == "***" || cfg.Forgejo == "" {
		cfg.Forgejo = current.Forgejo
	}
	if cfg.Codeberg == "***" || cfg.Codeberg == "" {
		cfg.Codeberg = current.Codeberg
	}
	if cfg.Bitbucket == "***" || cfg.Bitbucket == "" {
		cfg.Bitbucket = current.Bitbucket
	}
	if cfg.Gerrit.Password == "***" || cfg.Gerrit.Password == "" {
		cfg.Gerrit.Password = current.Gerrit.Password
	}
}

func mergeMaskedNotify(cfg, current *[]models.NotifyConfig) {
	if len(*cfg) != len(*current) {
		return
	}
	for i := range *cfg {
		if (*cfg)[i].Config == nil || (*current)[i].Config == nil {
			continue
		}
		for k := range (*cfg)[i].Config {
			if v, ok := (*cfg)[i].Config[k].(string); ok && (v == "***" || v == "") {
				if cv, exists := (*current)[i].Config[k]; exists {
					(*cfg)[i].Config[k] = cv
				}
			}
		}
	}
}

type ConfigSnapshot struct {
	Version   int            `json:"version"`
	Config    *models.Config `json:"config"`
	Timestamp time.Time      `json:"timestamp"`
}
