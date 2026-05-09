package commands

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var cherryPickCmd = &cobra.Command{
	Use:   "cherry-pick [repo_group] [pr_id] [target_branch]",
	Short: "Cherry-pick a merged PR's commit onto a target branch",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		repoGroup := args[0]
		prID := args[1]
		targetBranch := args[2]

		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/cherry-pick",
			GetServer(cmd), repoGroup, prID,
		)

		body := fmt.Sprintf(`{"target_branch": "%s"}`, targetBranch)
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		token := GetToken(cmd)
		if token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp.Body.Close()

		handleWriteResponse(resp, "Cherry-pick completed successfully")
	},
}

func init() {
	RootCmd.AddCommand(cherryPickCmd)
}
