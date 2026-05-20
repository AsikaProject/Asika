package init

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func ConfigurePlatforms(reader *bufio.Reader) map[string]interface{} {
	tokens := map[string]interface{}{}

	type platformInfo struct {
		name  string
		label string
		key   string
	}

	platforms := []platformInfo{
		{"github", "GitHub", "token"},
		{"gitlab", "GitLab", "token"},
		{"gitea", "Gitea", "token"},
		{"forgejo", "Forgejo", "token"},
		{"codeberg", "Codeberg", "token"},
		{"bitbucket", "Bitbucket", "token"},
	}

	var selected []int
	fmt.Println("Select platforms to configure (comma-separated numbers, or Enter to skip):")
	for i, p := range platforms {
		fmt.Printf("  [%d] %s\n", i+1, p.label)
	}
	fmt.Print("> ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		for _, part := range strings.Split(input, ",") {
			idx, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil && idx >= 1 && idx <= len(platforms) {
				selected = append(selected, idx-1)
			}
		}
	}

	for _, idx := range selected {
		p := platforms[idx]
		token := PromptSecret(reader, fmt.Sprintf("%s Token", p.name))
		tokens[p.key] = token
	}

	if Confirm(reader, "Configure Gerrit?") {
		gerritURL := Prompt(reader, "Gerrit URL", "")
		if gerritURL != "" {
			gerritUser := Prompt(reader, "Gerrit Username", "")
			gerritPass := PromptSecret(reader, "Gerrit Password")
			tokens["gerrit"] = map[string]string{
				"url":      gerritURL,
				"username": gerritUser,
				"password": gerritPass,
			}
		}
	}

	return tokens
}
