package workflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"asika/common/platforms"
)

type PRContext struct {
	CIPassed     bool
	CIFailed     bool
	HasConflicts bool
	IsApproved   bool
	IsDraft      bool
	Labels       []string
}

type Evaluator struct {
	client platforms.PlatformClient
	info   *PlatformInfo
}

func NewEvaluator(client platforms.PlatformClient, info *PlatformInfo) *Evaluator {
	return &Evaluator{
		client: client,
		info:   info,
	}
}

func (e *Evaluator) GetPRContext(ctx context.Context) (*PRContext, error) {
	pr, err := e.client.GetPR(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}

	ciStatus := "unknown"
	if pr.BranchInfo != nil && pr.BranchInfo.HeadSHA != "" {
		status, err := e.client.GetCIStatus(ctx, e.info.RepoOwner, e.info.RepoName, pr.BranchInfo.HeadSHA)
		if err == nil {
			ciStatus = status
		}
	}

	approvalStatus, err := e.client.GetApprovals(ctx, e.info.RepoOwner, e.info.RepoName, e.info.PRNumber)
	isApproved := false
	if err == nil && approvalStatus != nil {
		isApproved = len(approvalStatus.Approvers) > 0
	}

	return &PRContext{
		CIPassed:     ciStatus == "success",
		CIFailed:     ciStatus == "failure",
		HasConflicts: pr.HasConflict,
		IsApproved:   isApproved,
		IsDraft:      pr.IsDraft,
		Labels:       pr.Labels,
	}, nil
}

func (e *Evaluator) EvaluateCondition(condition string, ctx *PRContext) (bool, error) {
	return evaluateCondition(condition, ctx)
}

func evaluateCondition(condition string, ctx *PRContext) (bool, error) {
	condition = strings.TrimSpace(condition)

	labelRegex := regexp.MustCompile(`has_label\("([^"]+)"\)`)
	condition = labelRegex.ReplaceAllStringFunc(condition, func(match string) string {
		submatches := labelRegex.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return "false"
		}
		labelName := submatches[1]
		for _, l := range ctx.Labels {
			if l == labelName {
				return "true"
			}
		}
		return "false"
	})

	replacements := map[string]bool{
		"ci_passed":     ctx.CIPassed,
		"ci_failed":     ctx.CIFailed,
		"has_conflicts": ctx.HasConflicts,
		"approved":      ctx.IsApproved,
		"draft":         ctx.IsDraft,
	}

	for key, value := range replacements {
		condition = strings.ReplaceAll(condition, key, fmt.Sprintf("%t", value))
	}

	condition = strings.ReplaceAll(condition, "&&", " && ")
	condition = strings.ReplaceAll(condition, "||", " || ")
	condition = strings.ReplaceAll(condition, "!", "!")

	result, err := evaluateExpression(condition)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate condition: %w", err)
	}

	return result, nil
}

func evaluateExpression(expr string) (bool, error) {
	expr = strings.TrimSpace(expr)

	if expr == "true" {
		return true, nil
	}
	if expr == "false" {
		return false, nil
	}

	if strings.Contains(expr, "||") {
		parts := strings.Split(expr, "||")
		for _, part := range parts {
			result, err := evaluateExpression(strings.TrimSpace(part))
			if err != nil {
				return false, err
			}
			if result {
				return true, nil
			}
		}
		return false, nil
	}

	if strings.Contains(expr, "&&") {
		parts := strings.Split(expr, "&&")
		for _, part := range parts {
			result, err := evaluateExpression(strings.TrimSpace(part))
			if err != nil {
				return false, err
			}
			if !result {
				return false, nil
			}
		}
		return true, nil
	}

	if strings.HasPrefix(expr, "!") {
		result, err := evaluateExpression(strings.TrimPrefix(expr, "!"))
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		return evaluateExpression(expr[1 : len(expr)-1])
	}

	return false, fmt.Errorf("invalid expression: %s", expr)
}
