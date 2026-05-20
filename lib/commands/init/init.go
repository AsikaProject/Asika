package init

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

var WizardCmd = &cobra.Command{
	Use:   "wizard",
	Short: "Run configuration wizard and apply to server",
	Long: `Run an interactive configuration wizard that connects to an asikad server,
steps through all configuration options, writes the config file on the server,
and creates the admin user in the database.

You can also provide an existing TOML config file via --file and only enter
the admin credentials interactively.`,
	Run: func(cmd *cobra.Command, args []string) {
		reader := bufio.NewReader(os.Stdin)
		server := getServer(cmd)

		PrintHeader(server)

		if server == "http://localhost:8080" {
			fmt.Printf("  Target server: %s\n", server)
			confirm := Prompt(reader, "Is this the correct server? (y/n)", "y")
			if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
				server = Prompt(reader, "Enter server address", server)
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

		PrintDisclaimer()

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
			cfg = RunInteractiveWizard(reader)
		}

		PrintSummary(cfg)

		applyConfirm := Prompt(reader, "Apply this configuration? (y/n)", "n")
		if strings.ToLower(applyConfirm) != "y" && strings.ToLower(applyConfirm) != "yes" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}

		adminUser := Prompt(reader, "Admin username", "admin")
		adminPass := PromptSecret(reader, "Admin password")
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

func init() {
	WizardCmd.Flags().String("file", "", "Path to existing TOML config file (skip interactive config)")
}

// RegisterWizardCmd registers the wizard command on the given root command.
// Called from the commands package to avoid circular import.
func RegisterWizardCmd(root *cobra.Command) {
	root.AddCommand(WizardCmd)
}

func getServer(cmd *cobra.Command) string {
	server, _ := cmd.Flags().GetString("server")
	if server != "" && server != "http://localhost:8080" {
		return server
	}
	if s := os.Getenv("ASIKA_SERVER"); s != "" {
		return s
	}
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".config", "asika", "config.json")
	data, err := os.ReadFile(configPath)
	if err == nil {
		var cfg struct {
			Server string `json:"server"`
		}
		json.Unmarshal(data, &cfg)
		if cfg.Server != "" {
			return cfg.Server
		}
	}
	return "http://localhost:8080"
}
