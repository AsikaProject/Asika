package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rebaseCmd = &cobra.Command{
	Use:   "rebase [repo_group] [pr_id]",
	Short: "Rebase a PR onto its base branch",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		repoGroup := args[0]
		prID := args[1]

		url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/rebase",
			GetServer(cmd), repoGroup, prID,
		)
		resp := doRequest("POST", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Rebase completed successfully")
	},
}

var rebaseQueueCmd = &cobra.Command{
	Use:   "rebase-queue [repo_group]",
	Short: "Rebase all conflicted PRs in a repo group's queue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repoGroup := args[0]

		url := fmt.Sprintf("%s/api/v1/queue/%s/rebase",
			GetServer(cmd), repoGroup,
		)
		resp := doRequest("POST", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Queue rebase completed")
	},
}

func init() {
	RootCmd.AddCommand(rebaseCmd)
}
