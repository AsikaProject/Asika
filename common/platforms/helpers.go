package platforms

import (
	"net/url"
	"strings"

	"asika/common/models"
)

// GroupPlatforms returns the list of configured platforms for a repo group.
func GroupPlatforms(group *models.RepoGroup) []PlatformType {
	var result []PlatformType
	if group.GitHub != "" {
		result = append(result, PlatformGitHub)
	}
	if group.GitLab != "" {
		result = append(result, PlatformGitLab)
	}
	if group.Gitea != "" {
		result = append(result, PlatformGitea)
	}
	if group.Forgejo != "" {
		result = append(result, PlatformForgejo)
	}
	if group.Codeberg != "" {
		result = append(result, PlatformCodeberg)
	}
	if group.Bitbucket != "" {
		result = append(result, PlatformBitbucket)
	}
	if group.Gerrit != "" {
		result = append(result, PlatformGerrit)
	}
	return result
}

// PathEscapeSegments percent-encodes the reserved characters inside each path
// segment of a URL path while preserving the literal "/". It is equivalent to
// running url.PathEscape on every segment and rejoining with "/".
//
// Used to embed user-supplied file paths (branch names, file trees) into
// platform REST URLs without letting "?", "#", or spaces break the URL.
func PathEscapeSegments(path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
