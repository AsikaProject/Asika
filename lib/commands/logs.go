package commands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Manage audit logs",
}

var logsExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export audit logs",
	Run: func(cmd *cobra.Command, args []string) {
		server := GetServer(cmd)
		token := GetToken(cmd)

		format, _ := cmd.Flags().GetString("format")
		level, _ := cmd.Flags().GetString("level")
		category, _ := cmd.Flags().GetString("category")
		actor, _ := cmd.Flags().GetString("actor")
		repoGroup, _ := cmd.Flags().GetString("repo-group")
		action, _ := cmd.Flags().GetString("action")
		since, _ := cmd.Flags().GetString("since")
		output, _ := cmd.Flags().GetString("output")

		// Build query parameters
		params := make([]string, 0)
		if format != "" {
			params = append(params, "format="+format)
		}
		if level != "" {
			params = append(params, "level="+level)
		}
		if category != "" {
			params = append(params, "category="+category)
		}
		if actor != "" {
			params = append(params, "actor="+actor)
		}
		if repoGroup != "" {
			params = append(params, "repo_group="+repoGroup)
		}
		if action != "" {
			params = append(params, "action="+action)
		}
		if since != "" {
			params = append(params, "since="+since)
		}

		url := fmt.Sprintf("%s/api/v1/logs/export", server)
		if len(params) > 0 {
			url += "?" + strings.Join(params, "&")
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		if token != "" {
			if isAPIKey(token) {
				req.Header.Set("X-API-Key", token)
			} else {
				setAuthHeader(req, token)
			}
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == 401 {
			fmt.Fprintln(os.Stderr, "Error: authentication failed. Use 'asika login' to authenticate.")
			return
		}
		if resp.StatusCode == 403 {
			fmt.Fprintln(os.Stderr, "Error: permission denied")
			return
		}
		if resp.StatusCode >= 400 {
			fmt.Fprintf(os.Stderr, "Error: server returned status %d\n", resp.StatusCode)
			return
		}

		// Write to file or stdout
		if output != "" {
			file, err := os.Create(output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to create file: %v\n", err)
				return
			}
			defer file.Close()
			_, err = io.Copy(file, resp.Body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to write file: %v\n", err)
				return
			}
			fmt.Printf("Logs exported to %s\n", output)
		} else {
			_, err = io.Copy(os.Stdout, resp.Body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to read response: %v\n", err)
				return
			}
		}
	},
}

var logsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List audit logs",
	Run: func(cmd *cobra.Command, args []string) {
		server := GetServer(cmd)
		token := GetToken(cmd)

		level, _ := cmd.Flags().GetString("level")
		category, _ := cmd.Flags().GetString("category")
		actor, _ := cmd.Flags().GetString("actor")
		repoGroup, _ := cmd.Flags().GetString("repo-group")
		action, _ := cmd.Flags().GetString("action")
		since, _ := cmd.Flags().GetString("since")
		limit, _ := cmd.Flags().GetInt("limit")

		// Build query parameters
		params := make([]string, 0)
		if level != "" {
			params = append(params, "level="+level)
		}
		if category != "" {
			params = append(params, "category="+category)
		}
		if actor != "" {
			params = append(params, "actor="+actor)
		}
		if repoGroup != "" {
			params = append(params, "repo_group="+repoGroup)
		}
		if action != "" {
			params = append(params, "action="+action)
		}
		if since != "" {
			params = append(params, "since="+since)
		}
		if limit > 0 {
			params = append(params, fmt.Sprintf("limit=%d", limit))
		}

		url := fmt.Sprintf("%s/api/v1/logs", server)
		if len(params) > 0 {
			url += "?" + strings.Join(params, "&")
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		if token != "" {
			if isAPIKey(token) {
				req.Header.Set("X-API-Key", token)
			} else {
				setAuthHeader(req, token)
			}
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == 401 {
			fmt.Fprintln(os.Stderr, "Error: authentication failed. Use 'asika login' to authenticate.")
			return
		}
		if resp.StatusCode == 403 {
			fmt.Fprintln(os.Stderr, "Error: permission denied")
			return
		}
		handleResponse(resp, "No logs found")
	},
}

func init() {
	// Export command flags
	logsExportCmd.Flags().String("format", "json", "Export format: json or csv")
	logsExportCmd.Flags().String("level", "", "Filter by level (info, error, warn)")
	logsExportCmd.Flags().String("category", "", "Filter by category")
	logsExportCmd.Flags().String("actor", "", "Filter by actor")
	logsExportCmd.Flags().String("repo-group", "", "Filter by repo group")
	logsExportCmd.Flags().String("action", "", "Filter by action")
	logsExportCmd.Flags().String("since", "", "Filter since duration (e.g., 24h, 7d) or RFC3339 timestamp")
	logsExportCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")

	// List command flags
	logsListCmd.Flags().String("level", "", "Filter by level (info, error, warn)")
	logsListCmd.Flags().String("category", "", "Filter by category")
	logsListCmd.Flags().String("actor", "", "Filter by actor")
	logsListCmd.Flags().String("repo-group", "", "Filter by repo group")
	logsListCmd.Flags().String("action", "", "Filter by action")
	logsListCmd.Flags().String("since", "", "Filter since duration (e.g., 24h, 7d) or RFC3339 timestamp")
	logsListCmd.Flags().Int("limit", 100, "Limit number of results")

	logsCmd.AddCommand(logsExportCmd)
	logsCmd.AddCommand(logsListCmd)
	RootCmd.AddCommand(logsCmd)
}
