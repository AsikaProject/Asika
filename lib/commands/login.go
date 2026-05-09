package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to asikad and save token (JWT or API key)",
	Run: func(cmd *cobra.Command, args []string) {
		server := GetServer(cmd)
		apiKey, _ := cmd.Flags().GetString("api-key")

		// API Key mode: save directly without JWT login
		if apiKey != "" {
			save, _ := cmd.Flags().GetBool("save")
			if save {
				saveCLIConfig(cliConfig{
					Token:  apiKey,
					Server: server,
				})
				fmt.Println("API key saved. Use with --token or ASIKA_TOKEN env.")
			} else {
				fmt.Println("API key ready. Use with --token <key> or set ASIKA_TOKEN env.")
				fmt.Printf("Key: %s\n", apiKey)
			}
			return
		}

		// JWT login mode
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")
		save, _ := cmd.Flags().GetBool("save")

		if username == "" {
			fmt.Print("Username: ")
			fmt.Scanln(&username)
		}
		if password == "" {
			fmt.Print("Password: ")
			fmt.Scanln(&password)
		}

		payload, _ := json.Marshal(map[string]string{
			"username": username,
			"password": password,
		})

		url := server + "/api/v1/auth/login"
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			var e map[string]interface{}
			json.Unmarshal(body, &e)
			msg := string(body)
			if errMsg, ok := e["error"].(string); ok {
				msg = errMsg
			}
			fmt.Printf("Error: %s\n", msg)
			return
		}

		var result struct {
			Token    string `json:"token"`
			Username string `json:"username"`
			Role     string `json:"role"`
		}
		json.Unmarshal(body, &result)

		if save {
			saveCLIConfig(cliConfig{
				Token:  result.Token,
				Server: server,
			})
			fmt.Printf("Logged in as %s (%s). Config saved.\n", result.Username, result.Role)
		} else {
			fmt.Printf("Logged in as %s (%s)\n", result.Username, result.Role)
			fmt.Printf("Token: %s\n", result.Token)
			fmt.Println("Use --save to persist credentials.")
		}
	},
}

func init() {
	loginCmd.Flags().StringP("username", "u", "", "Username (prompt if empty)")
	loginCmd.Flags().StringP("password", "p", "", "Password (prompt if empty)")
	loginCmd.Flags().String("api-key", "", "Use API key directly instead of JWT login (e.g. from WebUI)")
	loginCmd.Flags().Bool("save", false, "Save credentials to ~/.config/asika/config.json")

	RootCmd.AddCommand(loginCmd)
}
