package platforms

import (
	"strings"

	"asika/common/models"
)

func parseDiffFiles(diff string) []string {
	files := make([]string, 0)
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git a/") {
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) == 2 {
				filePath := strings.TrimPrefix(parts[0], "diff --git a/")
				files = append(files, filePath)
			}
		}
	}
	return files
}

func parseDiffFileContents(diff string) []models.DiffFile {
	files := make([]models.DiffFile, 0)
	lines := strings.Split(diff, "\n")
	var currentFile *models.DiffFile
	var patchLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git a/") {
			if currentFile != nil {
				currentFile.Patch = strings.Join(patchLines, "\n")
				files = append(files, *currentFile)
			}
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) == 2 {
				filePath := strings.TrimPrefix(parts[0], "diff --git a/")
				currentFile = &models.DiffFile{
					Filename: filePath,
					Status:   "modified",
				}
				patchLines = make([]string, 0)
			}
		} else if currentFile != nil {
			if strings.HasPrefix(line, "new file") {
				currentFile.Status = "added"
			} else if strings.HasPrefix(line, "deleted file") {
				currentFile.Status = "removed"
			} else if strings.HasPrefix(line, "rename from") {
				currentFile.Status = "renamed"
			}
			patchLines = append(patchLines, line)
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				currentFile.Additions++
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				currentFile.Deletions++
			}
		}
	}
	if currentFile != nil {
		currentFile.Patch = strings.Join(patchLines, "\n")
		files = append(files, *currentFile)
	}
	return files
}

// NewForgejoClient creates a Forgejo client (reuses GiteaClient since Forgejo is a Gitea fork).
func NewForgejoClient(baseURL, token string, webhookSecret string) *GiteaClient {
	return NewGiteaClient(baseURL, token, webhookSecret)
}

// NewCodebergClient creates a Codeberg client (Codeberg is a hosted Forgejo instance).
// Uses https://codeberg.org as the default base URL.
func NewCodebergClient(token string, webhookSecret string) *GiteaClient {
	return NewForgejoClient("https://codeberg.org", token, webhookSecret)
}
