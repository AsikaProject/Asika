package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"asika/common/models"
)

var prReviewCmd = &cobra.Command{
	Use:   "review [repo_group] [pr_id]",
	Short: "Interactive PR review with diff viewing and inline comments",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		repoGroup := args[0]
		prID := args[1]

		server := GetServer(cmd)
		token := GetToken(cmd)

		// Get PR details first
		prURL := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s", server, repoGroup, prID)
		prResp := doRequestWithToken("GET", prURL, token)
		if prResp == nil {
			return
		}
		defer prResp.Body.Close()

		var pr models.PRRecord
		if err := json.NewDecoder(prResp.Body).Decode(&pr); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to decode PR: %v\n", err)
			return
		}

		fmt.Printf("Reviewing PR #%d: %s\n", pr.PRNumber, pr.Title)
		fmt.Printf("Author: %s | State: %s | Platform: %s\n", pr.Author, pr.State, pr.Platform)
		fmt.Println(strings.Repeat("-", 60))

		// Get diff
		diffURL := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/diff", server, repoGroup, prID)
		diffResp := doRequestWithToken("GET", diffURL, token)
		if diffResp == nil {
			return
		}
		defer diffResp.Body.Close()

		var diffResult struct {
			Files []models.DiffFile `json:"files"`
		}
		if err := json.NewDecoder(diffResp.Body).Decode(&diffResult); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to decode diff: %v\n", err)
			return
		}

		if len(diffResult.Files) == 0 {
			fmt.Println("No files changed in this PR.")
			return
		}

		fmt.Printf("\n%d files changed:\n", len(diffResult.Files))
		for i, f := range diffResult.Files {
			fmt.Printf("  [%d] %s (+%d, -%d) %s\n", i+1, f.Filename, f.Additions, f.Deletions, f.Status)
		}
		fmt.Println()

		// Interactive review loop
		reader := bufio.NewReader(os.Stdin)
		currentFile := 0

		for {
			if currentFile >= len(diffResult.Files) {
				fmt.Println("\nAll files reviewed!")
				break
			}

			f := diffResult.Files[currentFile]
			fmt.Printf("\n[%d/%d] %s (%s)\n", currentFile+1, len(diffResult.Files), f.Filename, f.Status)
			fmt.Printf("  +%d -%d\n", f.Additions, f.Deletions)

			if f.Patch != "" {
				fmt.Println("\nDiff:")
				printColoredDiff(f.Patch)
			} else {
				fmt.Println("  (Patch content not available for this platform)")
			}

			fmt.Print("\n> n(ext) / p(rev) / c(omment) / s(kip) / q(uit) / #(file number): ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			switch {
			case input == "q" || input == "quit":
				fmt.Println("Review aborted.")
				return
			case input == "n" || input == "next" || input == "":
				currentFile++
			case input == "p" || input == "prev":
				if currentFile > 0 {
					currentFile--
				} else {
					fmt.Println("Already at first file.")
				}
			case input == "s" || input == "skip":
				currentFile++
			case input == "c" || input == "comment":
				comment, line := promptForComment(reader, f.Filename)
				if comment != "" {
					if err := postInlineComment(server, token, repoGroup, prID, f.Filename, line, comment, pr.MergeCommitSHA); err != nil {
						fmt.Fprintf(os.Stderr, "Error posting comment: %v\n", err)
					} else {
						fmt.Println("Comment added!")
					}
				}
			default:
				if num, err := strconv.Atoi(input); err == nil && num >= 1 && num <= len(diffResult.Files) {
					currentFile = num - 1
				} else {
					fmt.Println("Unknown command. Use n/p/c/s/q or file number.")
				}
			}
		}

		fmt.Println("\nReview complete!")
	},
}

func doRequestWithToken(method, url, token string) *http.Response {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return nil
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
		return nil
	}
	if resp.StatusCode == 401 {
		fmt.Fprintln(os.Stderr, "Error: authentication failed. Use 'asika login' to authenticate.")
		return nil
	}
	if resp.StatusCode == 403 {
		fmt.Fprintln(os.Stderr, "Error: permission denied")
		return nil
	}
	return resp
}

func printColoredDiff(patch string) {
	lines := strings.Split(patch, "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			fmt.Printf("\033[32m%s\033[0m\n", line) // Green
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			fmt.Printf("\033[31m%s\033[0m\n", line) // Red
		case strings.HasPrefix(line, "@@"):
			fmt.Printf("\033[36m%s\033[0m\n", line) // Cyan
		default:
			fmt.Println(line)
		}
	}
}

func promptForComment(reader *bufio.Reader, filename string) (string, int) {
	fmt.Print("Enter line number (or 0 for general comment): ")
	lineInput, _ := reader.ReadString('\n')
	lineInput = strings.TrimSpace(lineInput)
	line, err := strconv.Atoi(lineInput)
	if err != nil {
		fmt.Println("Invalid line number.")
		return "", 0
	}

	fmt.Print("Enter comment (single line): ")
	comment, _ := reader.ReadString('\n')
	comment = strings.TrimSpace(comment)

	if comment == "" {
		fmt.Println("Empty comment, skipping.")
		return "", 0
	}

	return comment, line
}

func postInlineComment(server, token, repoGroup, prID string, filePath string, line int, body, commitSHA string) error {
	url := fmt.Sprintf("%s/api/v1/repos/%s/prs/%s/comment-line", server, repoGroup, prID)

	comment := models.InlineComment{
		Body:      body,
		CommitSHA: commitSHA,
		FilePath:  filePath,
		Line:      line,
	}

	jsonBody, err := json.Marshal(comment)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		if isAPIKey(token) {
			req.Header.Set("X-API-Key", token)
		} else {
			setAuthHeader(req, token)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	return nil
}

func init() {
	prCmd.AddCommand(prReviewCmd)
}
