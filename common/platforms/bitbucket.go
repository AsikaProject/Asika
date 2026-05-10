package platforms

import (
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

	bitbucket "github.com/ktrysmt/go-bitbucket"

	"asika/common/models"
)

// BitbucketClient implements PlatformClient for Bitbucket Cloud (api.bitbucket.org/2.0)
type BitbucketClient struct {
	client        *bitbucket.Client
	token         string
	webhookSecret string
}

// NewBitbucketClient creates a new Bitbucket Cloud client
func NewBitbucketClient(token string, webhookSecret string) *BitbucketClient {
	c, err := bitbucket.NewAPITokenAuth("", token)
	if err != nil {
		return &BitbucketClient{webhookSecret: webhookSecret}
	}
	return &BitbucketClient{
		client:        c,
		token:         token,
		webhookSecret: webhookSecret,
	}
}

func prOptions(owner, repo, id string) *bitbucket.PullRequestsOptions {
	return &bitbucket.PullRequestsOptions{
		Owner:    owner,
		RepoSlug: repo,
		ID:       id,
	}
}

func (c *BitbucketClient) prToRecord(pr interface{}, owner, repo string) *models.PRRecord {
	m, ok := pr.(map[string]interface{})
	if !ok {
		return &models.PRRecord{Platform: "bitbucket"}
	}

	record := &models.PRRecord{
		Platform: "bitbucket",
		Events:   []models.PREvent{},
	}

	if id, ok := m["id"].(float64); ok {
		record.ID = fmt.Sprintf("%d", int(id))
		record.PRNumber = int(id)
	}
	if title, ok := m["title"].(string); ok {
		record.Title = title
	}
	if state, ok := m["state"].(string); ok {
		record.State = bbStateToPRState(state)
	}
	if links, ok := m["links"].(map[string]interface{}); ok {
		if html, ok := links["html"].(map[string]interface{}); ok {
			if href, ok := html["href"].(string); ok {
				record.HTMLURL = href
			}
		}
	}
	if author, ok := m["author"].(map[string]interface{}); ok {
		if dn, ok := author["display_name"].(string); ok {
			record.Author = dn
		}
		if username, ok := author["username"].(string); ok && username != "" {
			record.Author = username
		}
	}
	if src, ok := m["source"].(map[string]interface{}); ok {
		record.BranchInfo = &models.PRBranchInfo{MaintainerCanModify: true}
		if branch, ok := src["branch"].(map[string]interface{}); ok {
			if name, ok := branch["name"].(string); ok {
				record.BranchInfo.HeadBranch = name
			}
		}
		if commit, ok := src["commit"].(map[string]interface{}); ok {
			if hash, ok := commit["hash"].(string); ok {
				record.BranchInfo.HeadSHA = hash
			}
		}
		if dst, ok := m["destination"].(map[string]interface{}); ok {
			if branch, ok := dst["branch"].(map[string]interface{}); ok {
				if name, ok := branch["name"].(string); ok {
					record.BranchInfo.BaseBranch = name
				}
			}
		}
	}
	if createdOn, ok := m["created_on"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdOn); err == nil {
			record.CreatedAt = t
		}
	}
	if updatedOn, ok := m["updated_on"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedOn); err == nil {
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

func (c *BitbucketClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	result, err := c.client.Repositories.PullRequests.Get(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}
	return c.prToRecord(result, owner, repo), nil
}

func (c *BitbucketClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	opts := &bitbucket.PullRequestsOptions{
		Owner:    owner,
		RepoSlug: repo,
	}
	if state != "" {
		opts.States = []string{strings.ToUpper(state)}
	}
	result, err := c.client.Repositories.PullRequests.List(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}
	return c.prListToRecords(result, owner, repo), nil
}

func (c *BitbucketClient) prListToRecords(result interface{}, owner, repo string) []*models.PRRecord {
	m, ok := result.(map[string]interface{})
	if !ok {
		return nil
	}
	values, ok := m["values"].([]interface{})
	if !ok {
		return nil
	}
	var records []*models.PRRecord
	for _, v := range values {
		records = append(records, c.prToRecord(v, owner, repo))
	}
	return records
}

func (c *BitbucketClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	_, err := c.client.Repositories.PullRequests.Approve(opts)
	if err != nil {
		return fmt.Errorf("failed to approve PR: %w", err)
	}
	return nil
}

func (c *BitbucketClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	if method != "" {
		opts.MergeStrategy = bitbucket.PullRequestsMergeStrategy(method)
	}
	_, err := c.client.Repositories.PullRequests.Merge(opts)
	if err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}
	return nil
}

func (c *BitbucketClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	_, err := c.client.Repositories.PullRequests.Decline(opts)
	if err != nil {
		return fmt.Errorf("failed to decline PR: %w", err)
	}
	return nil
}

func (c *BitbucketClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	return fmt.Errorf("bitbucket does not support reopening PRs via API")
}

func (c *BitbucketClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
	opts := &bitbucket.PullRequestCommentOptions{
		Owner:         owner,
		RepoSlug:      repo,
		PullRequestID: fmt.Sprintf("%d", number),
		Content:       body,
	}
	_, err := c.client.Repositories.PullRequests.AddComment(opts)
	if err != nil {
		return fmt.Errorf("failed to comment on PR: %w", err)
	}
	return nil
}

func (c *BitbucketClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	return fmt.Errorf("bitbucket does not support labels on PRs")
}

func (c *BitbucketClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	return fmt.Errorf("bitbucket does not support labels on PRs")
}

func (c *BitbucketClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	return nil
}

func (c *BitbucketClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	opts := &bitbucket.RepositoryBranchOptions{
		Owner:      owner,
		RepoSlug:   repo,
		BranchName: branch,
	}
	_, err := c.client.Repositories.Repository.GetBranch(opts)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check branch: %w", err)
	}
	return true, nil
}

func (c *BitbucketClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	opts := &bitbucket.RepositoryBranchOptions{
		Owner:    owner,
		RepoSlug: repo,
		Pagelen:  100,
	}
	result, err := c.client.Repositories.Repository.ListBranches(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}
	var branches []string
	for _, b := range result.Branches {
		branches = append(branches, b.Name)
	}
	return branches, nil
}

func (c *BitbucketClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	opts := &bitbucket.RepositoryBranchDeleteOptions{
		Owner:    owner,
		RepoSlug: repo,
		RefName:  branch,
	}
	err := c.client.Repositories.Repository.DeleteBranch(opts)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

func (c *BitbucketClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	opts := &bitbucket.RepositoryOptions{
		Owner:    owner,
		RepoSlug: repo,
	}
	result, err := c.client.Repositories.Repository.Get(opts)
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}
	if result.Mainbranch.Name != "" {
		return result.Mainbranch.Name, nil
	}
	return "main", nil
}

func (c *BitbucketClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
	opts := &bitbucket.PipelinesOptions{
		Owner:    owner,
		RepoSlug: repo,
		Sort:     "-created_on",
	}
	result, err := c.client.Repositories.Pipelines.List(opts)
	if err != nil {
		return "none", nil
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		return "none", nil
	}
	values, ok := m["values"].([]interface{})
	if !ok || len(values) == 0 {
		return "none", nil
	}
	for _, v := range values {
		pipeline, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		target, ok := pipeline["target"].(map[string]interface{})
		if !ok {
			continue
		}
		if refType, ok := target["type"].(string); ok && refType == "commit" {
			commit, ok := target["commit"].(map[string]interface{})
			if !ok {
				continue
			}
			if hash, ok := commit["hash"].(string); ok && hash == commitSHA {
				if state, ok := pipeline["state"].(map[string]interface{}); ok {
					if name, ok := state["name"].(string); ok {
						switch strings.ToUpper(name) {
						case "COMPLETED":
							if result, ok := state["result"].(map[string]interface{}); ok {
								if resultName, ok := result["name"].(string); ok {
									switch strings.ToUpper(resultName) {
									case "SUCCESSFUL":
										return "success", nil
									case "FAILED":
										return "failure", nil
									}
								}
							}
							return "success", nil
						case "IN_PROGRESS", "PENDING":
							return "pending", nil
						}
					}
				}
			}
		}
	}
	return "none", nil
}

func (c *BitbucketClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	return "merge", nil
}

func (c *BitbucketClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	return false, nil
}

func (c *BitbucketClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	result, err := c.client.Repositories.PullRequests.Get(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	participants, ok := m["participants"].([]interface{})
	if !ok {
		return nil, nil
	}
	var approvers []string
	for _, p := range participants {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if approved, ok := pm["approved"].(bool); ok && approved {
			if user, ok := pm["user"].(map[string]interface{}); ok {
				if dn, ok := user["display_name"].(string); ok {
					approvers = append(approvers, dn)
				}
			}
		}
	}
	return approvers, nil
}

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

func (c *BitbucketClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	result, err := c.client.Repositories.PullRequests.GetCommits(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list PR commits: %w", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	values, ok := m["values"].([]interface{})
	if !ok {
		return nil, nil
	}
	var shas []string
	for _, v := range values {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if hash, ok := vm["hash"].(string); ok {
			shas = append(shas, hash)
		}
	}
	return shas, nil
}

func (c *BitbucketClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	opts := &bitbucket.DiffStatOptions{
		Owner:             owner,
		RepoSlug:          repo,
		Spec:              fmt.Sprintf("..%d", number),
		FromPullRequestID: number,
	}
	result, err := c.client.Repositories.Diff.GetDiffStat(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR diff: %w", err)
	}
	var files []string
	for _, ds := range result.DiffStats {
		if old, ok := ds.Old["path"].(string); ok {
			files = append(files, old)
		}
		if nw, ok := ds.New["path"].(string); ok && nw != "" {
			files = append(files, nw)
		}
	}
	return files, nil
}

func (c *BitbucketClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
	result, err := c.client.Repositories.PullRequests.Get(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected PR response type")
	}
	info := &models.PRBranchInfo{MaintainerCanModify: true}
	if src, ok := m["source"].(map[string]interface{}); ok {
		if branch, ok := src["branch"].(map[string]interface{}); ok {
			if name, ok := branch["name"].(string); ok {
				info.HeadBranch = name
			}
		}
		if commit, ok := src["commit"].(map[string]interface{}); ok {
			if hash, ok := commit["hash"].(string); ok {
				info.HeadSHA = hash
			}
		}
	}
	if dst, ok := m["destination"].(map[string]interface{}); ok {
		if branch, ok := dst["branch"].(map[string]interface{}); ok {
			if name, ok := branch["name"].(string); ok {
				info.BaseBranch = name
			}
		}
	}
	return info, nil
}

func (c *BitbucketClient) RequestReview(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	for _, reviewer := range reviewers {
		opts := prOptions(owner, repo, fmt.Sprintf("%d", number))
		opts.Reviewers = []string{reviewer}
		_, err := c.client.Repositories.PullRequests.RequestChanges(opts)
		if err != nil {
			return fmt.Errorf("failed to request review from %s: %w", reviewer, err)
		}
	}
	return nil
}

// RevertPR creates a revert PR for a merged PR on Bitbucket.
func (c *BitbucketClient) RevertPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	endpoint := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s/pullrequests/%d/revert", owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create revert request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send revert request to bitbucket: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bitbucket revert failed (status %d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Type  string `json:"type"`
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
		Title string `json:"title"`
		ID    int    `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bitbucket revert response: %w", err)
	}
	return &models.PRRecord{
		Title:    result.Title,
		PRNumber: result.ID,
		State:    "open",
	}, nil
}
