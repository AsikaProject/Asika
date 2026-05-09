package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var apikeyCmd = &cobra.Command{
	Use:   "apikey",
	Short: "Manage API keys",
	Long:  "Create, list, and revoke API keys for external integrations.",
}

var apikeyCreateCmd = &cobra.Command{
	Use:   "create <name> <role>",
	Short: "Create a new API key",
	Long: `Create a new API key with the specified name and role.

Role can be: admin, operator, viewer

Examples:
  asika apikey create ci-cd operator
  asika apikey create deployer viewer
  asika apikey create admin-key admin`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		role := args[1]
		validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
		if !validRoles[role] {
			fmt.Fprintf(os.Stderr, "Error: invalid role %q (must be admin, operator, or viewer)\n", role)
			os.Exit(1)
		}

		body := map[string]interface{}{"name": name, "role": role}
		bodyJSON, _ := json.Marshal(body)

		url := fmt.Sprintf("%s/api/v1/apikeys", GetServer(cmd))
		req, err := http.NewRequest("POST", url, newBuffer(bodyJSON))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		req.Header.Set("Content-Type", "application/json")
		setAuthHeader(req, GetToken(cmd))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode >= 400 {
			if errMsg, ok := result["error"].(string); ok {
				fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
			} else {
				fmt.Fprintf(os.Stderr, "Error: HTTP %d\n", resp.StatusCode)
			}
			os.Exit(1)
		}

		key, _ := result["key"].(string)
		id, _ := result["id"].(string)
		fmt.Printf("API key created!\n")
		fmt.Printf("  ID:   %s\n", id)
		fmt.Printf("  Name: %s\n", name)
		fmt.Printf("  Role: %s\n", role)
		fmt.Printf("  Key:  %s\n", key)
		fmt.Printf("\n⚠️  Copy the key now — it won't be shown again!\n")
		fmt.Printf("Use: export ASIKA_TOKEN=%s\n", key)
	},
}

var apikeyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all API keys",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/apikeys", GetServer(cmd))
		req, _ := http.NewRequest("GET", url, nil)
		setAuthHeader(req, GetToken(cmd))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		var keys []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&keys)
		if resp.StatusCode >= 400 {
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			if errMsg, ok := result["error"].(string); ok {
				fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
			}
			os.Exit(1)
		}

		if len(keys) == 0 {
			fmt.Println("No API keys.")
			return
		}
		fmt.Printf("%-12s %-20s %-10s %-20s %-20s\n", "ID", "NAME", "ROLE", "CREATED", "LAST USED")
		fmt.Println(strings.Repeat("-", 82))
		for _, k := range keys {
			id, _ := k["id"].(string)
			name, _ := k["name"].(string)
			role, _ := k["role"].(string)
			created, _ := k["created_at"].(string)
			lastUsed, _ := k["last_used_at"].(string)
			if len(created) > 16 {
				created = created[:16]
			}
			if lastUsed == "" || lastUsed == "0001-01-01T00:00:00Z" {
				lastUsed = "-"
			} else if len(lastUsed) > 16 {
				lastUsed = lastUsed[:16]
			}
			fmt.Printf("%-12s %-20s %-10s %-20s %-20s\n", id, name, role, created, lastUsed)
		}
	},
}

var apikeyRevokeCmd = &cobra.Command{
	Use:   "revoke <key_id>",
	Short: "Revoke an API key",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/apikeys/%s", GetServer(cmd), args[0])
		req, _ := http.NewRequest("DELETE", url, nil)
		setAuthHeader(req, GetToken(cmd))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			if errMsg, ok := result["error"].(string); ok {
				fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
			} else {
				fmt.Fprintf(os.Stderr, "Error: HTTP %d\n", resp.StatusCode)
			}
			os.Exit(1)
		}
		fmt.Println("API key revoked.")
	},
}

func newBuffer(data []byte) *bytes.Buffer {
	return bytes.NewBuffer(data)
}

func init() {
	apikeyCmd.AddCommand(apikeyCreateCmd)
	apikeyCmd.AddCommand(apikeyListCmd)
	apikeyCmd.AddCommand(apikeyRevokeCmd)
	RootCmd.AddCommand(apikeyCmd)
}
