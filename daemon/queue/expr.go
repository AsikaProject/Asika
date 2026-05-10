package queue

import (
	"fmt"
	"strconv"
	"strings"
)

// EvalContext holds the merge decision variables available to expressions.
type EvalContext struct {
	Approvals      int
	Required       int
	CIStatus       string
	HasConflict    bool
	IsDraft        bool
	CoreApproved   int
	Author         string
	CoreContributors map[string]bool
	AgeHours       float64
	Labels         map[string]bool
}

// Eval evaluates a merge condition expression against the given context.
// Supported operators: AND, OR, NOT, ==, !=, >=, <=, >, <, IN
// Supported variables: approvals, required, ci, conflict, draft, core_approved, author, age_hours, label
func Eval(expr string, ctx EvalContext) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	return evalExpr(expr, ctx)
}

func evalExpr(expr string, ctx EvalContext) (bool, error) {
	expr = strings.TrimSpace(expr)

	if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		depth := 0
		for i, c := range expr {
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
			}
			if depth == 0 && i < len(expr)-1 {
				break
			}
			if depth == 0 && i == len(expr)-1 {
				return evalExpr(expr[1:len(expr)-1], ctx)
			}
		}
	}

	orIdx := findOperator(expr, "OR")
	if orIdx >= 0 {
		left := strings.TrimSpace(expr[:orIdx])
		right := strings.TrimSpace(expr[orIdx+2:])
		l, err := evalExpr(left, ctx)
		if err != nil {
			return false, err
		}
		if l {
			return true, nil
		}
		return evalExpr(right, ctx)
	}

	andIdx := findOperator(expr, "AND")
	if andIdx >= 0 {
		left := strings.TrimSpace(expr[:andIdx])
		right := strings.TrimSpace(expr[andIdx+3:])
		l, err := evalExpr(left, ctx)
		if err != nil {
			return false, err
		}
		if !l {
			return false, nil
		}
		return evalExpr(right, ctx)
	}

	if strings.HasPrefix(expr, "NOT ") {
		inner := strings.TrimSpace(expr[4:])
		result, err := evalExpr(inner, ctx)
		return !result, err
	}

	return evalComparison(expr, ctx)
}

func findOperator(expr, op string) int {
	depth := 0
	uExpr := strings.ToUpper(expr)
	for i := 0; i <= len(expr)-len(op); i++ {
		if expr[i] == '(' {
			depth++
			continue
		}
		if expr[i] == ')' {
			depth--
			continue
		}
		if depth == 0 && strings.HasPrefix(uExpr[i:], op) {
			beforeOK := i == 0 || expr[i-1] == ' '
			after := i + len(op)
			afterOK := after >= len(expr) || expr[after] == ' '
			if beforeOK && afterOK {
				return i
			}
		}
	}
	return -1
}

func evalComparison(expr string, ctx EvalContext) (bool, error) {
	expr = strings.TrimSpace(expr)

	for _, op := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(op):])
		return compare(left, op, right, ctx)
	}

	if idx := strings.Index(strings.ToUpper(expr), " IN "); idx >= 0 {
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+4:])
		return compare(left, "IN", right, ctx)
	}

	v := resolve(expr, ctx)
	switch val := v.(type) {
	case bool:
		return val, nil
	default:
		return false, fmt.Errorf("invalid expression: %s", expr)
	}
}

func compare(left, op, right string, ctx EvalContext) (bool, error) {
	lv := resolve(left, ctx)
	rv := resolve(right, ctx)

	switch op {
	case "==":
		return fmt.Sprintf("%v", lv) == fmt.Sprintf("%v", rv), nil
	case "!=":
		return fmt.Sprintf("%v", lv) != fmt.Sprintf("%v", rv), nil
	case ">=":
		return cmpFloat(lv, rv) >= 0, nil
	case "<=":
		return cmpFloat(lv, rv) <= 0, nil
	case ">":
		return cmpFloat(lv, rv) > 0, nil
	case "<":
		return cmpFloat(lv, rv) < 0, nil
	case "IN":
		return checkIn(lv, rv, ctx), nil
	default:
		return false, fmt.Errorf("unknown operator: %s", op)
	}
}

func resolve(name string, ctx EvalContext) interface{} {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "approvals":
		return ctx.Approvals
	case "required":
		return ctx.Required
	case "ci":
		return strings.ToLower(ctx.CIStatus)
	case "conflict":
		return ctx.HasConflict
	case "draft":
		return ctx.IsDraft
	case "core_approved":
		return ctx.CoreApproved
	case "author":
		return ctx.Author
	case "age_hours":
		return ctx.AgeHours
	case "label":
		return ctx.Labels
	default:
		if strings.HasPrefix(name, "\"") && strings.HasSuffix(name, "\"") {
			return name[1 : len(name)-1]
		}
		if n, err := strconv.Atoi(name); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(name, 64); err == nil {
			return f
		}
		if strings.ToLower(name) == "true" {
			return true
		}
		if strings.ToLower(name) == "false" {
			return false
		}
		return name
	}
}

func cmpFloat(a, b interface{}) int {
	af := toFloat64(a)
	bf := toFloat64(b)
	if af < bf {
		return -1
	}
	if af > bf {
		return 1
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

func checkIn(lv, rv interface{}, ctx EvalContext) bool {
	switch rv := rv.(type) {
	case map[string]bool:
		_, ok := rv[fmt.Sprintf("%v", lv)]
		return ok
	case []string:
		for _, s := range rv {
			if fmt.Sprintf("%v", lv) == s {
				return true
			}
		}
		return false
	default:
		return false
	}
}
