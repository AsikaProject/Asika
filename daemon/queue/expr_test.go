package queue

import (
	"testing"
)

func TestEval_Empty(t *testing.T) {
	ctx := EvalContext{}
	result, err := Eval("", ctx)
	if err != nil {
		t.Fatalf("empty expression should return true, got error: %v", err)
	}
	if !result {
		t.Fatal("empty expression should return true")
	}
}

func TestEval_ApprovalsGte(t *testing.T) {
	ctx := EvalContext{Approvals: 3, Required: 2}
	result, err := Eval("approvals >= 2", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for approvals >= 2")
	}
}

func TestEval_ApprovalsLt(t *testing.T) {
	ctx := EvalContext{Approvals: 1, Required: 2}
	result, err := Eval("approvals >= 2", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for approvals < 2")
	}
}

func TestEval_CIStatus(t *testing.T) {
	ctx := EvalContext{CIStatus: "success"}
	result, err := Eval("ci == success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for ci == success")
	}
}

func TestEval_CIFailure(t *testing.T) {
	ctx := EvalContext{CIStatus: "failure"}
	result, err := Eval("ci == success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for ci == failure")
	}
}

func TestEval_And(t *testing.T) {
	ctx := EvalContext{Approvals: 3, CIStatus: "success"}
	result, err := Eval("approvals >= 2 AND ci == success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for AND with both true")
	}
}

func TestEval_AndFalse(t *testing.T) {
	ctx := EvalContext{Approvals: 1, CIStatus: "success"}
	result, err := Eval("approvals >= 2 AND ci == success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for AND with first false")
	}
}

func TestEval_Or(t *testing.T) {
	ctx := EvalContext{Approvals: 1, CIStatus: "success"}
	result, err := Eval("approvals >= 2 OR ci == success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for OR with second true")
	}
}

func TestEval_OrFalse(t *testing.T) {
	ctx := EvalContext{Approvals: 1, CIStatus: "failure"}
	result, err := Eval("approvals >= 2 OR ci == success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for OR with both false")
	}
}

func TestEval_Not(t *testing.T) {
	ctx := EvalContext{HasConflict: true}
	result, err := Eval("NOT conflict", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for NOT conflict when conflict is true")
	}
}

func TestEval_Complex(t *testing.T) {
	ctx := EvalContext{
		Approvals:  3,
		CIStatus:   "success",
		HasConflict: false,
	}
	result, err := Eval("approvals >= 2 AND ci == success AND NOT conflict", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for complex expression")
	}
}

func TestEval_Parentheses(t *testing.T) {
	ctx := EvalContext{Approvals: 3, CIStatus: "failure"}
	result, err := Eval("(approvals >= 2 OR ci == success)", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for parenthesized OR")
	}
}

func TestEval_CoreApproved(t *testing.T) {
	ctx := EvalContext{CoreApproved: 2, Required: 1}
	result, err := Eval("core_approved >= 1", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for core_approved >= 1")
	}
}

func TestEval_AgeHours(t *testing.T) {
	ctx := EvalContext{AgeHours: 48}
	result, err := Eval("age_hours > 24", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Fatal("expected true for age_hours > 24")
	}
}

func TestEval_Draft(t *testing.T) {
	ctx := EvalContext{IsDraft: true}
	result, err := Eval("NOT draft", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for NOT draft when draft is true")
	}
}

func TestEval_InvalidExpression(t *testing.T) {
	ctx := EvalContext{}
	_, err := Eval("invalid expression here", ctx)
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestEval_SimpleBoolean(t *testing.T) {
	ctx := EvalContext{HasConflict: false}
	result, err := Eval("conflict", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("expected false for conflict when HasConflict is false")
	}

	ctx2 := EvalContext{HasConflict: true}
	result2, err := Eval("conflict", ctx2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result2 {
		t.Fatal("expected true for conflict when HasConflict is true")
	}
}
