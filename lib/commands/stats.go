package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"asika/common/utils"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show DORA metrics and stats",
	Run: func(cmd *cobra.Command, args []string) {
		period, _ := cmd.Flags().GetInt("period")

		url := fmt.Sprintf("%s/api/v1/stats?period=%d", GetServer(cmd), period)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		token := GetToken(cmd)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		var result map[string]interface{}
		if json.Unmarshal(body, &result) != nil {
			fmt.Println("Error parsing response")
			return
		}

		fmt.Println("═══════════════════════════════════════")
		fmt.Println("         DORA Metrics")
		fmt.Println("═══════════════════════════════════════")

		if v, ok := result["deployment_frequency"]; ok {
			fmt.Printf("  Deployments/Day:    %.2f\n", utils.ToFloat64(v))
		}
		if v, ok := result["lead_time_hours"]; ok {
			fmt.Printf("  Lead Time:          %s\n", formatHours(utils.ToFloat64(v)))
		}
		if v, ok := result["change_failure_rate"]; ok {
			fmt.Printf("  Failure Rate:       %.1f%%\n", utils.ToFloat64(v)*100)
		}
		if v, ok := result["mttr_hours"]; ok {
			fmt.Printf("  MTTR:               %s\n", formatHours(utils.ToFloat64(v)))
		}

		fmt.Println("───────────────────────────────────────")
		fmt.Println("         Overview")
		fmt.Println("───────────────────────────────────────")

		if v, ok := result["total_prs"]; ok {
			fmt.Printf("  Total PRs:          %v\n", v)
		}
		if v, ok := result["open_prs"]; ok {
			fmt.Printf("  Open PRs:           %v\n", v)
		}
		if v, ok := result["merged_prs"]; ok {
			fmt.Printf("  Merged PRs:         %v\n", v)
		}
		if v, ok := result["closed_prs"]; ok {
			fmt.Printf("  Closed PRs:         %v\n", v)
		}
		if v, ok := result["queue_items"]; ok {
			fmt.Printf("  Queue Items:        %v\n", v)
		}
		if v, ok := result["failed_queue_items"]; ok {
			fmt.Printf("  Failed Queue:       %v\n", v)
		}
		if v, ok := result["sync_failures"]; ok {
			fmt.Printf("  Sync Failures:     %v\n", v)
		}

		if byGroup, ok := result["prs_by_repo_group"]; ok {
			if m, ok := byGroup.(map[string]interface{}); ok && len(m) > 0 {
				fmt.Println("───────────────────────────────────────")
				fmt.Println("         PRs by Repo Group")
				fmt.Println("───────────────────────────────────────")
				for k, v := range m {
					fmt.Printf("  %-20s %v\n", k, v)
				}
			}
		}

		if byPlat, ok := result["prs_by_platform"]; ok {
			if m, ok := byPlat.(map[string]interface{}); ok && len(m) > 0 {
				fmt.Println("───────────────────────────────────────")
				fmt.Println("         PRs by Platform")
				fmt.Println("───────────────────────────────────────")
				for k, v := range m {
					fmt.Printf("  %-20s %v\n", k, v)
				}
			}
		}

		if v, ok := result["period_days"]; ok {
			fmt.Printf("\n  Period: last %v days\n", v)
		}
		fmt.Println("═══════════════════════════════════════")
	},
}

func formatHours(hours float64) string {
	if hours < 1 {
		return fmt.Sprintf("%.0f min", hours*60)
	}
	if hours < 24 {
		return fmt.Sprintf("%.1f hours", hours)
	}
	days := hours / 24
	if days < 30 {
		return fmt.Sprintf("%.1f days", days)
	}
	return fmt.Sprintf("%.0f days (%.1f months)", days, days/30)
}

func init() {
	statsCmd.Flags().Int("period", 30, "Stats period in days")
	RootCmd.AddCommand(statsCmd)
}
