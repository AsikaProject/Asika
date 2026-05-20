package init

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func ConfigureNotifications(reader *bufio.Reader) []map[string]interface{} {
	type notifyChannel struct {
		name  string
		label string
	}

	channels := []notifyChannel{
		{"telegram", "Telegram"},
		{"feishu", "Feishu / Lark"},
		{"discord", "Discord"},
		{"slack", "Slack"},
		{"dingtalk", "DingTalk"},
		{"wecom", "WeCom (WeChat Work)"},
		{"msteams", "MS Teams"},
		{"webhook", "Generic Webhook"},
		{"smtp", "SMTP"},
	}

	fmt.Println("Select notification channels to configure (comma-separated numbers, or Enter to skip):")
	for i, c := range channels {
		fmt.Printf("  [%d] %s\n", i+1, c.label)
	}
	fmt.Print("> ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var selected []int
	if input != "" {
		for _, part := range strings.Split(input, ",") {
			idx, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil && idx >= 1 && idx <= len(channels) {
				selected = append(selected, idx-1)
			}
		}
	}

	var notifyChannels []map[string]interface{}

	for _, idx := range selected {
		ch := channels[idx]
		switch ch.name {
		case "telegram":
			token := PromptSecret(reader, "Telegram Bot Token")
			if token != "" {
				chatIDs := SplitAndTrim(Prompt(reader, "Telegram Chat IDs (comma-separated)", ""))
				adminIDs := SplitAndTrim(Prompt(reader, "Telegram Admin IDs (comma-separated)", ""))
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "telegram",
					"config": map[string]interface{}{
						"token":     token,
						"chat_ids":  chatIDs,
						"admin_ids": adminIDs,
					},
				})
			}
		case "feishu":
			appID := Prompt(reader, "Feishu App ID", "")
			if appID != "" {
				appSecret := PromptSecret(reader, "Feishu App Secret")
				webhook := Prompt(reader, "Feishu Webhook URL (optional)", "")
				adminIDs := SplitAndTrim(Prompt(reader, "Feishu Admin IDs (comma-separated)", ""))
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "feishu",
					"config": map[string]interface{}{
						"app_id":      appID,
						"app_secret":  appSecret,
						"webhook_url": webhook,
						"admin_ids":   adminIDs,
					},
				})
			}
		case "discord":
			token := PromptSecret(reader, "Discord Bot Token")
			if token != "" {
				channelID := Prompt(reader, "Discord Channel ID", "")
				adminIDs := SplitAndTrim(Prompt(reader, "Discord Admin IDs (comma-separated)", ""))
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "discord",
					"config": map[string]interface{}{
						"token":      token,
						"channel_id": channelID,
						"admin_ids":  adminIDs,
					},
				})
			}
		case "slack":
			token := PromptSecret(reader, "Slack Bot Token (xoxb-...)")
			if token != "" {
				appToken := Prompt(reader, "Slack App Token (xapp-...) (optional)", "")
				adminIDs := SplitAndTrim(Prompt(reader, "Slack Admin IDs (comma-separated)", ""))
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "slack_bot",
					"config": map[string]interface{}{
						"token":     token,
						"app_token": appToken,
						"admin_ids": adminIDs,
					},
				})
			}
		case "dingtalk":
			token := PromptSecret(reader, "DingTalk Access Token")
			if token != "" {
				secret := PromptSecret(reader, "DingTalk App Secret (optional)")
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "dingtalk",
					"config": map[string]interface{}{
						"token":  token,
						"secret": secret,
					},
				})
			}
		case "wecom":
			webhook := Prompt(reader, "WeCom Webhook Key", "")
			if webhook != "" {
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "wecom",
					"config": map[string]interface{}{
						"webhook_key": webhook,
					},
				})
			}
		case "msteams":
			webhook := Prompt(reader, "MS Teams Webhook URL", "")
			if webhook != "" {
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "msteams",
					"config": map[string]interface{}{
						"webhook_url": webhook,
					},
				})
			}
		case "webhook":
			url := Prompt(reader, "Custom Webhook URL", "")
			if url != "" {
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "webhook",
					"config": map[string]interface{}{
						"url": url,
					},
				})
			}
		case "smtp":
			host := Prompt(reader, "SMTP Host", "")
			if host != "" {
				port := Prompt(reader, "SMTP Port", "587")
				user := Prompt(reader, "SMTP User", "")
				pass := PromptSecret(reader, "SMTP Password")
				to := SplitAndTrim(Prompt(reader, "SMTP Recipients (comma-separated)", ""))
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "smtp",
					"config": map[string]interface{}{
						"host":     host,
						"port":     port,
						"username": user,
						"password": pass,
						"to":       to,
					},
				})
			}
		}
	}

	return notifyChannels
}
