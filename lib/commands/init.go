package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
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

		printHeader(server)

		if server == "http://localhost:8080" {
			fmt.Printf("  Target server: %s\n", server)
			confirm := prompt(reader, "Is this the correct server? (y/n)", "y")
			if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
				server = prompt(reader, "Enter server address", server)
			}
			fmt.Println()
		}

		fmt.Print("Checking server connection... ")
		if err := checkServer(server); err != nil {
			fmt.Printf("FAILED\nError: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OK")

		fmt.Print("Checking server status... ")
		initialized, err := checkInitialized(server)
		if err != nil {
			fmt.Printf("FAILED\nError: %v\n", err)
			fmt.Println("Could not determine server initialization status.")
			os.Exit(1)
		}
		if initialized {
			fmt.Println("INITIALIZED")
			fmt.Println("\nServer is already initialized. Use 'asika login' to authenticate.")
			return
		}
		fmt.Println("NOT INITIALIZED")

		printDisclaimer()

		var cfg map[string]interface{}

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

		printSummary(cfg)

		confirm := prompt(reader, "Apply this configuration? (y/n)", "n")
		if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}

		adminUser := prompt(reader, "Admin username", "admin")
		adminPass := promptSecret(reader, "Admin password")
		if adminPass == "" {
			fmt.Println("Error: admin password cannot be empty")
			os.Exit(1)
		}

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

	stepHeader(1, "Operation Mode")
	mode := choose(reader, "Select operation mode", []string{"multi", "single"}, "multi")
	cfg["mode"] = mode

	stepHeader(2, "Database Configuration")
	dbType := choose(reader, "Select database type", []string{"bbolt", "mongo"}, "bbolt")
	dbConfig := map[string]string{"type": dbType}
	if dbType == "mongo" {
		dbConfig["path"] = prompt(reader, "MongoDB connection string", "mongodb://localhost:27017")
		dbConfig["name"] = prompt(reader, "MongoDB database name", "asika")
	} else {
		dbConfig["path"] = prompt(reader, "Database path", "/var/lib/asika/asika.db")
	}
	cfg["database"] = dbConfig

	stepHeader(3, "Platform Tokens")
	cfg["tokens"] = configurePlatforms(reader)

	stepHeader(4, "Repository Group")
	cfg["repo_groups"] = configureRepoGroup(reader, mode)

	stepHeader(5, "Notification Channels")
	cfg["notify"] = configureNotifications(reader)

	stepHeader(6, "Server & Auth")
	listen := prompt(reader, "Server listen address", ":8080")
	serverMode := choose(reader, "Server mode", []string{"release", "debug"}, "release")
	jwtSecret := prompt(reader, "JWT Secret (leave empty to auto-generate)", "")
	cfg["server"] = map[string]string{
		"listen": listen,
		"mode":   serverMode,
	}
	cfg["auth"] = map[string]string{
		"jwt_secret":   jwtSecret,
		"token_expiry": "72h",
	}

	stepHeader(7, "Self-Update Settings")
	enableUpdates := confirm(reader, "Enable automatic update check?")
	if enableUpdates {
		updateInterval := prompt(reader, "Check interval", "24h")
		updateNotify := confirm(reader, "Notify on new version?")
		cfg["updates"] = map[string]interface{}{
			"check":         true,
			"interval":      updateInterval,
			"notify_on_new": updateNotify,
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

func configurePlatforms(reader *bufio.Reader) map[string]interface{} {
	tokens := map[string]interface{}{}

	type platformInfo struct {
		name  string
		label string
		key   string
	}

	platforms := []platformInfo{
		{"github", "GitHub", "token"},
		{"gitlab", "GitLab", "token"},
		{"gitea", "Gitea", "token"},
		{"forgejo", "Forgejo", "token"},
		{"codeberg", "Codeberg", "token"},
		{"bitbucket", "Bitbucket", "token"},
	}

	var selected []int
	fmt.Println("Select platforms to configure (comma-separated numbers, or Enter to skip):")
	for i, p := range platforms {
		fmt.Printf("  [%d] %s\n", i+1, p.label)
	}
	fmt.Print("> ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		for _, part := range strings.Split(input, ",") {
			idx, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil && idx >= 1 && idx <= len(platforms) {
				selected = append(selected, idx-1)
			}
		}
	}

	for _, idx := range selected {
		p := platforms[idx]
		token := promptSecret(reader, fmt.Sprintf("%s Token", p.name))
		tokens[p.key] = token
	}

	// Gerrit is special (URL + username + password)
	if confirm(reader, "Configure Gerrit?") {
		gerritURL := prompt(reader, "Gerrit URL", "")
		if gerritURL != "" {
			gerritUser := prompt(reader, "Gerrit Username", "")
			gerritPass := promptSecret(reader, "Gerrit Password")
			tokens["gerrit"] = map[string]string{
				"url":      gerritURL,
				"username": gerritUser,
				"password": gerritPass,
			}
		}
	}

	return tokens
}

func configureRepoGroup(reader *bufio.Reader, mode string) []map[string]interface{} {
	groupName := prompt(reader, "Group name", "default")
	defaultBranch := prompt(reader, "Default branch", "main")

	type platformRepo struct {
		name  string
		label string
	}

	repoPlatforms := []platformRepo{
		{"github", "GitHub"},
		{"gitlab", "GitLab"},
		{"gitea", "Gitea"},
		{"forgejo", "Forgejo"},
		{"codeberg", "Codeberg"},
		{"bitbucket", "Bitbucket"},
		{"gerrit", "Gerrit"},
	}

	repos := map[string]string{}
	fmt.Println("Select platforms to configure repositories for (comma-separated numbers, or Enter to skip):")
	for i, p := range repoPlatforms {
		fmt.Printf("  [%d] %s\n", i+1, p.label)
	}
	fmt.Print("> ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var selected []int
	if input != "" {
		for _, part := range strings.Split(input, ",") {
			idx, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil && idx >= 1 && idx <= len(repoPlatforms) {
				selected = append(selected, idx-1)
			}
		}
	}

	for _, idx := range selected {
		p := repoPlatforms[idx]
		repo := prompt(reader, fmt.Sprintf("%s Repository (owner/repo)", p.label), "")
		if repo != "" {
			repos[p.name] = repo
		}
	}

	mirrorPlatform := ""
	if mode == "single" {
		mirrorPlatform = choose(reader, "Mirror platform", []string{"github", "gitlab", "gitea", "forgejo", "codeberg", "bitbucket", "gerrit"}, "github")
	}

	return []map[string]interface{}{
		{
			"name":            groupName,
			"mode":            mode,
			"mirror_platform": mirrorPlatform,
			"github":          repos["github"],
			"gitlab":          repos["gitlab"],
			"gitea":           repos["gitea"],
			"forgejo":         repos["forgejo"],
			"codeberg":        repos["codeberg"],
			"bitbucket":       repos["bitbucket"],
			"gerrit":          repos["gerrit"],
			"default_branch":  defaultBranch,
			"merge_queue": map[string]interface{}{
				"required_approvals": 1,
				"ci_check_required":  true,
				"core_contributors":  []string{},
			},
		},
	}
}

func configureNotifications(reader *bufio.Reader) []map[string]interface{} {
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
			token := promptSecret(reader, "Telegram Bot Token")
			if token != "" {
				chatIDs := splitAndTrim(prompt(reader, "Telegram Chat IDs (comma-separated)", ""))
				adminIDs := splitAndTrim(prompt(reader, "Telegram Admin IDs (comma-separated)", ""))
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
			appID := prompt(reader, "Feishu App ID", "")
			if appID != "" {
				appSecret := promptSecret(reader, "Feishu App Secret")
				webhook := prompt(reader, "Feishu Webhook URL (optional)", "")
				adminIDs := splitAndTrim(prompt(reader, "Feishu Admin IDs (comma-separated)", ""))
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
			token := promptSecret(reader, "Discord Bot Token")
			if token != "" {
				channelID := prompt(reader, "Discord Channel ID", "")
				adminIDs := splitAndTrim(prompt(reader, "Discord Admin IDs (comma-separated)", ""))
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
			token := promptSecret(reader, "Slack Bot Token (xoxb-...)")
			if token != "" {
				appToken := prompt(reader, "Slack App Token (xapp-...) (optional)", "")
				adminIDs := splitAndTrim(prompt(reader, "Slack Admin IDs (comma-separated)", ""))
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
			token := promptSecret(reader, "DingTalk Access Token")
			if token != "" {
				secret := promptSecret(reader, "DingTalk App Secret (optional)")
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "dingtalk",
					"config": map[string]interface{}{
						"token":  token,
						"secret": secret,
					},
				})
			}
		case "wecom":
			webhook := prompt(reader, "WeCom Webhook Key", "")
			if webhook != "" {
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "wecom",
					"config": map[string]interface{}{
						"webhook_key": webhook,
					},
				})
			}
		case "msteams":
			webhook := prompt(reader, "MS Teams Webhook URL", "")
			if webhook != "" {
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "msteams",
					"config": map[string]interface{}{
						"webhook_url": webhook,
					},
				})
			}
		case "webhook":
			url := prompt(reader, "Custom Webhook URL", "")
			if url != "" {
				notifyChannels = append(notifyChannels, map[string]interface{}{
					"type": "webhook",
					"config": map[string]interface{}{
						"url": url,
					},
				})
			}
		case "smtp":
			host := prompt(reader, "SMTP Host", "")
			if host != "" {
				port := prompt(reader, "SMTP Port", "587")
				user := prompt(reader, "SMTP User", "")
				pass := promptSecret(reader, "SMTP Password")
				to := splitAndTrim(prompt(reader, "SMTP Recipients (comma-separated)", ""))
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

func printHeader(server string) {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     Asika Configuration Wizard       ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Printf("  Server: %s\n\n", server)
}

func printDisclaimer() {
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  RISK DISCLOSURE & DISCLAIMER")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  By proceeding, you acknowledge:")
	fmt.Println("  • This wizard will configure an asikad")
	fmt.Println("    server with the credentials you")
	fmt.Println("    provide (tokens, passwords, etc.).")
	fmt.Println("  • Tokens will be stored on the server")
	fmt.Println("    and used to access your repos.")
	fmt.Println("  • You are responsible for securing")
	fmt.Println("    the server and its data.")
	fmt.Println("────────────────────────────────────────")
	fmt.Println()
}

func printSummary(cfg map[string]interface{}) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║       Configuration Summary          ║")
	fmt.Println("╚══════════════════════════════════════╝")

	fmt.Printf("  Mode: %v\n", cfg["mode"])

	if db, ok := cfg["database"].(map[string]string); ok {
		fmt.Printf("  Database: %s (%s)\n", db["type"], db["path"])
	}

	if tokens, ok := cfg["tokens"].(map[string]interface{}); ok {
		var configured []string
		for k, v := range tokens {
			switch val := v.(type) {
			case string:
				if val != "" {
					configured = append(configured, k)
				}
			case map[string]string:
				if val["url"] != "" {
					configured = append(configured, k)
				}
			}
		}
		if len(configured) > 0 {
			fmt.Printf("  Platforms: %s\n", strings.Join(configured, ", "))
		} else {
			fmt.Println("  Platforms: (none)")
		}
	}

	if groups, ok := cfg["repo_groups"].([]map[string]interface{}); ok && len(groups) > 0 {
		g := groups[0]
		fmt.Printf("  Repo Group: %v (branch: %v)\n", g["name"], g["default_branch"])
		var repos []string
		for _, p := range []string{"github", "gitlab", "gitea", "forgejo", "codeberg", "bitbucket", "gerrit"} {
			if v, ok := g[p].(string); ok && v != "" {
				repos = append(repos, fmt.Sprintf("%s=%s", p, v))
			}
		}
		if len(repos) > 0 {
			fmt.Printf("  Repos: %s\n", strings.Join(repos, ", "))
		}
	}

	if notify, ok := cfg["notify"].([]map[string]interface{}); ok && len(notify) > 0 {
		var types []string
		for _, n := range notify {
			if t, ok := n["type"].(string); ok {
				types = append(types, t)
			}
		}
		fmt.Printf("  Notifications: %s\n", strings.Join(types, ", "))
	} else {
		fmt.Println("  Notifications: (none)")
	}

	if server, ok := cfg["server"].(map[string]string); ok {
		fmt.Printf("  Server: %s (%s)\n", server["listen"], server["mode"])
	}

	if updates, ok := cfg["updates"].(map[string]interface{}); ok {
		if check, ok := updates["check"].(bool); ok && check {
			fmt.Printf("  Updates: enabled (%v)\n", updates["interval"])
		} else {
			fmt.Println("  Updates: disabled")
		}
	}

	fmt.Println()
}

func stepHeader(num int, title string) {
	fmt.Printf("\n─── Step %d: %s ───\n\n", num, title)
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptSecret(reader *bufio.Reader, label string) string {
	fmt.Printf("  %s: ", label)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func choose(reader *bufio.Reader, label string, options []string, defaultVal string) string {
	fmt.Printf("  %s\n", label)
	for i, opt := range options {
		marker := " "
		if opt == defaultVal {
			marker = "*"
		}
		fmt.Printf("    %s [%d] %s\n", marker, i+1, opt)
	}
	fmt.Printf("  Enter choice [%d]: ", indexOf(options, defaultVal)+1)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	idx, err := strconv.Atoi(input)
	if err == nil && idx >= 1 && idx <= len(options) {
		return options[idx-1]
	}
	return input
}

func confirm(reader *bufio.Reader, label string) bool {
	fmt.Printf("  %s (y/n) [n]: ", label)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
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

func indexOf(slice []string, val string) int {
	for i, s := range slice {
		if s == val {
			return i
		}
	}
	return 0
}

func init() {
	wizardCmd.Flags().String("file", "", "Path to existing TOML config file (skip interactive config)")
	RootCmd.AddCommand(wizardCmd)
}
