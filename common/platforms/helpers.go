package platforms

import "asika/common/models"

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
