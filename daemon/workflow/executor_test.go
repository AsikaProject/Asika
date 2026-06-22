package workflow

import (
	"context"
	"testing"

	"asika/common/models"
)

type mockPlatformClient struct {
	addedLabels   []string
	removedLabels []string
	merged        bool
	closed        bool
	mergeMethod   string
	commented     bool
	branchDeleted bool
}

func (m *mockPlatformClient) AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error {
	m.addedLabels = append(m.addedLabels, label)
	return nil
}

func (m *mockPlatformClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	m.removedLabels = append(m.removedLabels, label)
	return nil
}

func (m *mockPlatformClient) MergePR(ctx context.Context, owner, repo string, number int, method string) error {
	m.merged = true
	m.mergeMethod = method
	return nil
}

func (m *mockPlatformClient) ClosePR(ctx context.Context, owner, repo string, number int) error {
	m.closed = true
	return nil
}

func (m *mockPlatformClient) CommentPR(ctx context.Context, owner, repo string, number int, comment string) error {
	m.commented = true
	return nil
}

func (m *mockPlatformClient) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	m.branchDeleted = true
	return nil
}

func (m *mockPlatformClient) GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	return nil, nil
}

func (m *mockPlatformClient) ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error) {
	return nil, nil
}

func (m *mockPlatformClient) ApprovePR(ctx context.Context, owner, repo string, number int) error {
	return nil
}

func (m *mockPlatformClient) ReopenPR(ctx context.Context, owner, repo string, number int) error {
	return nil
}

func (m *mockPlatformClient) GetCIStatus(ctx context.Context, owner, repo, commitSHA string) (string, error) {
	return "", nil
}

func (m *mockPlatformClient) GetApprovals(ctx context.Context, owner, repo string, number int) (*models.ApprovalStatus, error) {
	return nil, nil
}

func (m *mockPlatformClient) GetBranch(ctx context.Context, owner, repo, branch string) (bool, error) {
	return false, nil
}

func (m *mockPlatformClient) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	return nil, nil
}

func (m *mockPlatformClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	return "", nil
}

func (m *mockPlatformClient) GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error) {
	return "", nil
}

func (m *mockPlatformClient) HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error) {
	return false, nil
}

func (m *mockPlatformClient) GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	return nil, nil
}

func (m *mockPlatformClient) GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	return nil, nil
}

func (m *mockPlatformClient) GetPRDiff(ctx context.Context, owner, repo string, number int) ([]models.DiffFile, error) {
	return nil, nil
}

func (m *mockPlatformClient) CommentPRLine(ctx context.Context, owner, repo string, number int, comment models.InlineComment) error {
	return nil
}

func (m *mockPlatformClient) HasSecurityAlerts(ctx context.Context, owner, repo string, number int) ([]models.SecurityAlert, error) {
	return nil, nil
}

func (m *mockPlatformClient) ListPRComments(ctx context.Context, owner, repo string, number int) ([]models.PRComment, error) {
	return nil, nil
}

func (m *mockPlatformClient) GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error) {
	return nil, nil
}

func (m *mockPlatformClient) RequestReview(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	return nil
}

func (m *mockPlatformClient) RevertPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error) {
	return nil, nil
}

func (m *mockPlatformClient) GetPRBody(ctx context.Context, owner, repo string, number int) (string, error) {
	return "", nil
}

func (m *mockPlatformClient) GetFileContent(ctx context.Context, owner, repo, path string) (string, error) {
	return "", nil
}

func (m *mockPlatformClient) VerifyWebhookSignature(payload []byte, signature string) bool {
	return false
}

func (m *mockPlatformClient) CreateLabel(ctx context.Context, owner, repo, name, color, description string) error {
	return nil
}

func (m *mockPlatformClient) HasWritePermission(ctx context.Context, owner, repo, username string) (bool, error) {
	return true, nil
}

func TestExecuteLabels(t *testing.T) {
	mock := &mockPlatformClient{}
	info := &PlatformInfo{
		Platform:  "github",
		PRNumber:  123,
		RepoOwner: "owner",
		RepoName:  "repo",
	}

	executor := NewExecutor(mock, info)

	rules := []models.WorkflowLabelRule{
		{Condition: "ci_passed", Action: "add", Label: "ready"},
		{Condition: "ci_failed", Action: "add", Label: "failed"},
		{Condition: "draft", Action: "remove", Label: "review-needed"},
	}

	ctx := &PRContext{
		CIPassed: true,
		CIFailed: false,
		IsDraft:  false,
	}

	if err := executor.ExecuteLabels(context.Background(), rules, ctx); err != nil {
		t.Fatalf("ExecuteLabels failed: %v", err)
	}

	if len(mock.addedLabels) != 1 || mock.addedLabels[0] != "ready" {
		t.Errorf("expected added labels [ready], got %v", mock.addedLabels)
	}

	if len(mock.removedLabels) != 0 {
		t.Errorf("expected no removed labels, got %v", mock.removedLabels)
	}
}

func TestExecuteMerge(t *testing.T) {
	mock := &mockPlatformClient{}
	info := &PlatformInfo{
		Platform:   "github",
		PRNumber:   123,
		RepoOwner:  "owner",
		RepoName:   "repo",
		HeadBranch: "feature",
	}

	executor := NewExecutor(mock, info)

	rule := models.WorkflowMergeRule{
		Enabled:      true,
		Condition:    "ci_passed && approved",
		AutoMerge:    true,
		MergeMethod:  "squash",
		DeleteBranch: true,
	}

	ctx := &PRContext{
		CIPassed:   true,
		IsApproved: true,
	}

	if err := executor.ExecuteMerge(context.Background(), rule, ctx); err != nil {
		t.Fatalf("ExecuteMerge failed: %v", err)
	}

	if !mock.merged {
		t.Error("expected PR to be merged")
	}

	if mock.mergeMethod != "squash" {
		t.Errorf("expected merge method squash, got %s", mock.mergeMethod)
	}

	if !mock.branchDeleted {
		t.Error("expected branch to be deleted")
	}
}
