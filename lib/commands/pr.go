package commands

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Manage pull requests",
}

var prListCmd = &cobra.Command{
	Use:   "list [repo_group]",
	Short: "List pull requests",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repoGroup := args[0]
		state, _ := cmd.Flags().GetString("state")
		platform, _ := cmd.Flags().GetString("platform")

		url := fmt.Sprintf("%s/api/v1/repos/%s/prs?state=%s&platform=%s",
			GetServer(cmd), repoGroup, state, platform,
		)
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		handleResponse(resp, "No PRs found")
	},
}

var prShowCmd = &cobra.Command{
	Use:   "show [repo_group] [pr_id]",
	Short: "Show pull request details",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s",
			GetServer(cmd), args[0], args[1],
		)
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		handleResponse(resp, "PR not found")
	},
}

var prApproveCmd = &cobra.Command{
	Use:   "approve [repo_group] [pr_id]",
	Short: "Approve a pull request",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/approve",
			GetServer(cmd), args[0], args[1],
		)
		resp := doRequest("POST", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "PR approved successfully")
	},
}

var prCloseCmd = &cobra.Command{
	Use:   "close [repo_group] [pr_id]",
	Short: "Close a pull request",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/close",
			GetServer(cmd), args[0], args[1],
		)
		resp := doRequest("POST", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "PR closed successfully")
	},
}

var prReopenCmd = &cobra.Command{
	Use:   "reopen [repo_group] [pr_id]",
	Short: "Reopen a pull request",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/reopen",
			GetServer(cmd), args[0], args[1],
		)
		resp := doRequest("POST", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "PR reopened successfully")
	},
}

var prSpamCmd = &cobra.Command{
	Use:   "spam [repo_group] [pr_id]",
	Short: "Mark/unmark spam",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		undo, _ := cmd.Flags().GetBool("undo")
		method := "POST"
		msg := "PR marked as spam"
		if undo {
			method = "DELETE"
			msg = "Spam mark removed"
		}

		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/spam",
			GetServer(cmd), args[0], args[1],
		)
		resp := doRequest(method, url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, msg)
	},
}

var prCommentCmd = &cobra.Command{
	Use:   "comment [repo_group] [pr_id] [body]",
	Short: "Comment on a pull request",
	Args:  cobra.MinimumNArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		body := ""
		for i := 2; i < len(args); i++ {
			if i > 2 {
				body += " "
			}
			body += args[i]
		}

		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/comment",
			GetServer(cmd), args[0], args[1],
		)

		// Create a request with JSON body
		reqBody := fmt.Sprintf(`{"body": "%s"}`, body)
		req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		token := GetToken(cmd)
		if token != "" {
			if isAPIKey(token) {
				req.Header.Set("X-API-Key", token)
			} else {
				setAuthHeader(req, token)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp.Body.Close()
		handleWriteResponse(resp, "Comment added successfully")
	},
}

func doRequest(method, url string, cmd *cobra.Command) *http.Response {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return nil
	}
	token := GetToken(cmd)
	if token != "" {
		setAuthHeader(req, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
		return nil
	}
	if resp.StatusCode == 401 {
		fmt.Fprintln(os.Stderr, "Error: authentication failed. Use 'asika login' to authenticate.")
		return nil
	}
	if resp.StatusCode == 403 {
		fmt.Fprintln(os.Stderr, "Error: 权限不够")
		return nil
	}
	return resp
}

func init() {
	prCmd.AddCommand(prListCmd)
	prCmd.AddCommand(prShowCmd)
	prCmd.AddCommand(prApproveCmd)
	prCmd.AddCommand(prCloseCmd)
	prCmd.AddCommand(prReopenCmd)
	prCmd.AddCommand(prSpamCmd)
	prCmd.AddCommand(prCommentCmd)
	prCmd.AddCommand(prBatchApproveCmd)
	prCmd.AddCommand(prBatchCloseCmd)
	prCmd.AddCommand(prBatchLabelCmd)

	prListCmd.Flags().String("state", "", "Filter by state")
	prListCmd.Flags().String("platform", "", "Filter by platform")
	prSpamCmd.Flags().Bool("undo", false, "Remove spam mark")
	prBatchLabelCmd.Flags().String("label", "", "Label to add (required)")
	prBatchLabelCmd.Flags().String("color", "", "Label color (optional)")

	RootCmd.AddCommand(prCmd)
}

var prBatchApproveCmd = &cobra.Command{
	Use:   "batch-approve [repo_group] [pr_ids]",
	Short: "Approve multiple pull requests",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		prIDs := strings.Split(args[1], ",")
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/batch/approve", GetServer(cmd), args[0])
		body := fmt.Sprintf(`{"pr_ids": ["%s"]`, strings.Join(prIDs, `","`))
		body += "}"
		resp := doBatchRequest(cmd, url, body)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Batch approve completed")
	},
}

var prBatchCloseCmd = &cobra.Command{
	Use:   "batch-close [repo_group] [pr_ids]",
	Short: "Close multiple pull requests",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		prIDs := strings.Split(args[1], ",")
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/batch/close", GetServer(cmd), args[0])
		body := fmt.Sprintf(`{"pr_ids": ["%s"]`, strings.Join(prIDs, `","`))
		body += "}"
		resp := doBatchRequest(cmd, url, body)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Batch close completed")
	},
}

var prBatchLabelCmd = &cobra.Command{
	Use:   "batch-label [repo_group] [pr_ids]",
	Short: "Add label to multiple pull requests",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		prIDs := strings.Split(args[1], ",")
		label, _ := cmd.Flags().GetString("label")
		if label == "" {
			fmt.Println("Error: --label flag is required")
			return
		}
		color, _ := cmd.Flags().GetString("color")
		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/batch/label", GetServer(cmd), args[0])
		body := fmt.Sprintf(`{"pr_ids": ["%s"], "label": "%s", "color": "%s"}`, strings.Join(prIDs, `","`), label, color)
		resp := doBatchRequest(cmd, url, body)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Batch label completed")
	},
}

func doBatchRequest(cmd *cobra.Command, url, body string) *http.Response {
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	token := GetToken(cmd)
	if token != "" {
		setAuthHeader(req, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}
	return resp
}
