package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cmdinit "asika/lib/commands/init"
)

// RootCmd is the root command
var RootCmd = &cobra.Command{
	Use:   "asika",
	Short: "Asika PR Manager CLI",
	Long:  `Asika is a PR management tool with multi-platform support.`,
}

type cliConfig struct {
	Token  string `json:"token"`
	Server string `json:"server"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "asika", "config.json")
}

func loadCLIConfig() cliConfig {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cliConfig{}
	}
	var cfg cliConfig
	json.Unmarshal(data, &cfg)
	return cfg
}

func saveCLIConfig(cfg cliConfig) {
	dir := filepath.Dir(configPath())
	os.MkdirAll(dir, 0700)
	data, _ := json.Marshal(cfg)
	os.WriteFile(configPath(), data, 0600)
}

// GetToken returns token from flag, env, or saved file
func GetToken(cmd *cobra.Command) string {
	token, _ := cmd.Flags().GetString("token")
	if token != "" {
		return token
	}
	token = os.Getenv("ASIKA_TOKEN")
	if token != "" {
		return token
	}
	return loadCLIConfig().Token
}

// isAPIKey returns true if the token looks like an API key (starts with "ak_")
func isAPIKey(token string) bool {
	return strings.HasPrefix(token, "ak_")
}

// setAuthHeader sets the appropriate auth header on a request.
// API keys use X-API-Key; JWT tokens use Authorization: Bearer.
func setAuthHeader(req *http.Request, token string) {
	if isAPIKey(token) {
		req.Header.Set("X-API-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// GetServer returns server address from flag, env, or saved config
func GetServer(cmd *cobra.Command) string {
	server, _ := cmd.Flags().GetString("server")
	if server != "" && server != "http://localhost:8080" {
		return server
	}
	if s := os.Getenv("ASIKA_SERVER"); s != "" {
		return s
	}
	if cfg := loadCLIConfig(); cfg.Server != "" {
		return cfg.Server
	}
	return "http://localhost:8080"
}

func init() {
	RootCmd.PersistentFlags().StringP("token", "t", "", "JWT token (or use ASIKA_TOKEN env)")
	RootCmd.PersistentFlags().StringP("server", "s", "http://localhost:8080", "asikad server address")
	RootCmd.PersistentFlags().StringP("output", "o", "table", "Output format: table, json, yaml")
	cmdinit.RegisterWizardCmd(RootCmd)
}

// handleResponse reads the HTTP response and returns the parsed body as []interface{}
// On error or empty data, prints a friendly message and returns nil
func handleResponse(resp *http.Response, emptyMsg string) []interface{} {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data []interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		var obj map[string]interface{}
		if err2 := json.Unmarshal(body, &obj); err2 == nil {
			if msg, ok := obj["error"].(string); ok {
				fmt.Fprintln(os.Stderr, msg)
				return nil
			}
			if msg, ok := obj["message"].(string); ok {
				fmt.Println(msg)
				return nil
			}
			if d, ok := obj["data"].([]interface{}); ok {
				data = d
				if len(data) == 0 {
					fmt.Println(emptyMsg)
					return nil
				}
				if total, ok := obj["total"].(float64); ok {
					if page, ok := obj["page"].(float64); ok {
						if perPage, ok := obj["per_page"].(float64); ok {
							totalPages := (int(total) + int(perPage) - 1) / int(perPage)
							fmt.Fprintf(os.Stderr, "--- Page %d of %d (%d items total) ---\n", int(page), totalPages, int(total))
						}
					}
				}
			} else {
				b, _ := json.MarshalIndent(obj, "", "  ")
				fmt.Println(string(b))
				return []interface{}{obj}
			}
		}
		if len(data) == 0 {
			fmt.Println(emptyMsg)
			return nil
		}
	} else if len(data) == 0 {
		fmt.Println(emptyMsg)
		return nil
	}

	for _, item := range data {
		b, _ := json.MarshalIndent(item, "", "  ")
		fmt.Println(string(b))
	}
	return data
}

// handleWriteResponse handles responses for write commands (approve/close/etc)
func handleWriteResponse(resp *http.Response, successMsg string) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var obj map[string]interface{}
	if json.Unmarshal(body, &obj) != nil {
		fmt.Println(successMsg)
		return
	}

	if msg, ok := obj["message"].(string); ok {
		fmt.Println(msg)
		return
	}
	if err, ok := obj["error"].(string); ok && err != "" {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	fmt.Println(successMsg)
}

// handleObjectResponse handles responses for single-object endpoints (config show, etc)
func handleObjectResponse(resp *http.Response, emptyMsg string) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var obj map[string]interface{}
	if json.Unmarshal(body, &obj) != nil {
		fmt.Println(emptyMsg)
		return
	}

	if err, ok := obj["error"].(string); ok && err != "" {
		fmt.Println("Error:", err)
		return
	}

	b, _ := json.MarshalIndent(obj, "", "  ")
	fmt.Println(string(b))
}
