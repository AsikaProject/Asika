package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var staleCmd = &cobra.Command{
	Use:   "stale",
	Short: "Manage stale PRs",
	Long:  "Check and manage stale (inactive) PRs across repo groups.",
}

var staleCheckCmd = &cobra.Command{
	Use:   "check [repo_group]",
	Short: "Run stale PR check",
	Long:  "Scan open PRs and mark/close stale ones. Use --dry-run to preview without changes.",
	Run: func(cmd *cobra.Command, args []string) {
		server := GetServer(cmd)
		token := GetToken(cmd)

		repoGroup := ""
		if len(args) > 0 {
			repoGroup = args[0]
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		url := fmt.Sprintf("%s/api/v1/stale/check", server)
		if repoGroup != "" {
			url = fmt.Sprintf("%s/api/v1/stale/check/%s", server, repoGroup)
		}
		if dryRun {
			url += "?dry_run=true"
		}

		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Server returned error: HTTP %d\n%s\n", resp.StatusCode, string(body))
			return
		}

		if dryRun {
			var actions []map[string]interface{}
			if err := json.Unmarshal(body, &actions); err != nil {
				fmt.Println(string(body))
				return
			}
			if len(actions) == 0 {
				fmt.Println("No stale PRs found.")
				return
			}
			for _, a := range actions {
				t, _ := a["type"].(string)
				pl, _ := a["platform"].(string)
				r, _ := a["repo"].(string)
				num, _ := a["pr_number"].(float64)
				title, _ := a["pr_title"].(string)
				reason, _ := a["reason"].(string)
				fmt.Printf("[%s] %s/%s #%d %s - %s\n", strings.ToUpper(t), pl, r, int(num), title, reason)
			}
		} else {
			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				fmt.Println(string(body))
				return
			}
			if msg, ok := result["message"].(string); ok {
				fmt.Println(msg)
			} else {
				b, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(b))
			}
		}
	},
}

var staleUnmarkCmd = &cobra.Command{
	Use:   "unmark [repo_group] [pr_number]",
	Short: "Remove stale label from a PR",
	Run: func(cmd *cobra.Command, args []string) {
		server := GetServer(cmd)
		token := GetToken(cmd)

		if len(args) < 2 {
			fmt.Println("Usage: asika stale unmark <repo_group> <pr_number>")
			return
		}
		repoGroup := args[0]
		prNumber := args[1]

		url := fmt.Sprintf("%s/api/v1/stale/unmark/%s/%s", server, repoGroup, prNumber)
		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Server returned error: HTTP %d\n%s\n", resp.StatusCode, string(body))
			return
		}
		fmt.Println(strings.TrimSpace(string(body)))
	},
}

func init() {
	staleCheckCmd.Flags().Bool("dry-run", false, "Preview actions without making changes")
	staleCmd.AddCommand(staleCheckCmd)
	staleCmd.AddCommand(staleUnmarkCmd)
	RootCmd.AddCommand(staleCmd)
}
