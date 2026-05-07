package platforms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/go-github/v69/github"

	"asika/common/models"
)

// GitHubClient implements PlatformClient for GitHub
type GitHubClient struct {
	client        *github.Client
	token         string
	webhookSecret string
}

// NewGitHubClient creates a new GitHub client. If baseURL is empty, uses github.com.
// For GitHub Enterprise, pass the base URL (e.g. "https://github.example.com/api/v3").
func NewGitHubClient(token, webhookSecret, baseURL string) *GitHubClient {
	httpClient := github.NewTokenClient(context.Background(), token)
	if baseURL != "" {
		ec, err := httpClient.WithEnterpriseURLs(baseURL, baseURL)
		if err == nil {
			httpClient = ec
		}
	}
	return &GitHubClient{
		client:        httpClient,
		token:         token,
		webhookSecret: webhookSecret,
	}
}

// GetPR retrieves a pull request
func (c *GitHubClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	record := &models.PRRecord{
		ID:             fmt.Sprintf("%d", pr.GetID()),
		Platform:       "github",
		PRNumber:       pr.GetNumber(),
		Title:          pr.GetTitle(),
		Author:         pr.GetUser().GetLogin(),
		State:          pr.GetState(),
		Labels:         extractLabels(pr.Labels),
		MergeCommitSHA: pr.GetMergeCommitSHA(),
		SpamFlag:       false,
		CreatedAt:      pr.GetCreatedAt().Time,
		UpdatedAt:      pr.GetUpdatedAt().Time,
		Events:         []models.PREvent{},
		HasConflict:    !pr.GetMergeable(),
		HTMLURL:        pr.GetHTMLURL(),
	}
	return record, nil
}

// ListPRs lists pull requests
func (c *GitHubClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	opts := &github.PullRequestListOptions{
		State: state,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var result []*models.PRRecord
	for {
		prs, resp, err := c.client.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list PRs: %w", err)
		}

		for _, pr := range prs {
 			record := &models.PRRecord{
 				ID:             fmt.Sprintf("%d", pr.GetID()),
 				Platform:       "github",
 				PRNumber:       pr.GetNumber(),
 				Title:          pr.GetTitle(),
 				Author:         pr.GetUser().GetLogin(),
 				State:          pr.GetState(),
 				Labels:         extractLabels(pr.Labels),
 				MergeCommitSHA: pr.GetMergeCommitSHA(),
 				SpamFlag:       false,
 				CreatedAt:      pr.GetCreatedAt().Time,
 				UpdatedAt:      pr.GetUpdatedAt().Time,
 				Events:         []models.PREvent{},
 				IsDraft:        pr.GetDraft(),
 				HTMLURL:        pr.GetHTMLURL(),
 				MergedAt:       pr.GetMergedAt().Time,
 			}
			result = append(result, record)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return result, nil
}

func extractLabels(labels []*github.Label) []string {
	result := make([]string, 0, len(labels))
	for _, l := range labels {
		result = append(result, l.GetName())
	}
	return result
}

// ApprovePR approves a pull request
func (c *GitHubClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.client.PullRequests.CreateReview(ctx, owner, repo, number, &github.PullRequestReviewRequest{
		Event: github.String("APPROVE"),
	})
	if err != nil {
		return fmt.Errorf("failed to approve PR: %w", err)
	}
	return nil
}

// MergePR merges a pull request
func (c *GitHubClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	mergeMethod := method
	if mergeMethod == "" {
		mergeMethod = "merge"
	}

	opts := &github.PullRequestOptions{
		MergeMethod: mergeMethod,
	}

	result, _, err := c.client.PullRequests.Merge(ctx, owner, repo, number, "", opts)
	if err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}
	if !result.GetMerged() {
		return fmt.Errorf("merge returned merged=false, message: %s", result.GetMessage())
	}
	return nil
}

// ClosePR closes a pull request
func (c *GitHubClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.client.PullRequests.Edit(ctx, owner, repo, number, &github.PullRequest{
		State: github.String("closed"),
	})
	if err != nil {
		return fmt.Errorf("failed to close PR: %w", err)
	}
	return nil
}

// ReopenPR reopens a pull request
func (c *GitHubClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.client.PullRequests.Edit(ctx, owner, repo, number, &github.PullRequest{
		State: github.String("open"),
	})
	if err != nil {
		return fmt.Errorf("failed to reopen PR: %w", err)
	}
	return nil
}

// CommentPR adds a comment to a pull request (via IssuesService)
func (c *GitHubClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
	_, _, err := c.client.Issues.CreateComment(ctx, owner, repo, number, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return fmt.Errorf("failed to comment on PR: %w", err)
	}
	return nil
}

// AddLabel adds a label to a pull request
func (c *GitHubClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	if color == "" {
		color = "ededed"
	}
	if err := c.CreateLabel(ctx, owner, repo, label, color, ""); err != nil {
		return err
	}
	_, _, err := c.client.Issues.AddLabelsToIssue(ctx, owner, repo, number, []string{label})
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from a pull request
func (c *GitHubClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	_, err := c.client.Issues.RemoveLabelForIssue(ctx, owner, repo, number, label)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}
	return nil
}

func (c *GitHubClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	lbl := &github.Label{
		Name:        &name,
		Color:       &color,
		Description: &description,
	}
	_, _, err := c.client.Issues.CreateLabel(ctx, owner, repo, lbl)
	if err != nil {
		if strings.Contains(err.Error(), "already_exists") {
			return nil
		}
		return fmt.Errorf("failed to create label: %w", err)
	}
	return nil
}

// GetBranch checks if a branch exists
func (c *GitHubClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	_, _, err := c.client.Repositories.GetBranch(ctx, owner, repo, branch, 0)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check branch: %w", err)
	}
	return true, nil
}

// ListBranches lists all branches in a repository
func (c *GitHubClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	opts := &github.ListOptions{PerPage: 100}
	var branches []string
	for {
		branchList, resp, err := c.client.Repositories.ListBranches(ctx, owner, repo, &github.BranchListOptions{ListOptions: *opts})
		if err != nil {
			return nil, fmt.Errorf("failed to list branches: %w", err)
		}
		for _, b := range branchList {
			branches = append(branches, b.GetName())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return branches, nil
}

// DeleteBranch deletes a branch
func (c *GitHubClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	_, err := c.client.Git.DeleteRef(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

// GetDefaultBranch gets the default branch
func (c *GitHubClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}
	return r.GetDefaultBranch(), nil
}

// GetCIStatus gets the CI status
func (c *GitHubClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
	statuses, _, err := c.client.Repositories.GetCombinedStatus(ctx, owner, repo, commitSHA, nil)
	if err != nil {
		return "none", nil
	}

	state := statuses.GetState()
	switch state {
	case "success":
		return "success", nil
	case "failure", "error":
		return "failure", nil
	case "pending":
		return "pending", nil
	default:
		return "none", nil
	}
}

// GetDefaultMergeMethod gets the default merge method
func (c *GitHubClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}

	if r.GetAllowSquashMerge() {
		return "squash", nil
	}
	if r.GetAllowMergeCommit() {
		return "merge", nil
	}
	if r.GetAllowRebaseMerge() {
		return "rebase", nil
	}
	return "merge", nil
}

// HasMultipleMergeMethods checks if multiple merge methods are available
func (c *GitHubClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	r, _, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return false, fmt.Errorf("failed to get repo: %w", err)
	}

	methods := 0
	if r.GetAllowMergeCommit() {
		methods++
	}
	if r.GetAllowSquashMerge() {
		methods++
	}
	if r.GetAllowRebaseMerge() {
		methods++
	}

	return methods > 1, nil
}

// GetApprovals gets the list of approvers
func (c *GitHubClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
	reviews, _, err := c.client.PullRequests.ListReviews(ctx, owner, repo, number, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list reviews: %w", err)
	}

	var approvers []string
	for _, review := range reviews {
		if review.GetState() == "APPROVED" {
			approvers = append(approvers, review.GetUser().GetLogin())
		}
	}
	return approvers, nil
}

// VerifyWebhookSignature verifies the webhook signature using HMAC-SHA256
func (c *GitHubClient) VerifyWebhookSignature(body []byte, signature string) bool {
	if c.webhookSecret == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	if strings.HasPrefix(signature, "sha256=") {
		signature = strings.TrimPrefix(signature, "sha256=")
	}

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}

// GetPRCommits gets the commits in a PR
func (c *GitHubClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	opts := &github.ListOptions{PerPage: 100}
	var shas []string
	for {
		commits, resp, err := c.client.PullRequests.ListCommits(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list PR commits: %w", err)
		}
		for _, cm := range commits {
			shas = append(shas, cm.GetSHA())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return shas, nil
}

// GetPRBranchInfo gets branch metadata for rebase operations
func (c *GitHubClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	info := &models.PRBranchInfo{
		BaseBranch: pr.GetBase().GetRef(),
		HeadBranch: pr.GetHead().GetRef(),
		HeadSHA:    pr.GetHead().GetSHA(),
	}
	if pr.MaintainerCanModify != nil {
		info.MaintainerCanModify = *pr.MaintainerCanModify
	}
	return info, nil
}

// GetDiffFiles gets the changed files in a PR
func (c *GitHubClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	opts := &github.ListOptions{PerPage: 100}
	var files []string
	for {
		commitFiles, resp, err := c.client.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list PR files: %w", err)
		}
		for _, f := range commitFiles {
			files = append(files, f.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return files, nil
}