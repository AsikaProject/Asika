package platforms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"asika/common/models"
)

// BitbucketClient implements PlatformClient for Bitbucket Cloud (api.bitbucket.org/2.0)
type BitbucketClient struct {
	httpClient    *http.Client
	token         string
	webhookSecret string
}

// NewBitbucketClient creates a new Bitbucket Cloud client
func NewBitbucketClient(token string, webhookSecret string) *BitbucketClient {
	return &BitbucketClient{
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		token:         token,
		webhookSecret: webhookSecret,
	}
}

func (c *BitbucketClient) apiURL(owner, repo string) string {
	return fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", owner, repo)
}

func (c *BitbucketClient) doRequest(ctx context.Context, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bitbucket API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// bbPullRequest represents a Bitbucket pull request response
type bbPullRequest struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	Links       bbLinks   `json:"links"`
	Author      *bbUser   `json:"author"`
	Source      *bbBranch `json:"source"`
	Destination *bbBranch `json:"destination"`
	CreatedOn   string    `json:"created_on"`
	UpdatedOn   string    `json:"updated_on"`
	Participants []bbParticipant `json:"participants"`
}

type bbLinks struct {
	HTML bbLink `json:"html"`
}

type bbLink struct {
	Href string `json:"href"`
}

type bbUser struct {
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
}

type bbBranch struct {
	Branch bbRef   `json:"branch"`
	Commit bbCommit `json:"commit"`
}

type bbRef struct {
	Name string `json:"name"`
}

type bbCommit struct {
	Hash string `json:"hash"`
}

type bbParticipant struct {
	Approved bool   `json:"approved"`
	User     *bbUser `json:"user"`
}

type bbPullRequestList struct {
	Values []bbPullRequest `json:"values"`
}

func (c *BitbucketClient) bbToRecord(pr *bbPullRequest, owner, repo string) *models.PRRecord {
	record := &models.PRRecord{
		ID:        fmt.Sprintf("%d", pr.ID),
		Platform:  "bitbucket",
		PRNumber:  pr.ID,
		Title:     pr.Title,
		State:     bbStateToPRState(pr.State),
		HTMLURL:   pr.Links.HTML.Href,
		Events:    []models.PREvent{},
	}

	if pr.Author != nil {
		record.Author = pr.Author.DisplayName
		if pr.Author.Username != "" {
			record.Author = pr.Author.Username
		}
	}

	if pr.Source != nil {
		record.BranchInfo = &models.PRBranchInfo{
			MaintainerCanModify: true,
		}
		if pr.Source.Branch.Name != "" {
			record.BranchInfo.HeadBranch = pr.Source.Branch.Name
		}
		if pr.Source.Commit.Hash != "" {
			record.BranchInfo.HeadSHA = pr.Source.Commit.Hash
		}
		if pr.Destination != nil && pr.Destination.Branch.Name != "" {
			record.BranchInfo.BaseBranch = pr.Destination.Branch.Name
		}
	}

	if pr.CreatedOn != "" {
		if t, err := time.Parse(time.RFC3339, pr.CreatedOn); err == nil {
			record.CreatedAt = t
		}
	}
	if pr.UpdatedOn != "" {
		if t, err := time.Parse(time.RFC3339, pr.UpdatedOn); err == nil {
			record.UpdatedAt = t
		}
	}

	return record
}

func bbStateToPRState(state string) string {
	switch strings.ToUpper(state) {
	case "OPEN":
		return "open"
	case "MERGED":
		return "merged"
	case "DECLINED", "SUPERSEDED":
		return "closed"
	default:
		return "open"
	}
}

// GetPR retrieves a pull request
func (c *BitbucketClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	url := fmt.Sprintf("%s/pullrequests/%d", c.apiURL(owner, repo), number)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	var pr bbPullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PR: %w", err)
	}
	return c.bbToRecord(&pr, owner, repo), nil
}

// ListPRs lists pull requests
func (c *BitbucketClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	url := fmt.Sprintf("%s/pullrequests", c.apiURL(owner, repo))
	if state != "" {
		stateParam := strings.ToLower(state)
		if stateParam == "closed" {
			stateParam = "declined"
		}
		url += "?state=" + stateParam
	}

	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}

	var list bbPullRequestList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse PR list: %w", err)
	}

	var result []*models.PRRecord
	for i := range list.Values {
		result = append(result, c.bbToRecord(&list.Values[i], owner, repo))
	}
	return result, nil
}

// ApprovePR approves a pull request
func (c *BitbucketClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	url := fmt.Sprintf("%s/pullrequests/%d/approve", c.apiURL(owner, repo), number)
	_, err := c.doRequest(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to approve PR: %w", err)
	}
	return nil
}

// MergePR merges a pull request
func (c *BitbucketClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	url := fmt.Sprintf("%s/pullrequests/%d/merge", c.apiURL(owner, repo), number)
	body := `{}`
	if method != "" {
		body = fmt.Sprintf(`{"merge_strategy": "%s"}`, method)
	}
	_, err := c.doRequest(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}
	return nil
}

// ClosePR declines a pull request
func (c *BitbucketClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	url := fmt.Sprintf("%s/pullrequests/%d/decline", c.apiURL(owner, repo), number)
	_, err := c.doRequest(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to decline PR: %w", err)
	}
	return nil
}

// ReopenPR reopens a pull request (Bitbucket doesn't support reopen via API)
func (c *BitbucketClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	return fmt.Errorf("bitbucket does not support reopening PRs via API")
}

// CommentPR adds a comment to a pull request
func (c *BitbucketClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
	url := fmt.Sprintf("%s/pullrequests/%d/comments", c.apiURL(owner, repo), number)
	payload := fmt.Sprintf(`{"content": {"raw": %q}}`, body)
	_, err := c.doRequest(ctx, "POST", url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to comment on PR: %w", err)
	}
	return nil
}

// AddLabel — Bitbucket doesn't support labels on PRs
func (c *BitbucketClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	return fmt.Errorf("bitbucket does not support labels on PRs")
}

// RemoveLabel — Bitbucket doesn't support labels on PRs
func (c *BitbucketClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	return fmt.Errorf("bitbucket does not support labels on PRs")
}

// CreateLabel — no-op for Bitbucket
func (c *BitbucketClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	return nil
}

// GetBranch checks if a branch exists
func (c *BitbucketClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	url := fmt.Sprintf("%s/refs/branches/%s", c.apiURL(owner, repo), branch)
	_, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check branch: %w", err)
	}
	return true, nil
}

// ListBranches lists all branches
func (c *BitbucketClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	url := fmt.Sprintf("%s/refs/branches", c.apiURL(owner, repo))
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var result struct {
		Values []struct {
			Name string `json:"name"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse branches: %w", err)
	}

	var branches []string
	for _, b := range result.Values {
		branches = append(branches, b.Name)
	}
	return branches, nil
}

// DeleteBranch deletes a branch
func (c *BitbucketClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	url := fmt.Sprintf("%s/refs/branches/%s", c.apiURL(owner, repo), branch)
	_, err := c.doRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

// GetDefaultBranch gets the default branch
func (c *BitbucketClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	url := c.apiURL(owner, repo)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}

	var result struct {
		Mainbranch *struct {
			Name string `json:"name"`
		} `json:"mainbranch"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("failed to parse repo: %w", err)
	}
	if result.Mainbranch != nil && result.Mainbranch.Name != "" {
		return result.Mainbranch.Name, nil
	}
	return "main", nil
}

// GetCIStatus gets CI status from Bitbucket Pipelines
func (c *BitbucketClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
	url := fmt.Sprintf("%s/commit/%s/statuses", c.apiURL(owner, repo), commitSHA)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return "none", nil
	}

	var result struct {
		Values []struct {
			State string `json:"state"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "none", nil
	}

	for _, s := range result.Values {
		switch strings.ToUpper(s.State) {
		case "SUCCESSFUL", "COMPLETED":
			return "success", nil
		case "FAILED", "ERROR":
			return "failure", nil
		case "IN_PROGRESS", "PENDING", "RUNNING":
			return "pending", nil
		}
	}
	return "none", nil
}

// GetDefaultMergeMethod returns the default merge method
func (c *BitbucketClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	return "merge", nil
}

// HasMultipleMergeMethods checks if multiple merge methods are available
func (c *BitbucketClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	return false, nil
}

// GetApprovals gets the list of approvers
func (c *BitbucketClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
	url := fmt.Sprintf("%s/pullrequests/%d", c.apiURL(owner, repo), number)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	var pr bbPullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PR: %w", err)
	}

	var approvers []string
	for _, p := range pr.Participants {
		if p.Approved && p.User != nil {
			approvers = append(approvers, p.User.DisplayName)
		}
	}
	return approvers, nil
}

// VerifyWebhookSignature verifies the webhook signature using HMAC-SHA256
// Bitbucket Cloud sends X-Hub-Signature header: sha256=<hex_hmac>
func (c *BitbucketClient) VerifyWebhookSignature(body []byte, signature string) bool {
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
func (c *BitbucketClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	url := fmt.Sprintf("%s/pullrequests/%d/commits", c.apiURL(owner, repo), number)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list PR commits: %w", err)
	}

	var result struct {
		Values []struct {
			Hash string `json:"hash"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse commits: %w", err)
	}

	var shas []string
	for _, c := range result.Values {
		shas = append(shas, c.Hash)
	}
	return shas, nil
}

// GetDiffFiles gets the changed files in a PR
func (c *BitbucketClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	url := fmt.Sprintf("%s/pullrequests/%d/diff", c.apiURL(owner, repo), number)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR diff: %w", err)
	}
	return parseDiffFiles(string(data)), nil
}

// GetPRBranchInfo gets branch metadata for rebase operations
func (c *BitbucketClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	url := fmt.Sprintf("%s/pullrequests/%d", c.apiURL(owner, repo), number)
	data, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	var pr bbPullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PR: %w", err)
	}

	info := &models.PRBranchInfo{
		MaintainerCanModify: true,
	}
	if pr.Source != nil {
		info.HeadBranch = pr.Source.Branch.Name
		info.HeadSHA = pr.Source.Commit.Hash
	}
	if pr.Destination != nil {
		info.BaseBranch = pr.Destination.Branch.Name
	}
	return info, nil
}

// RequestReview adds a reviewer to a Bitbucket PR.
func (c *BitbucketClient) RequestReview(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	for _, reviewer := range reviewers {
		payload := map[string]interface{}{
			"uuid": reviewer,
		}
		body, _ := json.Marshal(payload)
		path := fmt.Sprintf("/2.0/repositories/%s/%s/pullrequests/%d/request-changes", owner, repo, number)
		_, err := c.doRequest(ctx, http.MethodPost, path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to request review from %s: %w", reviewer, err)
		}
	}
	return nil
}
