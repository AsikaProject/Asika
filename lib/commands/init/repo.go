package init

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func ConfigureRepoGroup(reader *bufio.Reader, mode string) []map[string]interface{} {
	groupName := Prompt(reader, "Group name", "default")
	defaultBranch := Prompt(reader, "Default branch", "main")

	type platformRepo struct {
		name  string
		label string
	}

	repoPlatforms := []platformRepo{
		{"github", "GitHub"},
		{"gitlab", "GitLab"},
		{"gitea", "Gitea"},
		{"forgejo", "Forgejo"},
		{"codeberg", "Codeberg"},
		{"bitbucket", "Bitbucket"},
		{"gerrit", "Gerrit"},
	}

	repos := map[string]string{}
	fmt.Println("Select platforms to configure repositories for (comma-separated numbers, or Enter to skip):")
	for i, p := range repoPlatforms {
		fmt.Printf("  [%d] %s\n", i+1, p.label)
	}
	fmt.Print("> ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var selected []int
	if input != "" {
		for _, part := range strings.Split(input, ",") {
			idx, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil && idx >= 1 && idx <= len(repoPlatforms) {
				selected = append(selected, idx-1)
			}
		}
	}

	for _, idx := range selected {
		p := repoPlatforms[idx]
		repo := Prompt(reader, fmt.Sprintf("%s Repository (owner/repo)", p.label), "")
		if repo != "" {
			repos[p.name] = repo
		}
	}

	mirrorPlatform := ""
	if mode == "single" {
		mirrorPlatform = Choose(reader, "Mirror platform", []string{"github", "gitlab", "gitea", "forgejo", "codeberg", "bitbucket", "gerrit"}, "github")
	}

	return []map[string]interface{}{
		{
			"name":            groupName,
			"mode":            mode,
			"mirror_platform": mirrorPlatform,
			"github":          repos["github"],
			"gitlab":          repos["gitlab"],
			"gitea":           repos["gitea"],
			"forgejo":         repos["forgejo"],
			"codeberg":        repos["codeberg"],
			"bitbucket":       repos["bitbucket"],
			"gerrit":          repos["gerrit"],
			"default_branch":  defaultBranch,
			"merge_queue": map[string]interface{}{
				"required_approvals": 1,
				"ci_check_required":  true,
				"core_contributors":  []string{},
			},
		},
	}
}
