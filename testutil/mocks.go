package testutil

import (
    "context"
    "fmt"

    "asika/common/models"
)

// MockPlatformClient is a mock implementation of PlatformClient
type MockPlatformClient struct {
	PRs                  map[string]*models.PRRecord
	MergeMethods         []string
	DefaultMethod        string
	Approvals            []string
	CIStatus             string
	DiffFiles            []string
	AppliedLabels        []string
	Err                  error
	MaintainerCanModify  bool
	BranchBase           string
	BranchHead           string
	BranchHeadSHA        string
}

// NewMockPlatformClient creates a new mock client
func NewMockPlatformClient() *MockPlatformClient {
    return &MockPlatformClient{
        PRs:                 make(map[string]*models.PRRecord),
        Approvals:           make([]string, 0),
        CIStatus:            "success",
        DiffFiles:           []string{"main.go", "README.md"},
        MaintainerCanModify: true,
    }
}

func (m *MockPlatformClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
    if pr, ok := m.PRs[key]; ok {
        return pr, nil
    }
    return nil, nil
}

func (m *MockPlatformClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    prs := make([]*models.PRRecord, 0, len(m.PRs))
    for _, pr := range m.PRs {
        if state == "" || pr.State == state {
            prs = append(prs, pr)
        }
    }
    return prs, nil
}

func (m *MockPlatformClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
    return m.Err
}

func (m *MockPlatformClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
    return m.Err
}

func (m *MockPlatformClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
    return m.Err
}

func (m *MockPlatformClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
    return m.Err
}

func (m *MockPlatformClient) CommentPR(ctx context.Context, owner, repo string, number int, body string) error {
    return m.Err
}

func (m *MockPlatformClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	m.AppliedLabels = append(m.AppliedLabels, label)
	return nil
}

func (m *MockPlatformClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
    return m.Err
}

func (m *MockPlatformClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
    return m.Err
}

func (m *MockPlatformClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	return true, m.Err
}

func (m *MockPlatformClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	return []string{"main", "develop"}, m.Err
}

func (m *MockPlatformClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
    return m.Err
}

func (m *MockPlatformClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
    return "main", m.Err
}

func (m *MockPlatformClient) GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error) {
    return m.CIStatus, m.Err
}

func (m *MockPlatformClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
    return m.DefaultMethod, m.Err
}

func (m *MockPlatformClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
    return len(m.MergeMethods) > 1, m.Err
}

func (m *MockPlatformClient) GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error) {
    return m.Approvals, m.Err
}

func (m *MockPlatformClient) VerifyWebhookSignature(body []byte, signature string) bool {
    return true
}

func (m *MockPlatformClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
    return []string{"abc123"}, m.Err
}

func (m *MockPlatformClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
    return m.DiffFiles, m.Err
}

func (m *MockPlatformClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    base := m.BranchBase
    if base == "" {
        base = "main"
    }
    head := m.BranchHead
    if head == "" {
        head = "feature-branch"
    }
    sha := m.BranchHeadSHA
    if sha == "" {
        sha = "abc123def456"
    }
    return &models.PRBranchInfo{
        BaseBranch:          base,
        HeadBranch:          head,
        HeadSHA:             sha,
        MaintainerCanModify: m.MaintainerCanModify,
    }, nil
}