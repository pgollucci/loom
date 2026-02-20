package messages

import (
	"testing"
	"time"
)

func TestNewPlanCreated(t *testing.T) {
	plan := PlanData{
		Title:       "Fix login bug",
		Description: "Users cannot log in",
		Priority:    1,
		Steps: []PlanStep{
			{StepID: "s1", Role: "coder", Action: "implement", Description: "Fix it", Status: "pending"},
		},
	}
	msg := NewPlanCreated("proj-1", "bead-1", "plan-1", "orchestrator", plan, "corr-1")

	if msg.Type != "plan.created" {
		t.Errorf("got type %q, want plan.created", msg.Type)
	}
	if msg.PlanID != "plan-1" {
		t.Errorf("got plan ID %q", msg.PlanID)
	}
	if msg.ProjectID != "proj-1" {
		t.Errorf("got project %q", msg.ProjectID)
	}
	if msg.BeadID != "bead-1" {
		t.Errorf("got bead %q", msg.BeadID)
	}
	if msg.CreatedBy != "orchestrator" {
		t.Errorf("got created_by %q", msg.CreatedBy)
	}
	if msg.CorrelationID != "corr-1" {
		t.Errorf("got corr %q", msg.CorrelationID)
	}
	if msg.Timestamp.IsZero() {
		t.Error("timestamp not set")
	}
	if len(msg.Plan.Steps) != 1 {
		t.Errorf("got %d steps", len(msg.Plan.Steps))
	}
}

func TestNewReviewRequested(t *testing.T) {
	review := ReviewData{
		Commits:  []string{"abc123"},
		Decision: "",
		Summary:  "needs review",
	}
	msg := NewReviewRequested("proj-1", "bead-2", review, "corr-2")

	if msg.Type != "review.requested" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ProjectID != "proj-1" {
		t.Errorf("got project %q", msg.ProjectID)
	}
	if msg.BeadID != "bead-2" {
		t.Errorf("got bead %q", msg.BeadID)
	}
	if len(msg.Review.Commits) != 1 {
		t.Error("missing commits")
	}
	if msg.ReviewerID != "" {
		t.Errorf("reviewer should be empty, got %q", msg.ReviewerID)
	}
}

func TestNewReviewCompleted(t *testing.T) {
	review := ReviewData{
		Score:    95,
		Decision: "approve",
		Summary:  "LGTM",
		Comments: []ReviewComment{{File: "main.go", Line: 10, Severity: "info", Body: "nice"}},
	}
	msg := NewReviewCompleted("proj-1", "bead-3", "reviewer-1", review, "corr-3")

	if msg.Type != "review.completed" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ReviewerID != "reviewer-1" {
		t.Errorf("got reviewer %q", msg.ReviewerID)
	}
	if msg.Review.Score != 95 {
		t.Errorf("got score %d", msg.Review.Score)
	}
	if len(msg.Review.Comments) != 1 {
		t.Errorf("got %d comments", len(msg.Review.Comments))
	}
}

func TestAgentCommunicationMessageFields(t *testing.T) {
	msg := AgentCommunicationMessage{
		MessageID:       "msg-1",
		Type:            "agent_message",
		FromAgentID:     "agent-a",
		ToAgentID:       "agent-b",
		Subject:         "help",
		Body:            "need review",
		Priority:        "high",
		RequiresResponse: true,
		SourceContainer: "container-1",
		Timestamp:       time.Now(),
	}

	if msg.MessageID != "msg-1" {
		t.Error("message ID mismatch")
	}
	if msg.FromAgentID != "agent-a" || msg.ToAgentID != "agent-b" {
		t.Error("agent ID mismatch")
	}
	if !msg.RequiresResponse {
		t.Error("requires_response should be true")
	}
}

func TestPlanStepDependencies(t *testing.T) {
	steps := []PlanStep{
		{StepID: "s1", Role: "coder", Action: "implement", Status: "pending"},
		{StepID: "s2", Role: "reviewer", Action: "review", DependsOn: []string{"s1"}, Status: "pending"},
		{StepID: "s3", Role: "qa", Action: "test", DependsOn: []string{"s2"}, Status: "pending"},
	}

	if len(steps[0].DependsOn) != 0 {
		t.Error("step 1 should have no deps")
	}
	if steps[1].DependsOn[0] != "s1" {
		t.Errorf("step 2 should depend on s1, got %v", steps[1].DependsOn)
	}
	if steps[2].DependsOn[0] != "s2" {
		t.Errorf("step 3 should depend on s2, got %v", steps[2].DependsOn)
	}
}
