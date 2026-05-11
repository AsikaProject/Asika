package platforms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"

	"asika/common/models"
)

// GiteaClient implements PlatformClient for Gitea/Forgejo
type GiteaClient struct {
	client        *gitea.Client
	token         string
	baseURL       string
	webhookSecret string
}

// NewGiteaClient creates a new Gitea client
func NewGiteaClient(baseURL, token string, webhookSecret string) *GiteaClient {
	client, err := gitea.NewClient(baseURL, gitea.SetToken(token))
	if err != nil || client == nil {
		slog.Warn("failed to create gitea client, platform disabled", "baseURL", baseURL, "error", err)
		return nil
	}

	return &GiteaClient{
		client:        client,
		token:         token,
		baseURL:       baseURL,
		webhookSecret: webhookSecret,
	}
}

// GetPR retrieves a pull request
func (c *GiteaClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	pr, _, err := c.client.GetPullRequest(owner, repo, int64(number))
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	return giteaPRToRecord(pr), nil
}

// ListPRs lists pull requests
func (c *GiteaClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	opts := gitea.ListPullRequestsOptions{
		State:       gitea.StateType(state),
		ListOptions: gitea.ListOptions{Page: 1, PageSize: 100},
	}

	var result []*models.PRRecord
	for {
		prs, _, err := c.client.ListRepoPullRequests(owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list PRs: %w", err)
		}

		if len(prs) == 0 {
			break
		}

		for _, pr := range prs {
			result = append(result, giteaPRToRecord(pr))
		}

		if len(prs) < 100 {
			break
		}
		opts.Page++
	}

	return result, nil
}

func giteaPRToRecord(pr *gitea.PullRequest) *models.PRRecord {
	state := "open"
	if pr.HasMerged {
		state = "merged"
	} else if pr.State == gitea.StateClosed {
		state = "closed"
	}

	var mergeCommitSHA string
	if pr.MergedCommitID != nil {
		mergeCommitSHA = *pr.MergedCommitID
	}

	var mergedAt time.Time
	if pr.Merged != nil {
		mergedAt = *pr.Merged
	}

	return &models.PRRecord{
		ID:             fmt.Sprintf("%d", pr.ID),
		Platform:       "gitea",
		PRNumber:       int(pr.Index),
		Title:          pr.Title,
		Author:         pr.Poster.UserName,
		State:          state,
		Labels:         extractGiteaLabels(pr.Labels),
		MergeCommitSHA: mergeCommitSHA,
		SpamFlag:       false,
		CreatedAt:      *pr.Created,
		UpdatedAt:      *pr.Updated,
		Events:         []models.PREvent{},
		HasConflict:    !pr.Mergeable,
		HTMLURL:        pr.HTMLURL,
		MergedAt:       mergedAt,
	}
}

func extractGiteaLabels(labels []*gitea.Label) []string {
	result := make([]string, 0, len(labels))
	for _, l := range labels {
		result = append(result, l.Name)
	}
	return result
}

// ApprovePR approves a pull request
func (c *GiteaClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.client.CreatePullReview(owner, repo, int64(number), gitea.CreatePullReviewOptions{
		State: gitea.ReviewStateApproved,
		Body:  "Approved by Asika",
	})
	if err != nil {
		return fmt.Errorf("failed to approve PR: %w", err)
	}
	return nil
}

// MergePR merges a pull request
func (c *GiteaClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	style := gitea.MergeStyleMerge
	switch strings.ToLower(method) {
	case "squash":
		style = gitea.MergeStyleSquash
	case "rebase":
		style = gitea.MergeStyleRebase
	case "rebase-merge":
		style = gitea.MergeStyleRebaseMerge
	}

	_, _, err := c.client.MergePullRequest(owner, repo, int64(number), gitea.MergePullRequestOption{
		Style: style,
	})
	if err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}
	return nil
}

// ClosePR closes a pull request
func (c *GiteaClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	stateClosed := gitea.StateClosed
	_, _, err := c.client.EditPullRequest(owner, repo, int64(number), gitea.EditPullRequestOption{
		State: &stateClosed,
	})
	if err != nil {
		return fmt.Errorf("failed to close PR: %w", err)
	}
	return nil
}

// ReopenPR reopens a pull request
func (c *GiteaClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	stateOpen := gitea.StateOpen
	_, _, err := c.client.EditPullRequest(owner, repo, int64(number), gitea.EditPullRequestOption{
		State: &stateOpen,
	})
	if err != nil {
		return fmt.Errorf("failed to reopen PR: %w", err)
	}
	return nil
}

// CommentPR adds a comment to a pull request (uses issue comment API)
func (c *GiteaClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
	_, _, err := c.client.CreateIssueComment(owner, repo, int64(number), gitea.CreateIssueCommentOption{
		Body: body,
	})
	if err != nil {
		return fmt.Errorf("failed to comment on PR: %w", err)
	}
	return nil
}

// AddLabel adds a label to a pull request
func (c *GiteaClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	if color == "" {
		color = "#ededed"
	}
	// Find or create the label first
	labels, _, err := c.client.ListRepoLabels(owner, repo, gitea.ListLabelsOptions{})
	if err != nil {
		return fmt.Errorf("failed to list labels: %w", err)
	}

	var labelID int64
	for _, l := range labels {
		if l.Name == label {
			labelID = l.ID
			break
		}
	}

	if labelID == 0 {
		// Create the label
		newLabel, _, err := c.client.CreateLabel(owner, repo, gitea.CreateLabelOption{
			Name:  label,
			Color: color,
		})
		if err != nil {
			return fmt.Errorf("failed to create label: %w", err)
		}
		labelID = newLabel.ID
	}

	_, _, err = c.client.AddIssueLabels(owner, repo, int64(number), gitea.IssueLabelsOption{
		Labels: []int64{labelID},
	})
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from a pull request
func (c *GiteaClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	labels, _, err := c.client.ListRepoLabels(owner, repo, gitea.ListLabelsOptions{})
	if err != nil {
		return fmt.Errorf("failed to list labels: %w", err)
	}

	for _, l := range labels {
		if l.Name == label {
			_, err = c.client.DeleteIssueLabel(owner, repo, int64(number), l.ID)
			if err != nil {
				return fmt.Errorf("failed to remove label: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("label not found: %s", label)
}

func (c *GiteaClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	opts := gitea.CreateLabelOption{
		Name:        name,
		Color:       "#" + color,
		Description: description,
	}
	_, _, err := c.client.CreateLabel(owner, repo, opts)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "422") {
			return nil
		}
		return fmt.Errorf("failed to create label: %w", err)
	}
	return nil
}

// GetBranch checks if a branch exists
func (c *GiteaClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	_, _, err := c.client.GetRepoBranch(owner, repo, branch)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check branch: %w", err)
	}
	return true, nil
}

// ListBranches lists all branches in a repository
func (c *GiteaClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	opts := gitea.ListRepoBranchesOptions{
		ListOptions: gitea.ListOptions{Page: 1, PageSize: 100},
	}
	var branches []string
	for {
		branchList, _, err := c.client.ListRepoBranches(owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list branches: %w", err)
		}
		if len(branchList) == 0 {
			break
		}
		for _, b := range branchList {
			branches = append(branches, b.Name)
		}
		if len(branchList) < 100 {
			break
		}
		opts.Page++
	}
	return branches, nil
}

// DeleteBranch deletes a branch
func (c *GiteaClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	_, _, err := c.client.DeleteRepoBranch(owner, repo, branch)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

// GetDefaultBranch gets the default branch
func (c *GiteaClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := c.client.GetRepo(owner, repo)
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}
	return r.DefaultBranch, nil
}

// GetCIStatus gets the CI status
func (c *GiteaClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
	statuses, _, err := c.client.GetCombinedStatus(owner, repo, commitSHA)
	if err != nil || statuses == nil {
		return "none", nil
	}

	switch statuses.State {
	case gitea.StatusSuccess:
		return "success", nil
	case gitea.StatusFailure, gitea.StatusError, gitea.StatusWarning:
		return "failure", nil
	case gitea.StatusPending:
		return "pending", nil
	default:
		return "none", nil
	}
}

// GetDefaultMergeMethod gets the default merge method
func (c *GiteaClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := c.client.GetRepo(owner, repo)
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}

	// Determine default based on what's allowed
	if r.AllowSquash {
		return "squash", nil
	}
	if r.AllowMerge {
		return "merge", nil
	}
	if r.AllowRebase {
		return "rebase", nil
	}
	return "merge", nil
}

// HasMultipleMergeMethods checks if multiple merge methods are available
func (c *GiteaClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	if c.client == nil {
		return false, fmt.Errorf("gitea client not initialized")
	}
	r, _, err := c.client.GetRepo(owner, repo)
	if err != nil {
		return false, fmt.Errorf("failed to get repo: %w", err)
	}

	methods := 0
	if r.AllowMerge {
		methods++
	}
	if r.AllowSquash {
		methods++
	}
	if r.AllowRebase {
		methods++
	}
	if r.AllowRebaseMerge {
		methods++
	}

	return methods > 1, nil
}

// GetApprovals gets the list of approvers
func (c *GiteaClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
	reviews, _, err := c.client.ListPullReviews(owner, repo, int64(number), gitea.ListPullReviewsOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list reviews: %w", err)
	}

	var approvers []string
	for _, review := range reviews {
		if review.State == gitea.ReviewStateApproved && review.Reviewer != nil {
			approvers = append(approvers, review.Reviewer.UserName)
		}
	}
	return approvers, nil
}

// VerifyWebhookSignature verifies the webhook signature using HMAC-SHA256
func (c *GiteaClient) VerifyWebhookSignature(body []byte, signature string) bool {
	if c.webhookSecret == "" {
		return false
	}

	if strings.HasPrefix(signature, "sha256=") {
		signature = strings.TrimPrefix(signature, "sha256=")
	}

	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}

// GetPRCommits gets the commits in a PR
// v0.13.2 doesn't have GetPullRequestCommits, so we get the merge/head SHA from GetPR
func (c *GiteaClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	pr, _, err := c.client.GetPullRequest(owner, repo, int64(number))
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	var shas []string
	if pr.MergedCommitID != nil {
		shas = append(shas, *pr.MergedCommitID)
	}
	if pr.Head != nil {
		shas = append(shas, pr.Head.Sha)
	}
	return shas, nil
}

// GetDiffFiles gets the changed files in a PR
// v0.13.2 doesn't have a structured diff files API, so we parse the raw diff
func (c *GiteaClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	diff, _, err := c.client.GetPullRequestDiff(owner, repo, int64(number))
	if err != nil {
		return nil, fmt.Errorf("failed to get PR diff: %w", err)
	}

	// Parse the diff to extract file names
	return parseDiffFiles(string(diff)), nil
}

// GetPRBranchInfo gets branch metadata for rebase operations
func (c *GiteaClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	pr, _, err := c.client.GetPullRequest(owner, repo, int64(number))
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	info := &models.PRBranchInfo{
		BaseBranch: pr.Base.Name,
	}
	if pr.Head != nil {
		info.HeadBranch = pr.Head.Name
		info.HeadSHA = pr.Head.Sha
	}
	// Gitea/Forgejo: maintainer_can_modify not exposed in SDK, default to true
	// The bot token has write access so it can push regardless
	info.MaintainerCanModify = true
	return info, nil
}

// parseDiffFiles extracts file names from a raw diff
func parseDiffFiles(diff string) []string {
	files := make([]string, 0)
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git a/") {
			// Format: "diff --git a/path/to/file b/path/to/file"
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) == 2 {
				filePath := strings.TrimPrefix(parts[0], "diff --git a/")
				files = append(files, filePath)
			}
		}
	}
	return files
}

// NewForgejoClient creates a Forgejo client (reuses GiteaClient since Forgejo is a Gitea fork).
func NewForgejoClient(baseURL, token string, webhookSecret string) *GiteaClient {
	return NewGiteaClient(baseURL, token, webhookSecret)
}

// NewCodebergClient creates a Codeberg client (Codeberg is a hosted Forgejo instance).
// Uses https://codeberg.org as the default base URL.
// Note: Codeberg client is created via NewForgejoClient in bootstrap to allow URL override.
func NewCodebergClient(token string, webhookSecret string) *GiteaClient {
	return NewForgejoClient("https://codeberg.org", token, webhookSecret)
}

// RequestReview requests reviewers for a PR on Gitea/Forgejo via a comment note.
func (c *GiteaClient) RequestReview(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	note := fmt.Sprintf("Review requested from: %s", strings.Join(reviewers, ", "))
	_, _, err := c.client.CreateIssueComment(owner, repo, int64(number), gitea.CreateIssueCommentOption{
		Body: note,
	})
	return err
}

// RevertPR creates a revert PR for a merged PR on Gitea/Forgejo.
func (c *GiteaClient) RevertPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	opts := gitea.CreatePullRequestOption{
		Title: fmt.Sprintf("Revert #%d", number),
		Head:  fmt.Sprintf("revert-%d", number),
		Base:  "",
		Body:  fmt.Sprintf("Revert of PR #%d", number),
	}
	revertPR, _, err := c.client.CreatePullRequest(owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create revert PR on gitea: %w", err)
	}
	return &models.PRRecord{
		Title:    revertPR.Title,
		PRNumber: int(revertPR.Index),
		State:    "open",
	}, nil
}

func (c *GiteaClient) GetPRBody(ctx context.Context, owner, repo string, number int) (string, error) {
	pr, _, err := c.client.GetPullRequest(owner, repo, int64(number))
	if err != nil {
		return "", fmt.Errorf("failed to get PR: %w", err)
	}
	return pr.Body, nil
}

func (c *GiteaClient) GetFileContent(ctx context.Context, owner, repo, path string) (string, error) {
	content, _, err := c.client.GetContents(owner, repo, "", path)
	if err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}
	if content != nil && content.Content != nil {
		return *content.Content, nil
	}
	return "", nil
}
