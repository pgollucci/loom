package orchestrator

import (
	"context"
	"testing"
)

func TestParsePlanResponse_DirectJSON(t *testing.T) {
	input := `{"title":"Fix bug","description":"Fix login","steps":[{"step_id":"s1","role":"coder","action":"implement","description":"Code it"}],"priority":1}`
	plan, err := parsePlanResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Title != "Fix bug" {
		t.Errorf("got title %q", plan.Title)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Role != "coder" {
		t.Errorf("got role %q", plan.Steps[0].Role)
	}
}

func TestParsePlanResponse_MarkdownCodeBlock(t *testing.T) {
	input := "Here's the plan:\n```json\n{\"title\":\"Plan\",\"description\":\"d\",\"steps\":[],\"priority\":2}\n```\n"
	plan, err := parsePlanResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Title != "Plan" {
		t.Errorf("got title %q", plan.Title)
	}
	if plan.Priority != 2 {
		t.Errorf("got priority %d", plan.Priority)
	}
}

func TestParsePlanResponse_EmbeddedJSON(t *testing.T) {
	input := `I think we should do this: {"title":"Embedded","description":"d","steps":[],"priority":3} and that's the plan.`
	plan, err := parsePlanResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Title != "Embedded" {
		t.Errorf("got title %q", plan.Title)
	}
}

func TestParsePlanResponse_NoJSON(t *testing.T) {
	input := "This has no JSON at all."
	_, err := parsePlanResponse(input)
	if err == nil {
		t.Error("expected error for no JSON")
	}
}

func TestParsePlanResponse_InvalidJSON(t *testing.T) {
	input := `{broken json here`
	_, err := parsePlanResponse(input)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePlanResponse_WhitespaceWrapped(t *testing.T) {
	input := `   {"title":"Trimmed","description":"d","steps":[],"priority":1}   `
	plan, err := parsePlanResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Title != "Trimmed" {
		t.Errorf("got title %q", plan.Title)
	}
}

func TestNewLLMPlanner(t *testing.T) {
	p := NewLLMPlanner("http://llm:8000/v1", "sk-test", "gpt-4")
	if p == nil {
		t.Fatal("expected non-nil planner")
	}
	if p.model != "gpt-4" {
		t.Errorf("got model %q", p.model)
	}
	if p.provider == nil {
		t.Error("provider should be set")
	}
}

func TestStaticPlanner_GeneratePlan(t *testing.T) {
	planner := &StaticPlanner{}
	req := PlanRequest{
		ProjectID:   "proj-1",
		BeadID:      "bead-1",
		Title:       "Fix the login page",
		Description: "Login page crashes on submit",
	}

	plan, err := planner.GeneratePlan(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Title != "Fix the login page" {
		t.Errorf("got title %q", plan.Title)
	}
	if plan.Description != "Login page crashes on submit" {
		t.Errorf("got description %q", plan.Description)
	}
	if plan.Priority != 1 {
		t.Errorf("got priority %d", plan.Priority)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps (implement/review/test), got %d", len(plan.Steps))
	}

	// Verify roles
	roles := make(map[string]bool)
	for _, step := range plan.Steps {
		roles[step.Role] = true
		if step.StepID == "" {
			t.Error("step ID should not be empty")
		}
		if step.Status != "pending" {
			t.Errorf("step %q status should be pending, got %q", step.StepID, step.Status)
		}
	}
	if !roles["coder"] {
		t.Error("missing coder step")
	}
	if !roles["reviewer"] {
		t.Error("missing reviewer step")
	}
	if !roles["qa"] {
		t.Error("missing qa step")
	}

	// Verify actions
	actions := make(map[string]bool)
	for _, step := range plan.Steps {
		actions[step.Action] = true
	}
	if !actions["implement"] {
		t.Error("missing implement action")
	}
	if !actions["review"] {
		t.Error("missing review action")
	}
	if !actions["test"] {
		t.Error("missing test action")
	}
}
