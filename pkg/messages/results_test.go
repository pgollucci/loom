package messages

import (
	"testing"
)

func TestTaskCompleted(t *testing.T) {
	rd := ResultData{
		Status:   "success",
		Output:   "all tests pass",
		Commits:  []string{"abc123"},
		Duration: 5000,
	}
	msg := TaskCompleted("proj-1", "bead-1", "agent-1", rd, "corr-1")

	if msg.Type != "task.completed" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ProjectID != "proj-1" {
		t.Errorf("got project %q", msg.ProjectID)
	}
	if msg.AgentID != "agent-1" {
		t.Errorf("got agent %q", msg.AgentID)
	}
	if msg.Result.Status != "success" {
		t.Errorf("got status %q", msg.Result.Status)
	}
	if msg.Result.Output != "all tests pass" {
		t.Errorf("got output %q", msg.Result.Output)
	}
	if len(msg.Result.Commits) != 1 {
		t.Errorf("got %d commits", len(msg.Result.Commits))
	}
	if msg.Timestamp.IsZero() {
		t.Error("timestamp not set")
	}
}

func TestTaskFailed(t *testing.T) {
	rd := ResultData{
		Status: "failure",
		Error:  "compilation error",
		Output: "build failed",
	}
	msg := TaskFailed("proj-1", "bead-1", "agent-1", rd, "corr-2")

	if msg.Type != "task.failed" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Result.Error != "compilation error" {
		t.Errorf("got error %q", msg.Result.Error)
	}
}

func TestTaskProgress(t *testing.T) {
	rd := ResultData{
		Status: "in_progress",
		Output: "running tests...",
	}
	msg := TaskProgress("proj-1", "bead-1", "agent-1", rd, "corr-3")

	if msg.Type != "task.progress" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Result.Status != "in_progress" {
		t.Errorf("got status %q", msg.Result.Status)
	}
}

func TestResultDataAllFields(t *testing.T) {
	rd := ResultData{
		Status:     "success",
		Output:     "done",
		Error:      "",
		Commits:    []string{"a", "b"},
		Artifacts:  []string{"/tmp/out.tar"},
		Duration:   12000,
		NextAction: "close",
		Context:    map[string]interface{}{"key": "val"},
	}

	if len(rd.Commits) != 2 {
		t.Errorf("got %d commits", len(rd.Commits))
	}
	if len(rd.Artifacts) != 1 {
		t.Errorf("got %d artifacts", len(rd.Artifacts))
	}
	if rd.NextAction != "close" {
		t.Errorf("got next_action %q", rd.NextAction)
	}
}
