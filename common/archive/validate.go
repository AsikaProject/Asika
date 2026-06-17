package archive

import "strings"

// IsValidGitHubDownloadURL reports whether u points at a GitHub-hosted release
// asset that self-update is allowed to fetch. Only the two canonical CDN hosts
// are accepted; everything else is rejected to prevent an attacker-controlled
// release description or mirrored asset from redirecting the updater.
func IsValidGitHubDownloadURL(u string) bool {
	return strings.HasPrefix(u, "https://github.com/") ||
		strings.HasPrefix(u, "https://objects.githubusercontent.com/")
}
