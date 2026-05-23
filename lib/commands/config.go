package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"asika/common/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current config (masked)",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/config", GetServer(cmd))
		resp := doRequest("GET", url, cmd)
		if resp == nil {
			return
		}
		handleObjectResponse(resp, "No configuration loaded")
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set configuration (reads from file or stdin, sends to server)",
	Run: func(cmd *cobra.Command, args []string) {
		token := GetToken(cmd)
		var inputData []byte
		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			data, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("Error reading file: %v\n", err)
				return
			}
			inputData = data
		} else {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Printf("Error reading stdin: %v\n", err)
				return
			}
			if len(data) == 0 {
				fmt.Println("Error: no input provided. Use --file or pipe TOML content to stdin")
				return
			}
			inputData = data
		}

		var cfg map[string]interface{}
		if err := toml.Unmarshal(inputData, &cfg); err != nil {
			fmt.Printf("Error: invalid TOML format: %v\n", err)
			return
		}

		jsonData, err := json.Marshal(cfg)
		if err != nil {
			fmt.Printf("Error: failed to convert to JSON: %v\n", err)
			return
		}

		url := fmt.Sprintf("%s/api/v1/config", GetServer(cmd))
		req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			setAuthHeader(req, token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		handleWriteResponse(resp, "Config updated successfully")
	},
}

var configReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Trigger config hot reload",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("%s/api/v1/config", GetServer(cmd))
		resp := doRequest("PUT", url, cmd)
		if resp == nil {
			return
		}
		handleWriteResponse(resp, "Config reload triggered")
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		path, _ := cmd.Flags().GetString("path")
		if path == "" {
			path = "asika.toml"
		}
		cfg, warnings, err := config.ValidateFile(path)
		_ = cfg
		for _, w := range warnings {
			fmt.Printf("Warning: %s\n", w)
		}
		if err != nil {
			fmt.Printf("Config invalid: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Config is valid")
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configReloadCmd)
	configCmd.AddCommand(configValidateCmd)

	configSetCmd.Flags().String("file", "", "Path to TOML config file (if not provided, reads from stdin)")
	configValidateCmd.Flags().String("path", "asika.toml", "Path to TOML config file")

	RootCmd.AddCommand(configCmd)
}
