package messages

import (
	"testing"
)

func TestTaskAssigned(t *testing.T) {
	td := TaskData{
		Title:       "Fix login bug",
		Description: "Users can't log in",
		Priority:    1,
		Type:        "bug",
		Context:     map[string]interface{}{"branch": "main"},
		WorkDir:     "/workspace",
	}
	msg := TaskAssigned("proj-1", "bead-1", "agent-1", td, "corr-1")

	if msg.Type != "task.assigned" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ProjectID != "proj-1" {
		t.Errorf("got project %q", msg.ProjectID)
	}
	if msg.BeadID != "bead-1" {
		t.Errorf("got bead %q", msg.BeadID)
	}
	if msg.AssignedTo != "agent-1" {
		t.Errorf("got assigned_to %q", msg.AssignedTo)
	}
	if msg.CorrelationID != "corr-1" {
		t.Errorf("got corr %q", msg.CorrelationID)
	}
	if msg.TaskData.Title != "Fix login bug" {
		t.Errorf("got title %q", msg.TaskData.Title)
	}
	if msg.TaskData.WorkDir != "/workspace" {
		t.Errorf("got work_dir %q", msg.TaskData.WorkDir)
	}
	if msg.Timestamp.IsZero() {
		t.Error("timestamp not set")
	}
}

func TestTaskUpdated(t *testing.T) {
	td := TaskData{Title: "Updated task", Priority: 2}
	msg := TaskUpdated("proj-1", "bead-1", "agent-1", td, "corr-2")

	if msg.Type != "task.updated" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.TaskData.Priority != 2 {
		t.Errorf("got priority %d", msg.TaskData.Priority)
	}
}

func TestTaskCancelled(t *testing.T) {
	msg := TaskCancelled("proj-1", "bead-1", "agent-1", "corr-3")

	if msg.Type != "task.cancelled" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ProjectID != "proj-1" {
		t.Errorf("got project %q", msg.ProjectID)
	}
	if msg.BeadID != "bead-1" {
		t.Errorf("got bead %q", msg.BeadID)
	}
	if msg.AssignedTo != "agent-1" {
		t.Errorf("got assigned_to %q", msg.AssignedTo)
	}
	if msg.TaskData.Title != "" {
		t.Errorf("task data should be empty, got title %q", msg.TaskData.Title)
	}
}
