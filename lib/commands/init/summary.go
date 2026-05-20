package init

import (
	"fmt"
	"strings"
)

func PrintHeader(server string) {
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     Asika Configuration Wizard       ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Printf("  Server: %s\n\n", server)
}

func PrintDisclaimer() {
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  RISK DISCLOSURE & DISCLAIMER")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  By proceeding, you acknowledge:")
	fmt.Println("  • This wizard will configure an asikad")
	fmt.Println("    server with the credentials you")
	fmt.Println("    provide (tokens, passwords, etc.).")
	fmt.Println("  • Tokens will be stored on the server")
	fmt.Println("    and used to access your repos.")
	fmt.Println("  • You are responsible for securing")
	fmt.Println("    the server and its data.")
	fmt.Println("────────────────────────────────────────")
	fmt.Println()
}

func PrintSummary(cfg map[string]interface{}) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║       Configuration Summary          ║")
	fmt.Println("╚══════════════════════════════════════╝")

	fmt.Printf("  Mode: %v\n", cfg["mode"])

	if db, ok := cfg["database"].(map[string]string); ok {
		fmt.Printf("  Database: %s (%s)\n", db["type"], db["path"])
	}

	if tokens, ok := cfg["tokens"].(map[string]interface{}); ok {
		var configured []string
		for k, v := range tokens {
			switch val := v.(type) {
			case string:
				if val != "" {
					configured = append(configured, k)
				}
			case map[string]string:
				if val["url"] != "" {
					configured = append(configured, k)
				}
			}
		}
		if len(configured) > 0 {
			fmt.Printf("  Platforms: %s\n", strings.Join(configured, ", "))
		} else {
			fmt.Println("  Platforms: (none)")
		}
	}

	if groups, ok := cfg["repo_groups"].([]map[string]interface{}); ok && len(groups) > 0 {
		g := groups[0]
		fmt.Printf("  Repo Group: %v (branch: %v)\n", g["name"], g["default_branch"])
		var repos []string
		for _, p := range []string{"github", "gitlab", "gitea", "forgejo", "codeberg", "bitbucket", "gerrit"} {
			if v, ok := g[p].(string); ok && v != "" {
				repos = append(repos, fmt.Sprintf("%s=%s", p, v))
			}
		}
		if len(repos) > 0 {
			fmt.Printf("  Repos: %s\n", strings.Join(repos, ", "))
		}
	}

	if notify, ok := cfg["notify"].([]map[string]interface{}); ok && len(notify) > 0 {
		var types []string
		for _, n := range notify {
			if t, ok := n["type"].(string); ok {
				types = append(types, t)
			}
		}
		fmt.Printf("  Notifications: %s\n", strings.Join(types, ", "))
	} else {
		fmt.Println("  Notifications: (none)")
	}

	if server, ok := cfg["server"].(map[string]string); ok {
		fmt.Printf("  Server: %s (%s)\n", server["listen"], server["mode"])
	}

	if updates, ok := cfg["updates"].(map[string]interface{}); ok {
		if check, ok := updates["check"].(bool); ok && check {
			fmt.Printf("  Updates: enabled (%v)\n", updates["interval"])
		} else {
			fmt.Println("  Updates: disabled")
		}
	}

	fmt.Println()
}
