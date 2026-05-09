package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage label rules",
}

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all label rules",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/rules/labels", GetServer(cmd))
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		defer resp.Body.Close()
		handleResponse(resp, "No label rules found")
	},
}

var rulesAddCmd = &cobra.Command{
	Use:   "add <pattern> <label>",
	Short: "Add a label rule",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		pattern := args[0]
		label := args[1]

		server := GetServer(cmd)
		token := GetToken(cmd)

		// Get existing rules
		url := fmt.Sprintf("%s/api/v1/rules/labels", server)
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		defer resp.Body.Close()

		var rules []map[string]string
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
				rules = []map[string]string{}
			}
		}

		// Add new rule
		rules = append(rules, map[string]string{
			"pattern": pattern,
			"label":   label,
		})

		// Update rules
		data, err := json.Marshal(rules)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		req, err := http.NewRequest("PUT", url, strings.NewReader(string(data)))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if token != "" {
			setAuthHeader(req, token)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp2, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp2.Body.Close()

		if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
			fmt.Println("Rule added successfully")
		} else {
			fmt.Printf("Failed to add rule: HTTP %d\n", resp2.StatusCode)
		}
	},
}

var rulesRemoveCmd = &cobra.Command{
	Use:   "remove <pattern>",
	Short: "Remove a label rule by pattern",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pattern := args[0]

		server := GetServer(cmd)

		// Get existing rules
		url := fmt.Sprintf("%s/api/v1/rules/labels", server)
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		defer resp.Body.Close()

		var rules []map[string]string
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
				fmt.Println("Error: failed to parse existing rules")
				return
			}
		}

		// Filter out matching rule
		newRules := make([]map[string]string, 0)
		for _, rule := range rules {
			if rule["pattern"] != pattern {
				newRules = append(newRules, rule)
			}
		}

		if len(newRules) == len(rules) {
			fmt.Println("Rule not found")
			return
		}

		// Update rules
		data, err := json.Marshal(newRules)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		req, err := http.NewRequest("PUT", url, strings.NewReader(string(data)))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		token := GetToken(cmd)
		if token != "" {
			setAuthHeader(req, token)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp2, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		defer resp2.Body.Close()

		if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
			fmt.Println("Rule removed successfully")
		} else {
			fmt.Printf("Failed to remove rule: HTTP %d\n", resp2.StatusCode)
		}
	},
}

func init() {
	rulesCmd.AddCommand(rulesListCmd)
	rulesCmd.AddCommand(rulesAddCmd)
	rulesCmd.AddCommand(rulesRemoveCmd)
	RootCmd.AddCommand(rulesCmd)
}
