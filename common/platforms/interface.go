package platforms

import (
    "context"

    "asika/common/models"
)

// PlatformType represents the type of platform
type PlatformType string

const (
    PlatformGitHub   PlatformType = "github"
    PlatformGitLab   PlatformType = "gitlab"
    PlatformGitea    PlatformType = "gitea"
    PlatformForgejo  PlatformType = "forgejo"
    PlatformCodeberg PlatformType = "codeberg"
)

// PlatformClient defines the unified platform operation interface
type PlatformClient interface {
    // PR operations
    GetPR(ctx context.Context, owner, repo string, number int) (*models.PRRecord, error)
    ListPRs(ctx context.Context, owner, repo string, state string) ([]*models.PRRecord, error)
    ApprovePR(ctx context.Context, owner, repo string, number int) error
    MergePR(ctx context.Context, owner, repo string, number int, method string) error
    ClosePR(ctx context.Context, owner, repo string, number int) error
    ReopenPR(ctx context.Context, owner, repo string, number int) error
    CommentPR(ctx context.Context, owner, repo string, number int, body string) error
    AddLabel(ctx context.Context, owner, repo string, number int, label string, color string) error
    RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error
    CreateLabel(ctx context.Context, owner, repo, name, color, description string) error

	// Branch operations
	GetBranch(ctx context.Context, owner, repo, branch string) (bool, error)
	ListBranches(ctx context.Context, owner, repo string) ([]string, error)
	DeleteBranch(ctx context.Context, owner, repo, branch string) error
	GetDefaultBranch(ctx context.Context, owner, repo string) (string, error)

    // CI status
    GetCIStatus(ctx context.Context, owner, repo string, commitSHA string) (string, error)

    // Merge method
    GetDefaultMergeMethod(ctx context.Context, owner, repo string) (string, error)
    HasMultipleMergeMethods(ctx context.Context, owner, repo string) (bool, error)

    // Approval status
    GetApprovals(ctx context.Context, owner, repo string, number int) ([]string, error)

    // Webhook
    VerifyWebhookSignature(body []byte, signature string) bool

    // PR commits & diff files
    GetPRCommits(ctx context.Context, owner, repo string, number int) ([]string, error)
    GetDiffFiles(ctx context.Context, owner, repo string, number int) ([]string, error)

    // PR branch info for rebase
    GetPRBranchInfo(ctx context.Context, owner, repo string, number int) (*models.PRBranchInfo, error)
}