package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch PR status changes in real time",
	Long:  `Poll the asikad server and display live updates for PRs and queue status.`,
}

var watchPRsCmd = &cobra.Command{
	Use:   "prs [repo_group]",
	Short: "Watch PR changes for a repo group",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repoGroup := args[0]
		interval, _ := cmd.Flags().GetInt("interval")
		watchPRs(cmd, repoGroup, interval)
	},
}

var watchStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Watch DORA metrics and queue status",
	Run: func(cmd *cobra.Command, args []string) {
		interval, _ := cmd.Flags().GetInt("interval")
		watchStats(cmd, interval)
	},
}

var watchTeamCmd = &cobra.Command{
	Use:   "team",
	Short: "Watch team contributor stats",
	Run: func(cmd *cobra.Command, args []string) {
		interval, _ := cmd.Flags().GetInt("interval")
		watchTeam(cmd, interval)
	},
}

func watchPRs(cmd *cobra.Command, repoGroup string, interval int) {
	server := GetServer(cmd)
	token := GetToken(cmd)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	fmt.Printf("Watching PRs for %s (every %ds, Ctrl+C to stop)\n\n", repoGroup, interval)
	printPRsHeader()

	fetchAndPrintPRs(server, token, repoGroup)
	for range ticker.C {
		fmt.Print("\033[2J\033[H")
		fmt.Printf("Watching PRs for %s (every %ds, Ctrl+C to stop)\n\n", repoGroup, interval)
		printPRsHeader()
		fetchAndPrintPRs(server, token, repoGroup)
	}
}

func fetchAndPrintPRs(server, token, repoGroup string) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/prs", server, repoGroup)
	req, _ := http.NewRequest("GET", url, nil)
	setAuthHeader(req, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var prs []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		return
	}

	for _, pr := range prs {
		state := fmt.Sprintf("%v", pr["state"])
		color := stateColor(state)
		fmt.Printf("%s%-8s\033[0m  #%-6v  %-40s  %s\n",
			color, state, pr["pr_number"], truncateStr(fmt.Sprintf("%v", pr["title"]), 38), pr["author"])
	}
}

func watchStats(cmd *cobra.Command, interval int) {
	server := GetServer(cmd)
	token := GetToken(cmd)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	fmt.Printf("Watching stats (every %ds, Ctrl+C to stop)\n\n", interval)
	fetchAndPrintStats(server, token)
	for range ticker.C {
		fmt.Print("\033[2J\033[H")
		fmt.Printf("Watching stats (every %ds, Ctrl+C to stop)\n\n", interval)
		fetchAndPrintStats(server, token)
	}
}

func fetchAndPrintStats(server, token string) {
	url := fmt.Sprintf("%s/api/v1/stats?period=7", server)
	req, _ := http.NewRequest("GET", url, nil)
	setAuthHeader(req, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing: %v\n", err)
		return
	}

	fmt.Printf("  Deployments/Day:  %.1f\n", stats["deployment_frequency"])
	fmt.Printf("  Lead Time:        %.1f hours\n", stats["lead_time_hours"])
	fmt.Printf("  Failure Rate:     %.1f%%\n", toFloat(stats["change_failure_rate"])*100)
	fmt.Printf("  MTTR:             %.1f hours\n\n", stats["mttr_hours"])
	fmt.Printf("  Total PRs: %v | Open: %v | Merged: %v | Closed: %v\n",
		stats["total_prs"], stats["open_prs"], stats["merged_prs"], stats["closed_prs"])
	fmt.Printf("  Queue: %v | Failed: %v | Spam: %v\n",
		stats["queue_items"], stats["failed_queue_items"], stats["spam_prs"])
}

func watchTeam(cmd *cobra.Command, interval int) {
	server := GetServer(cmd)
	token := GetToken(cmd)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	fmt.Printf("Watching team stats (every %ds, Ctrl+C to stop)\n\n", interval)
	fetchAndPrintTeam(server, token)
	for range ticker.C {
		fmt.Print("\033[2J\033[H")
		fmt.Printf("Watching team stats (every %ds, Ctrl+C to stop)\n\n", interval)
		fetchAndPrintTeam(server, token)
	}
}

func fetchAndPrintTeam(server, token string) {
	url := fmt.Sprintf("%s/api/v1/stats/team?period=30", server)
	req, _ := http.NewRequest("GET", url, nil)
	setAuthHeader(req, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var ts map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing: %v\n", err)
		return
	}

	fmt.Printf("  Total Authors: %v\n\n", ts["total_authors"])
	top, _ := ts["top_contributors"].([]interface{})
	fmt.Println("  Top Contributors:")
	for i, a := range top {
		am, _ := a.(map[string]interface{})
		if am == nil {
			continue
		}
		fmt.Printf("    %d. %s — opened: %v, merged: %v, reviewed: %v\n",
			i+1, am["author"], am["prs_opened"], am["prs_merged"], am["prs_reviewed"])
	}
}

func printPRsHeader() {
	fmt.Printf("%-8s  %-8s  %-40s  %s\n", "STATE", "#", "TITLE", "AUTHOR")
	fmt.Println(strings.Repeat("-", 80))
}

func stateColor(state string) string {
	switch state {
	case "open":
		return "\033[32m"
	case "merged":
		return "\033[35m"
	case "closed":
		return "\033[31m"
	case "spam":
		return "\033[33m"
	default:
		return "\033[0m"
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}

func init() {
	watchCmd.PersistentFlags().IntP("interval", "i", 10, "Polling interval in seconds")

	watchCmd.AddCommand(watchPRsCmd)
	watchCmd.AddCommand(watchStatsCmd)
	watchCmd.AddCommand(watchTeamCmd)

	RootCmd.AddCommand(watchCmd)
}
