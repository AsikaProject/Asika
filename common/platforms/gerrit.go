package platforms

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/andygrunwald/go-gerrit"

	"asika/common/models"
)

// GerritClient implements PlatformClient for Gerrit
type GerritClient struct {
	client        *gerrit.Client
	webhookSecret string
}

// NewGerritClient creates a new Gerrit client
func NewGerritClient(url, username, password, webhookSecret string) *GerritClient {
	if url == "" || username == "" || password == "" {
		slog.Warn("gerrit client not configured, platform disabled")
		return nil
	}

	client, err := gerrit.NewClient(context.Background(), url, nil)
	if err != nil {
		slog.Warn("failed to create gerrit client", "url", url, "error", err)
		return nil
	}
	client.Authentication.SetBasicAuth(username, password)

	return &GerritClient{
		client:        client,
		webhookSecret: webhookSecret,
	}
}

func gerritChangeToRecord(change *gerrit.ChangeInfo, project string) *models.PRRecord {
	record := &models.PRRecord{
		RepoGroup: project,
		Platform:  "gerrit",
		PRNumber:  change.Number,
		Title:     change.Subject,
		Author:    gerritAccountToName(change.Owner),
		State:     gerritStatusToState(change.Status),
		IsDraft:   change.Status == "DRAFT",
	}

	if !change.Created.Time.IsZero() {
		record.CreatedAt = change.Created.Time
	}
	if !change.Updated.Time.IsZero() {
		record.UpdatedAt = change.Updated.Time
	}

	if change.URL != "" {
		record.HTMLURL = change.URL
	}

	record.HasConflict = !change.Mergeable
	record.Labels = extractGerritLabels(change)
	record.IsApproved = gerritIsApproved(change)

	if change.Revisions != nil {
		for _, rev := range change.Revisions {
			if rev.Commit.Commit != "" {
				record.BranchInfo = &models.PRBranchInfo{
					BaseBranch: change.Branch,
					HeadSHA:    rev.Commit.Commit,
				}
				break
			}
		}
	}

	return record
}

func gerritAccountToName(acct gerrit.AccountInfo) string {
	if acct.Name != "" {
		return acct.Name
	}
	if acct.Username != "" {
		return acct.Username
	}
	if acct.Email != "" {
		return acct.Email
	}
	if acct.AccountID > 0 {
		return fmt.Sprintf("%d", acct.AccountID)
	}
	return ""
}

func gerritStatusToState(status string) string {
	switch strings.ToUpper(status) {
	case "NEW", "DRAFT":
		return "open"
	case "MERGED":
		return "merged"
	case "ABANDONED":
		return "closed"
	default:
		return strings.ToLower(status)
	}
}

func extractGerritLabels(change *gerrit.ChangeInfo) []string {
	var labels []string
	if change.Labels == nil {
		return labels
	}
	for name, labelInfo := range change.Labels {
		if labelInfo.Approved.AccountID > 0 {
			labels = append(labels, name+"+2")
		} else if labelInfo.Rejected.AccountID > 0 {
			labels = append(labels, name+"-2")
		} else if labelInfo.Recommended.AccountID > 0 {
			labels = append(labels, name+"+1")
		} else if labelInfo.Disliked.AccountID > 0 {
			labels = append(labels, name+"-1")
		}
	}
	return labels
}

func gerritIsApproved(change *gerrit.ChangeInfo) bool {
	if change.Labels == nil {
		return false
	}
	if codeReview, ok := change.Labels["Code-Review"]; ok {
		return codeReview.Approved.AccountID > 0 || codeReview.Recommended.AccountID > 0
	}
	return false
}

func gerritQueryByState(state string) string {
	switch state {
	case "open":
		return "status:open"
	case "merged":
		return "status:merged"
	case "closed", "abandoned":
		return "status:abandoned"
	default:
		return "status:open"
	}
}

func (c *GerritClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	changeID := fmt.Sprintf("%s~%d", owner, number)
	opt := &gerrit.ChangeOptions{AdditionalFields: []string{"LABELS", "DETAILED_ACCOUNTS"}}
	change, _, err := c.client.Changes.GetChangeDetail(ctx, changeID, opt)
	if err != nil {
		return nil, fmt.Errorf("failed to get gerrit change: %w", err)
	}
	return gerritChangeToRecord(change, owner), nil
}

func (c *GerritClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	query := fmt.Sprintf("project:%s+%s", owner, gerritQueryByState(state))
	opt := &gerrit.QueryChangeOptions{}
	opt.Query = []string{query}
	opt.Limit = 100
	changes, _, err := c.client.Changes.QueryChanges(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("failed to query gerrit changes: %w", err)
	}
	var result []*models.PRRecord
	for i := range *changes {
		result = append(result, gerritChangeToRecord(&(*changes)[i], owner))
	}
	return result, nil
}

func (c *GerritClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	review := &gerrit.ReviewInput{
		Labels: map[string]int{
			"Code-Review": 2,
		},
	}
	_, _, err := c.client.Changes.SetReview(ctx, fmt.Sprintf("%s~%d", owner, number), "current", review)
	if err != nil {
		return fmt.Errorf("failed to approve gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	_, _, err := c.client.Changes.SubmitChange(ctx, fmt.Sprintf("%s~%d", owner, number), &gerrit.SubmitInput{})
	if err != nil {
		return fmt.Errorf("failed to submit gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.client.Changes.AbandonChange(ctx, fmt.Sprintf("%s~%d", owner, number), nil)
	if err != nil {
		return fmt.Errorf("failed to abandon gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.client.Changes.RestoreChange(ctx, fmt.Sprintf("%s~%d", owner, number), nil)
	if err != nil {
		return fmt.Errorf("failed to restore gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
	review := &gerrit.ReviewInput{
		Message: body,
	}
	_, _, err := c.client.Changes.SetReview(ctx, fmt.Sprintf("%s~%d", owner, number), "current", review)
	if err != nil {
		return fmt.Errorf("failed to comment on gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	review := &gerrit.ReviewInput{
		Labels: map[string]int{
			label: 1,
		},
	}
	_, _, err := c.client.Changes.SetReview(ctx, fmt.Sprintf("%s~%d", owner, number), "current", review)
	if err != nil {
		return fmt.Errorf("failed to add label on gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	review := &gerrit.ReviewInput{
		Labels: map[string]int{
			label: 0,
		},
	}
	_, _, err := c.client.Changes.SetReview(ctx, fmt.Sprintf("%s~%d", owner, number), "current", review)
	if err != nil {
		return fmt.Errorf("failed to remove label on gerrit change: %w", err)
	}
	return nil
}

func (c *GerritClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	slog.Warn("gerrit: CreateLabel not supported via API, must be configured in project.config")
	return nil
}

func (c *GerritClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	_, _, err := c.client.Projects.GetBranch(ctx, owner, branch)
	return err == nil, nil
}

func (c *GerritClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	opt := &gerrit.BranchOptions{}
	branches, _, err := c.client.Projects.ListBranches(ctx, owner, opt)
	if err != nil {
		return nil, fmt.Errorf("failed to list gerrit branches: %w", err)
	}
	var result []string
	for i := range *branches {
		result = append(result, (*branches)[i].Ref)
	}
	return result, nil
}

func (c *GerritClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	_, err := c.client.Projects.DeleteBranch(ctx, owner, branch)
	if err != nil {
		return fmt.Errorf("failed to delete gerrit branch: %w", err)
	}
	return nil
}

func (c *GerritClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	head, _, err := c.client.Projects.GetHEAD(ctx, repo)
	if err != nil || head == "" {
		return "master", nil
	}
	return head, nil
}

func (c *GerritClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
	query := fmt.Sprintf("project:%s+commit:%s", owner, commitSHA)
	opt := &gerrit.QueryChangeOptions{}
	opt.Query = []string{query}
	changes, _, err := c.client.Changes.QueryChanges(ctx, opt)
	if err != nil {
		return "unknown", err
	}
	if len(*changes) == 0 {
		return "unknown", nil
	}
	if verified, ok := (*changes)[0].Labels["Verified"]; ok {
		if verified.Approved.AccountID > 0 {
			return "success", nil
		}
		if verified.Rejected.AccountID > 0 {
			return "failure", nil
		}
	}
	return "unknown", nil
}

func (c *GerritClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	return "merge", nil
}

func (c *GerritClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	return false, nil
}

func (c *GerritClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
	changeID := fmt.Sprintf("%s~%d", owner, number)
	opt := &gerrit.ChangeOptions{AdditionalFields: []string{"LABELS", "DETAILED_ACCOUNTS"}}
	change, _, err := c.client.Changes.GetChangeDetail(ctx, changeID, opt)
	if err != nil {
		return nil, fmt.Errorf("failed to get gerrit change detail: %w", err)
	}
	var approvals []string
	if codeReview, ok := change.Labels["Code-Review"]; ok {
		if codeReview.Approved.AccountID > 0 {
			approvals = append(approvals, gerritAccountToName(codeReview.Approved))
		}
		if codeReview.Recommended.AccountID > 0 {
			approvals = append(approvals, gerritAccountToName(codeReview.Recommended))
		}
	}
	return approvals, nil
}

func (c *GerritClient) VerifyWebhookSignature(body []byte, signature string) bool {
	if c.webhookSecret == "" {
		return true
	}
	return signature == c.webhookSecret
}

func (c *GerritClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	changeID := fmt.Sprintf("%s~%d", owner, number)
	revision, _, err := c.client.Changes.GetCommit(ctx, changeID, "current", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get gerrit commit: %w", err)
	}
	if revision.Commit != "" {
		return []string{revision.Commit}, nil
	}
	return nil, nil
}

func (c *GerritClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	changeID := fmt.Sprintf("%s~%d", owner, number)
	files, _, err := c.client.Changes.ListFiles(ctx, changeID, "current", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list gerrit diff files: %w", err)
	}
	var result []string
	for name := range files {
		if name != "/COMMIT_MSG" {
			result = append(result, name)
		}
	}
	return result, nil
}

func (c *GerritClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	changeID := fmt.Sprintf("%s~%d", owner, number)
	change, _, err := c.client.Changes.GetChange(ctx, changeID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get gerrit change: %w", err)
	}
	info := &models.PRBranchInfo{
		BaseBranch: change.Branch,
	}
	if change.Revisions != nil {
		for _, rev := range change.Revisions {
			if rev.Commit.Commit != "" {
				info.HeadSHA = rev.Commit.Commit
				break
			}
		}
	}
	return info, nil
}

func (c *GerritClient) RequestReview(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	changeID := fmt.Sprintf("%s~%d", owner, number)
	for _, reviewer := range reviewers {
		input := &gerrit.ReviewerInput{
			Reviewer: reviewer,
		}
		_, _, err := c.client.Changes.AddReviewer(ctx, changeID, input)
		if err != nil {
			return fmt.Errorf("failed to add reviewer %s: %w", reviewer, err)
		}
	}
	return nil
}
