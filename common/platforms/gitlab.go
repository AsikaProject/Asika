package platforms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gitlab.com/gitlab-org/api/client-go"

	"asika/common/models"
)

// GitLabClient implements PlatformClient for GitLab
type GitLabClient struct {
	client        *gitlab.Client
	token         string
	baseURL       string
	webhookSecret string
}

// NewGitLabClient creates a new GitLab client
func NewGitLabClient(token string, baseURL string, webhookSecret string) *GitLabClient {
	var client *gitlab.Client
	var err error

	if baseURL != "" {
		client, err = gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	} else {
		client, err = gitlab.NewClient(token)
	}

	if err != nil {
		client, _ = gitlab.NewClient(token)
	}

	return &GitLabClient{
		client:        client,
		token:         token,
		baseURL:       baseURL,
		webhookSecret: webhookSecret,
	}
}

// strPtr returns a pointer to the given string
func strPtr(s string) *string { return &s }

// boolPtr returns a pointer to the given bool
func boolPtr(b bool) *bool { return &b }

// GetPR retrieves a merge request
func (c *GitLabClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	project := owner + "/" + repo
	mr, _, err := c.client.MergeRequests.GetMergeRequest(project, int64(number), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get MR: %w", err)
	}

	return gitLabMRToRecord(mr), nil
}

// ListPRs lists merge requests
func (c *GitLabClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	project := owner + "/" + repo

	opts := &gitlab.ListProjectMergeRequestsOptions{
		State: strPtr(state),
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}

	var result []*models.PRRecord
	for {
		mrs, resp, err := c.client.MergeRequests.ListProjectMergeRequests(project, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list MRs: %w", err)
		}

		for _, mr := range mrs {
			result = append(result, gitLabBasicMRToRecord(mr))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return result, nil
}

func gitLabMRToRecord(mr *gitlab.MergeRequest) *models.PRRecord {
	labels := make([]string, 0)
	for _, l := range mr.Labels {
		labels = append(labels, l)
	}

	var author string
	if mr.Author != nil {
		author = mr.Author.Username
	}

	var createdAt, updatedAt time.Time
	if mr.CreatedAt != nil {
		createdAt = *mr.CreatedAt
	}
	if mr.UpdatedAt != nil {
		updatedAt = *mr.UpdatedAt
	}

	var mergedAt time.Time
 	if mr.MergedAt != nil {
 		mergedAt = *mr.MergedAt
 	}

 	return &models.PRRecord{
 		ID:             fmt.Sprintf("%d", mr.ID),
 		Platform:       "gitlab",
 		PRNumber:       int(mr.IID),
 		Title:          mr.Title,
 		Author:         author,
 		State:          gitLabState(mr.State),
 		Labels:         labels,
 		MergeCommitSHA: mr.MergeCommitSHA,
 		SpamFlag:       false,
 		CreatedAt:      createdAt,
 		UpdatedAt:      updatedAt,
 		Events:         []models.PREvent{},
 		IsDraft:        mr.WorkInProgress,
 		HasConflict:    gitLabHasConflict(mr),
 		HTMLURL:        mr.WebURL,
 		MergedAt:       mergedAt,
 	}
}

func gitLabBasicMRToRecord(mr *gitlab.BasicMergeRequest) *models.PRRecord {
	labels := make([]string, 0)
	for _, l := range mr.Labels {
		labels = append(labels, l)
	}

	var author string
	if mr.Author != nil {
		author = mr.Author.Username
	}

	var createdAt, updatedAt time.Time
	if mr.CreatedAt != nil {
		createdAt = *mr.CreatedAt
	}
	if mr.UpdatedAt != nil {
		updatedAt = *mr.UpdatedAt
	}

	return &models.PRRecord{
		ID:             fmt.Sprintf("%d", mr.ID),
		Platform:       "gitlab",
		PRNumber:       int(mr.IID),
		Title:          mr.Title,
		Author:         author,
		State:          gitLabState(mr.State),
		Labels:         labels,
		MergeCommitSHA: mr.MergeCommitSHA,
		SpamFlag:       false,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Events:         []models.PREvent{},
		HTMLURL:        mr.WebURL,
	}
}

func gitLabHasConflict(mr *gitlab.MergeRequest) bool {
	return mr.HasConflicts
}

func gitLabState(state string) string {
	switch strings.ToLower(state) {
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	case "opened":
		return "open"
	default:
		return state
	}
}

// ApprovePR approves a merge request
func (c *GitLabClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	project := owner + "/" + repo
	_, _, err := c.client.MergeRequestApprovals.ApproveMergeRequest(project, int64(number), &gitlab.ApproveMergeRequestOptions{})
	if err != nil {
		return fmt.Errorf("failed to approve MR: %w", err)
	}
	return nil
}

// MergePR merges a merge request
func (c *GitLabClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	project := owner + "/" + repo

	opts := &gitlab.AcceptMergeRequestOptions{}

	switch strings.ToLower(method) {
	case "squash":
		opts.Squash = boolPtr(true)
	}

	_, _, err := c.client.MergeRequests.AcceptMergeRequest(project, int64(number), opts)
	if err != nil {
		return fmt.Errorf("failed to merge MR: %w", err)
	}
	return nil
}

// ClosePR closes a merge request
func (c *GitLabClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	project := owner + "/" + repo
	_, _, err := c.client.MergeRequests.UpdateMergeRequest(project, int64(number), &gitlab.UpdateMergeRequestOptions{
		StateEvent: strPtr("close"),
	})
	if err != nil {
		return fmt.Errorf("failed to close MR: %w", err)
	}
	return nil
}

// ReopenPR reopens a merge request
func (c *GitLabClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	project := owner + "/" + repo
	_, _, err := c.client.MergeRequests.UpdateMergeRequest(project, int64(number), &gitlab.UpdateMergeRequestOptions{
		StateEvent: strPtr("reopen"),
	})
	if err != nil {
		return fmt.Errorf("failed to reopen MR: %w", err)
	}
	return nil
}

// CommentPR adds a comment to a merge request
func (c *GitLabClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
	project := owner + "/" + repo
	_, _, err := c.client.Notes.CreateMergeRequestNote(project, int64(number), &gitlab.CreateMergeRequestNoteOptions{
		Body: strPtr(body),
	})
	if err != nil {
		return fmt.Errorf("failed to comment on MR: %w", err)
	}
	return nil
}

// AddLabel adds a label to a merge request
func (c *GitLabClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	project := owner + "/" + repo
	if color == "" {
		color = "#ededed"
	}
	if err := c.CreateLabel(ctx, owner, repo, label, color, ""); err != nil {
		return err
	}
	mr, _, err := c.client.MergeRequests.GetMergeRequest(project, int64(number), nil)
	if err != nil {
		return fmt.Errorf("failed to get MR: %w", err)
	}

	labels := gitlab.LabelOptions(append(mr.Labels, label))
	_, _, err = c.client.MergeRequests.UpdateMergeRequest(project, int64(number), &gitlab.UpdateMergeRequestOptions{
		Labels: &labels,
	})
	if err != nil {
		return fmt.Errorf("failed to add label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from a merge request
func (c *GitLabClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	project := owner + "/" + repo
	mr, _, err := c.client.MergeRequests.GetMergeRequest(project, int64(number), nil)
	if err != nil {
		return fmt.Errorf("failed to get MR: %w", err)
	}

	var labels []string
	for _, l := range mr.Labels {
		if l != label {
			labels = append(labels, l)
		}
	}

	labelOpts := gitlab.LabelOptions(labels)
	_, _, err = c.client.MergeRequests.UpdateMergeRequest(project, int64(number), &gitlab.UpdateMergeRequestOptions{
		Labels: &labelOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}
	return nil
}

func (c *GitLabClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	project := owner + "/" + repo
	opts := &gitlab.CreateLabelOptions{
		Name:        &name,
		Color:       &color,
		Description: &description,
	}
	_, _, err := c.client.Labels.CreateLabel(project, opts)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "409") {
			return nil
		}
		return fmt.Errorf("failed to create label: %w", err)
	}
	return nil
}

// GetBranch checks if a branch exists
func (c *GitLabClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	project := owner + "/" + repo
	_, _, err := c.client.Branches.GetBranch(project, branch)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check branch: %w", err)
	}
	return true, nil
}

// ListBranches lists all branches in a project
func (c *GitLabClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	project := owner + "/" + repo
	opts := &gitlab.ListBranchesOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}
	var branches []string
	for {
		branchList, resp, err := c.client.Branches.ListBranches(project, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list branches: %w", err)
		}
		for _, b := range branchList {
			branches = append(branches, b.Name)
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return branches, nil
}

// DeleteBranch deletes a branch
func (c *GitLabClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	project := owner + "/" + repo
	_, err := c.client.Branches.DeleteBranch(project, branch)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

// GetDefaultBranch gets the default branch
func (c *GitLabClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	project := owner + "/" + repo
	p, _, err := c.client.Projects.GetProject(project, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get project: %w", err)
	}
	return p.DefaultBranch, nil
}

// GetCIStatus gets the CI status
func (c *GitLabClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
	project := owner + "/" + repo
	pipelines, _, err := c.client.Pipelines.ListProjectPipelines(project, &gitlab.ListProjectPipelinesOptions{
		SHA: strPtr(commitSHA),
	})
	if err != nil || len(pipelines) == 0 {
		return "none", nil
	}

	switch pipelines[0].Status {
	case "success":
		return "success", nil
	case "failed", "canceled":
		return "failure", nil
	case "running", "pending":
		return "pending", nil
	default:
		return "none", nil
	}
}

// GetDefaultMergeMethod gets the default merge method
func (c *GitLabClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	project := owner + "/" + repo
	p, _, err := c.client.Projects.GetProject(project, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get project: %w", err)
	}

	switch p.MergeMethod {
	case "squash":
		return "squash", nil
	case "rebase_merge":
		return "rebase", nil
	default:
		return "merge", nil
	}
}

// HasMultipleMergeMethods checks if multiple merge methods are available
func (c *GitLabClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	// GitLab typically allows only one configured method per project
	return false, nil
}

// GetApprovals gets the list of approvers
func (c *GitLabClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
	project := owner + "/" + repo
	approvals, _, err := c.client.MergeRequests.GetMergeRequestApprovals(project, int64(number))
	if err != nil {
		return nil, fmt.Errorf("failed to get approvals: %w", err)
	}

	var approvers []string
	for _, approver := range approvals.ApprovedBy {
		if approver.User != nil {
			approvers = append(approvers, approver.User.Username)
		}
	}
	return approvers, nil
}

// VerifyWebhookSignature verifies the webhook signature using HMAC-SHA256
// GitLab uses X-Hub-Signature-256 header for system/service integration hooks,
// and X-GitLab-Token for basic integration hooks.
func (c *GitLabClient) VerifyWebhookSignature(body []byte, signature string) bool {
	if c.webhookSecret == "" {
		return false
	}

	// Try token-based verification first (X-GitLab-Token)
	if signature == c.webhookSecret {
		return true
	}

	// Try HMAC-SHA256 verification (X-Hub-Signature-256)
	if strings.HasPrefix(signature, "sha256=") {
		signature = strings.TrimPrefix(signature, "sha256=")
	}

	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}

// GetPRCommits gets the commits in a MR
func (c *GitLabClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	project := owner + "/" + repo
	commits, _, err := c.client.MergeRequests.GetMergeRequestCommits(project, int64(number), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get MR commits: %w", err)
	}

	var shas []string
	for _, commit := range commits {
		shas = append(shas, commit.ID)
	}
	return shas, nil
}

// GetPRBranchInfo gets branch metadata for rebase operations
func (c *GitLabClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	project := owner + "/" + repo
	mr, _, err := c.client.MergeRequests.GetMergeRequest(project, int64(number), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get MR: %w", err)
	}

	info := &models.PRBranchInfo{
		BaseBranch: mr.TargetBranch,
		HeadBranch: mr.SourceBranch,
		HeadSHA:    mr.SHA,
	}
	// GitLab: AllowMaintainerToPush indicates maintainer_can_modify equivalent
	info.MaintainerCanModify = mr.AllowMaintainerToPush
	return info, nil
}

// GetDiffFiles gets the changed files in a MR via ListMergeRequestDiffs
func (c *GitLabClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	project := owner + "/" + repo
	diffs, _, err := c.client.MergeRequests.ListMergeRequestDiffs(project, int64(number), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get MR diffs: %w", err)
	}

	result := make([]string, 0)
	for _, d := range diffs {
		if d.NewPath != "" {
			result = append(result, d.NewPath)
		}
		if d.OldPath != "" && d.OldPath != d.NewPath {
			result = append(result, d.OldPath)
		}
	}
	return result, nil
}