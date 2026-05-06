package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"asika/common/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Asika CLI version %s\n", version.Version)
	},
}

var versionServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Show server version",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/stats", GetServer(cmd))
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
			fmt.Printf("Error connecting to server: %v\n", err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		var result map[string]interface{}
		if json.Unmarshal(body, &result) != nil {
			fmt.Println("Error parsing server response")
			return
		}

		// Try health endpoint for version
		healthURL := fmt.Sprintf("%s/health", GetServer(cmd))
		healthReq, _ := http.NewRequest("GET", healthURL, nil)
		healthResp, err := http.DefaultClient.Do(healthReq)
		if err == nil {
			defer healthResp.Body.Close()
			healthBody, _ := io.ReadAll(healthResp.Body)
			var healthResult map[string]interface{}
			if json.Unmarshal(healthBody, &healthResult) == nil {
				fmt.Printf("Server: %s\n", GetServer(cmd))
				if v, ok := healthResult["status"]; ok {
					fmt.Printf("Status: %v\n", v)
				}
				return
			}
		}

		fmt.Printf("Server: %s\n", GetServer(cmd))
		fmt.Printf("Response: %s\n", string(body))
	},
}

func init() {
	versionCmd.AddCommand(versionServerCmd)
	RootCmd.AddCommand(versionCmd)
}
