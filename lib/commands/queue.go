package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Manage merge queue",
}

var queueListCmd = &cobra.Command{
	Use:   "list [repo_group]",
	Short: "List queue items",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/queue/%s",
			GetServer(cmd), args[0],
		)
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		handleResponse(resp, "No items in queue")
	},
}

var queueRecheckCmd = &cobra.Command{
	Use:   "recheck [repo_group]",
	Short: "Trigger queue recheck",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/queue/%s/recheck",
			GetServer(cmd), args[0],
		)
		resp := doRequest("POST", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Queue recheck triggered")
	},
}

var queueClearCmd = &cobra.Command{
	Use:   "clear [repo_group]",
	Short: "Clear all queue items for a repo group",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/queue/%s",
			GetServer(cmd), args[0],
		)
		resp := doRequest("DELETE", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Queue cleared")
	},
}

var queueRemoveCmd = &cobra.Command{
	Use:   "remove <repo_group> <pr_id>",
	Short: "Remove a specific PR from the queue",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/queue/%s/%s",
			GetServer(cmd), args[0], args[1],
		)
		resp := doRequest("DELETE", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Queue item removed")
	},
}

func init() {
	queueCmd.AddCommand(queueListCmd)
	queueCmd.AddCommand(queueRecheckCmd)
	queueCmd.AddCommand(queueClearCmd)
	queueCmd.AddCommand(queueRemoveCmd)
	RootCmd.AddCommand(queueCmd)
}
