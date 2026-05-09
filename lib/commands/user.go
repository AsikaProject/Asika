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

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
	Long:  "Create, list, update, and delete users with role-based access control.",
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	Long:  "Display all users with their roles, permissions, and repo group access.",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/users", GetServer(cmd))
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		_ = handleResponse(resp, "No users found")
	},
}

var userAddCmd = &cobra.Command{
	Use:   "add <username>",
	Short: "Add a new user",
	Long: `Create a new user with specified role and permissions.

Examples:
  asika user add alice --password secret --role operator
  asika user add bob --password secret --role viewer --groups frontend,backend
  asika user add charlie --password secret --role operator --perms approve,close,queue`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		password, _ := cmd.Flags().GetString("password")
		role, _ := cmd.Flags().GetString("role")
		groups, _ := cmd.Flags().GetStringSlice("groups")
		perms, _ := cmd.Flags().GetStringSlice("permissions")

		if password == "" {
			fmt.Fprintln(os.Stderr, "Error: --password is required")
			os.Exit(1)
		}
		if role == "" {
			role = "viewer"
		}

		body := map[string]interface{}{
			"username":            args[0],
			"password":            password,
			"role":                role,
			"allowed_repo_groups": groups,
			"permissions":         parsePermFlags(perms),
		}

		bodyJSON, err := json.Marshal(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		url := fmt.Sprintf("%s/api/v1/users", GetServer(cmd))
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyJSON))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		req.Header.Set("Content-Type", "application/json")
		if token := GetToken(cmd); token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		handleWriteResponse(resp, "User '"+args[0]+"' created successfully")
	},
}

var userUpdateCmd = &cobra.Command{
	Use:   "update <username>",
	Short: "Update an existing user",
	Long: `Modify a user's password, role, repo group access, or permissions.

Examples:
  asika user update alice --role admin
  asika user update bob --password newpass --groups docs
  asika user update charlie --perms approve,close --no-perms spam`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		password, _ := cmd.Flags().GetString("password")
		role, _ := cmd.Flags().GetString("role")
		groups, _ := cmd.Flags().GetStringSlice("groups")
		perms, _ := cmd.Flags().GetStringSlice("permissions")
		noPerms, _ := cmd.Flags().GetStringSlice("no-perms")

		body := map[string]interface{}{}

		if cmd.Flags().Changed("password") {
			body["password"] = password
		}
		if cmd.Flags().Changed("role") {
			body["role"] = role
		}
		if cmd.Flags().Changed("groups") {
			body["allowed_repo_groups"] = groups
		}

		permObj := parsePermFlags(perms)
		for _, np := range noPerms {
			permObj[np] = false
		}
		if len(perms) > 0 || len(noPerms) > 0 {
			body["permissions"] = permObj
		}

		bodyJSON, err := json.Marshal(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		url := fmt.Sprintf("%s/api/v1/users/%s", GetServer(cmd), username)
		req, err := http.NewRequest("PUT", url, bytes.NewBuffer(bodyJSON))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		req.Header.Set("Content-Type", "application/json")
		if token := GetToken(cmd); token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		handleWriteResponse(resp, "User '"+username+"' updated successfully")
	},
}

var userDeleteCmd = &cobra.Command{
	Use:   "delete <username>",
	Short: "Delete a user",
	Long:  "Permanently remove a user. This action cannot be undone.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/users/%s", GetServer(cmd), args[0])
		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if token := GetToken(cmd); token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		handleWriteResponse(resp, "User '"+args[0]+"' deleted successfully")
	},
}

func parsePermFlags(perms []string) map[string]bool {
	result := map[string]bool{
		"can_approve":      false,
		"can_merge":        false,
		"can_close":        false,
		"can_reopen":       false,
		"can_spam":         false,
		"can_manage_queue": false,
	}
	for _, p := range perms {
		switch strings.ToLower(p) {
		case "approve":
			result["can_approve"] = true
		case "merge":
			result["can_merge"] = true
		case "close":
			result["can_close"] = true
		case "reopen":
			result["can_reopen"] = true
		case "spam":
			result["can_spam"] = true
		case "queue", "manage_queue":
			result["can_manage_queue"] = true
		}
	}
	return result
}

func init() {
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userUpdateCmd)
	userCmd.AddCommand(userDeleteCmd)

	userAddCmd.Flags().StringP("password", "p", "", "User password (required)")
	userAddCmd.Flags().StringP("role", "r", "viewer", "Role: admin, operator, viewer")
	userAddCmd.Flags().StringSliceP("groups", "g", nil, "Repo groups the user can access (comma-separated)")
	userAddCmd.Flags().StringSliceP("permissions", "", nil, "Permissions: approve,merge,close,reopen,spam,queue")

	userUpdateCmd.Flags().StringP("password", "p", "", "New password (leave empty to keep unchanged)")
	userUpdateCmd.Flags().StringP("role", "r", "", "New role: admin, operator, viewer")
	userUpdateCmd.Flags().StringSliceP("groups", "g", nil, "Repo groups (replaces existing)")
	userUpdateCmd.Flags().StringSlice("permissions", nil, "Enable permissions: approve,merge,close,reopen,spam,queue")
	userUpdateCmd.Flags().StringSlice("no-perms", nil, "Disable permissions: approve,merge,close,reopen,spam,queue")

	RootCmd.AddCommand(userCmd)
}
