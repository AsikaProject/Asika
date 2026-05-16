package config

import (
	"strings"

	"asika/common/models"
)

func maskConfig(cfg *models.Config) *models.Config {
	masked := *cfg

	masked.Tokens = models.TokensConfig{
		GitHub:    maskToken(cfg.Tokens.GitHub),
		GitLab:    maskToken(cfg.Tokens.GitLab),
		Gitea:     maskToken(cfg.Tokens.Gitea),
		Forgejo:   maskToken(cfg.Tokens.Forgejo),
		Codeberg:  maskToken(cfg.Tokens.Codeberg),
		Bitbucket: maskToken(cfg.Tokens.Bitbucket),
		Gerrit: models.GerritAuth{
			URL:      cfg.Tokens.Gerrit.URL,
			Username: cfg.Tokens.Gerrit.Username,
			Password: maskToken(cfg.Tokens.Gerrit.Password),
		},
	}

	masked.Auth.JWTSecret = maskSecret(cfg.Auth.JWTSecret)
	masked.Auth.FingerprintSecret = maskSecret(cfg.Auth.FingerprintSecret)

	masked.Events.WebhookSecret = maskSecret(cfg.Events.WebhookSecret)

	for i := range masked.Notify {
		if masked.Notify[i].Config != nil {
			for k, v := range masked.Notify[i].Config {
				if isSecretNotifyKey(k) {
					if s, ok := v.(string); ok {
						masked.Notify[i].Config[k] = maskSecret(s)
					}
				}
			}
		}
	}

	if masked.Database.Type == "mongo" {
		masked.Database.Path = maskSecret(cfg.Database.Path)
	}

	masked.Feishu.AppSecret = maskSecret(cfg.Feishu.AppSecret)
	masked.Feishu.EncryptKey = maskSecret(cfg.Feishu.EncryptKey)

	masked.Telegram.Token = maskSecret(cfg.Telegram.Token)

	masked.Discord.Token = maskSecret(cfg.Discord.Token)

	masked.Slack.Token = maskSecret(cfg.Slack.Token)
	masked.Slack.AppToken = maskSecret(cfg.Slack.AppToken)

	return &masked
}

func isSecretNotifyKey(key string) bool {
	switch strings.ToLower(key) {
	case "password", "secret", "token", "api_key", "apikey", "webhook_url", "bot_token", "app_secret", "access_key", "private_key":
		return true
	}
	return false
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "****" + secret[len(secret)-4:]
}
