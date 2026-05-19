package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

var wizardCmd = &cobra.Command{
	Use:   "wizard",
	Short: "Run configuration wizard and apply to server",
	Long: `Run an interactive configuration wizard that connects to an asikad server,
steps through all configuration options, writes the config file on the server,
and creates the admin user in the database.

You can also provide an existing TOML config file via --file and only enter
the admin credentials interactively.`,
	Run: func(cmd *cobra.Command, args []string) {
		reader := bufio.NewReader(os.Stdin)
		server := GetServer(cmd)

		fmt.Printf("=== Asika Configuration Wizard ===\n")
		fmt.Printf("Server: %s\n\n", server)

		// Check if server is reachable
		fmt.Print("Checking server connection... ")
		if err := checkServer(server); err != nil {
			fmt.Printf("FAILED\nError: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")

		// Check if already initialized
		if initialized, err := checkInitialized(server); err == nil && initialized {
			fmt.Println("Server is already initialized. Use 'asika login' to authenticate.")
			return
		}

		var cfg map[string]interface{}

		// Try loading from --file first
		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			fmt.Printf("Loading config from %s...\n", filePath)
			data, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("Error reading file: %v\n", err)
				os.Exit(1)
			}
			if err := toml.Unmarshal(data, &cfg); err != nil {
				fmt.Printf("Error parsing TOML: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Config loaded. Only admin credentials needed.")
		} else {
			cfg = runInteractiveWizard(reader)
		}

		// Admin account (always asked, not stored in TOML)
		adminUser := prompt(reader, "Admin username", "admin")
		adminPass := promptSecret(reader, "Admin password")
		if adminPass == "" {
			fmt.Println("Error: admin password cannot be empty")
			os.Exit(1)
		}

		// Build payload matching wizardPayload struct
		payload := map[string]interface{}{
			"config": cfg,
			"users": []map[string]interface{}{
				{
					"username": adminUser,
					"password": adminPass,
					"role":     "admin",
				},
			},
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			fmt.Printf("Error encoding payload: %v\n", err)
			os.Exit(1)
		}

		fmt.Print("\nApplying configuration to server... ")
		url := fmt.Sprintf("%s/api/v1/wizard/step/complete", server)
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("FAILED\n%v\n", err)
			os.Exit(1)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("FAILED\n%v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if resp.StatusCode != http.StatusOK {
			errMsg := "unknown error"
			if e, ok := result["error"].(string); ok {
				errMsg = e
			}
			fmt.Printf("FAILED\nServer error: %s\n", errMsg)
			os.Exit(1)
		}

		fmt.Println("OK")
		fmt.Println("\n=== Setup Complete ===")
		fmt.Println("You can now login with:")
		fmt.Printf("  asika login -s %s\n", server)
	},
}

func runInteractiveWizard(reader *bufio.Reader) map[string]interface{} {
	cfg := make(map[string]interface{})

	// Step 1: Mode Selection
	fmt.Println("--- Step 1: Operation Mode ---")
	mode := prompt(reader, "Mode (single/multi)", "multi")
	cfg["mode"] = mode

	// Step 2: Database
	fmt.Println("\n--- Step 2: Database Configuration ---")
	dbType := prompt(reader, "Database type (bbolt/mongo)", "bbolt")
	dbConfig := map[string]string{"type": dbType}
	if dbType == "mongo" {
		dbPath := prompt(reader, "MongoDB connection string", "mongodb://localhost:27017")
		dbName := prompt(reader, "MongoDB database name", "asika")
		dbConfig["path"] = dbPath
		dbConfig["name"] = dbName
	} else {
		dbPath := prompt(reader, "Database path", "/var/lib/asika/asika.db")
		dbConfig["path"] = dbPath
	}
	cfg["database"] = dbConfig

	// Step 3: Platform Tokens
	fmt.Println("\n--- Step 3: Platform Tokens ---")
	githubToken := prompt(reader, "GitHub Token (leave empty to skip)", "")
	gitlabToken := prompt(reader, "GitLab Token (leave empty to skip)", "")
	giteaToken := prompt(reader, "Gitea Token (leave empty to skip)", "")
	forgejoToken := prompt(reader, "Forgejo Token (leave empty to skip)", "")
	codebergToken := prompt(reader, "Codeberg Token (leave empty to skip)", "")
	bitbucketToken := prompt(reader, "Bitbucket Token (leave empty to skip)", "")
	gerritURL := prompt(reader, "Gerrit URL (leave empty to skip)", "")
	gerritUser := ""
	gerritPass := ""
	if gerritURL != "" {
		gerritUser = prompt(reader, "Gerrit Username", "")
		gerritPass = promptSecret(reader, "Gerrit Password")
	}
	cfg["tokens"] = map[string]interface{}{
		"github":    githubToken,
		"gitlab":    gitlabToken,
		"gitea":     giteaToken,
		"forgejo":   forgejoToken,
		"codeberg":  codebergToken,
		"bitbucket": bitbucketToken,
		"gerrit": map[string]string{
			"url":      gerritURL,
			"username": gerritUser,
			"password": gerritPass,
		},
	}

	// Step 4: Repository Group
	fmt.Println("\n--- Step 4: Repository Group ---")
	groupName := prompt(reader, "Group name", "default")
	defaultBranch := prompt(reader, "Default branch", "main")
	githubRepo := prompt(reader, "GitHub Repository (owner/repo)", "")
	gitlabRepo := prompt(reader, "GitLab Repository (owner/repo)", "")
	giteaRepo := prompt(reader, "Gitea Repository (owner/repo)", "")
	forgejoRepo := prompt(reader, "Forgejo Repository (owner/repo)", "")
	codebergRepo := prompt(reader, "Codeberg Repository (owner/repo)", "")
	bitbucketRepo := prompt(reader, "Bitbucket Repository (owner/repo)", "")
	gerritRepo := prompt(reader, "Gerrit Repository (project~number)", "")
	mirrorPlatform := ""
	if mode == "single" {
		mirrorPlatform = prompt(reader, "Mirror Platform (github/gitlab/gitea/forgejo/codeberg/bitbucket/gerrit)", "github")
	}
	cfg["repo_groups"] = []map[string]interface{}{
		{
			"name":            groupName,
			"mode":            mode,
			"mirror_platform": mirrorPlatform,
			"github":          githubRepo,
			"gitlab":          gitlabRepo,
			"gitea":           giteaRepo,
			"forgejo":         forgejoRepo,
			"codeberg":        codebergRepo,
			"bitbucket":       bitbucketRepo,
			"gerrit":          gerritRepo,
			"default_branch":  defaultBranch,
			"merge_queue": map[string]interface{}{
				"required_approvals": 1,
				"ci_check_required":  true,
				"core_contributors":  []string{},
			},
		},
	}

	// Step 5: Notification Channels
	fmt.Println("\n--- Step 5: Notification Channels ---")
	notifyChannels := []map[string]interface{}{}

	// Telegram
	tgToken := prompt(reader, "Telegram Bot Token (leave empty to skip)", "")
	if tgToken != "" {
		tgChatIDs := splitAndTrim(prompt(reader, "Telegram Chat IDs (comma-separated)", ""))
		tgAdminIDs := splitAndTrim(prompt(reader, "Telegram Admin IDs (comma-separated)", ""))
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "telegram",
			"config": map[string]interface{}{
				"token":     tgToken,
				"chat_ids":  tgChatIDs,
				"admin_ids": tgAdminIDs,
			},
		})
	}

	// Feishu / Lark
	feishuAppID := prompt(reader, "Feishu App ID (leave empty to skip)", "")
	if feishuAppID != "" {
		feishuAppSecret := promptSecret(reader, "Feishu App Secret")
		feishuWebhook := prompt(reader, "Feishu Webhook URL (optional)", "")
		feishuAdminIDs := splitAndTrim(prompt(reader, "Feishu Admin IDs (comma-separated)", ""))
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "feishu",
			"config": map[string]interface{}{
				"app_id":      feishuAppID,
				"app_secret":  feishuAppSecret,
				"webhook_url": feishuWebhook,
				"admin_ids":   feishuAdminIDs,
			},
		})
	}

	// Discord
	discordToken := prompt(reader, "Discord Bot Token (leave empty to skip)", "")
	if discordToken != "" {
		discordChannel := prompt(reader, "Discord Channel ID", "")
		discordAdminIDs := splitAndTrim(prompt(reader, "Discord Admin IDs (comma-separated)", ""))
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "discord",
			"config": map[string]interface{}{
				"token":      discordToken,
				"channel_id": discordChannel,
				"admin_ids":  discordAdminIDs,
			},
		})
	}

	// Slack
	slackToken := prompt(reader, "Slack Bot Token (xoxb-...) (leave empty to skip)", "")
	if slackToken != "" {
		slackAppToken := prompt(reader, "Slack App Token (xapp-...) (optional, for Socket Mode)", "")
		slackAdminIDs := splitAndTrim(prompt(reader, "Slack Admin IDs (comma-separated)", ""))
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "slack_bot",
			"config": map[string]interface{}{
				"token":     slackToken,
				"app_token": slackAppToken,
				"admin_ids": slackAdminIDs,
			},
		})
	}

	// DingTalk
	dingtalkToken := prompt(reader, "DingTalk Access Token (leave empty to skip)", "")
	if dingtalkToken != "" {
		dingtalkSecret := promptSecret(reader, "DingTalk App Secret (optional)")
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "dingtalk",
			"config": map[string]interface{}{
				"token":  dingtalkToken,
				"secret": dingtalkSecret,
			},
		})
	}

	// WeCom (WeChat Work)
	wecomWebhook := prompt(reader, "WeCom Webhook Key (leave empty to skip)", "")
	if wecomWebhook != "" {
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "wecom",
			"config": map[string]interface{}{
				"webhook_key": wecomWebhook,
			},
		})
	}

	// MS Teams
	msteamsWebhook := prompt(reader, "MS Teams Webhook URL (leave empty to skip)", "")
	if msteamsWebhook != "" {
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "msteams",
			"config": map[string]interface{}{
				"webhook_url": msteamsWebhook,
			},
		})
	}

	// Generic Webhook
	customWebhook := prompt(reader, "Custom Webhook URL (leave empty to skip)", "")
	if customWebhook != "" {
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "webhook",
			"config": map[string]interface{}{
				"url": customWebhook,
			},
		})
	}

	// SMTP
	smtpHost := prompt(reader, "SMTP Host (leave empty to skip)", "")
	if smtpHost != "" {
		smtpPort := prompt(reader, "SMTP Port", "587")
		smtpUser := prompt(reader, "SMTP User", "")
		smtpPass := promptSecret(reader, "SMTP Password")
		smtpTo := splitAndTrim(prompt(reader, "SMTP Recipients (comma-separated)", ""))
		notifyChannels = append(notifyChannels, map[string]interface{}{
			"type": "smtp",
			"config": map[string]interface{}{
				"host":     smtpHost,
				"port":     smtpPort,
				"username": smtpUser,
				"password": smtpPass,
				"to":       smtpTo,
			},
		})
	}

	cfg["notify"] = notifyChannels

	// Step 6: Server & Auth
	fmt.Println("\n--- Step 6: Server & Auth ---")
	listen := prompt(reader, "Server listen address", ":8080")
	serverMode := prompt(reader, "Server mode (debug/release)", "release")
	jwtSecret := prompt(reader, "JWT Secret (leave empty to auto-generate)", "")
	cfg["server"] = map[string]string{
		"listen": listen,
		"mode":   serverMode,
	}
	cfg["auth"] = map[string]string{
		"jwt_secret":   jwtSecret,
		"token_expiry": "72h",
	}

	// Step 7: Self-Updates
	fmt.Println("\n--- Step 7: Self-Update Settings ---")
	updateCheck := prompt(reader, "Enable automatic update check? (y/n)", "n")
	if strings.ToLower(updateCheck) == "y" || strings.ToLower(updateCheck) == "yes" {
		updateInterval := prompt(reader, "Check interval", "24h")
		updateNotify := prompt(reader, "Notify on new version? (y/n)", "n")
		cfg["updates"] = map[string]interface{}{
			"check":         true,
			"interval":      updateInterval,
			"notify_on_new": strings.ToLower(updateNotify) == "y" || strings.ToLower(updateNotify) == "yes",
		}
	} else {
		cfg["updates"] = map[string]interface{}{
			"check":         false,
			"interval":      "24h",
			"notify_on_new": false,
		}
	}

	return cfg
}

func checkServer(server string) error {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/config", server))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func checkInitialized(server string) (bool, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/wizard", server))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode == http.StatusBadRequest {
		if errMsg, ok := result["error"].(string); ok && errMsg == "already initialized" {
			return true, nil
		}
	}
	return false, nil
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptSecret(reader *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func init() {
	wizardCmd.Flags().String("file", "", "Path to existing TOML config file (skip interactive config)")
	RootCmd.AddCommand(wizardCmd)
}
