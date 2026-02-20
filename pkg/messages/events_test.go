package messages

import (
	"testing"
)

func TestBeadCreated(t *testing.T) {
	msg := BeadCreated("proj-1", "bead-1", "control-plane")

	if msg.Type != "bead.created" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Source != "control-plane" {
		t.Errorf("got source %q", msg.Source)
	}
	if msg.ProjectID != "proj-1" {
		t.Errorf("got project %q", msg.ProjectID)
	}
	if msg.EntityID != "bead-1" {
		t.Errorf("got entity %q", msg.EntityID)
	}
	if msg.Event.Action != "created" {
		t.Errorf("got action %q", msg.Event.Action)
	}
	if msg.Event.Category != "bead" {
		t.Errorf("got category %q", msg.Event.Category)
	}
	if msg.Timestamp.IsZero() {
		t.Error("timestamp not set")
	}
}

func TestBeadUpdated(t *testing.T) {
	data := map[string]interface{}{"status": "in_progress"}
	msg := BeadUpdated("proj-1", "bead-1", "dispatcher", data)

	if msg.Type != "bead.updated" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Event.Action != "updated" {
		t.Errorf("got action %q", msg.Event.Action)
	}
	if msg.Event.Data["status"] != "in_progress" {
		t.Error("data not preserved")
	}
}

func TestBeadUpdatedNilData(t *testing.T) {
	msg := BeadUpdated("proj-1", "bead-1", "source", nil)
	if msg.Event.Data != nil {
		t.Error("expected nil data")
	}
}

func TestAgentStarted(t *testing.T) {
	msg := AgentStarted("agent-1", "swarm-manager")

	if msg.Type != "agent.started" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.EntityID != "agent-1" {
		t.Errorf("got entity %q", msg.EntityID)
	}
	if msg.Event.Category != "agent" {
		t.Errorf("got category %q", msg.Event.Category)
	}
	if msg.ProjectID != "" {
		t.Errorf("project should be empty, got %q", msg.ProjectID)
	}
}

func TestDispatchCycle(t *testing.T) {
	data := map[string]interface{}{"beads_dispatched": 3}
	msg := DispatchCycle("proj-1", "dispatcher", data)

	if msg.Type != "dispatch.cycle" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Event.Action != "cycle" {
		t.Errorf("got action %q", msg.Event.Action)
	}
	if msg.Event.Category != "dispatch" {
		t.Errorf("got category %q", msg.Event.Category)
	}
	if msg.Event.Data["beads_dispatched"] != 3 {
		t.Error("data lost")
	}
}

func TestSystemError(t *testing.T) {
	data := map[string]interface{}{"code": 500}
	msg := SystemError("api", "internal error", data)

	if msg.Type != "system.error" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Event.Description != "internal error" {
		t.Errorf("got description %q", msg.Event.Description)
	}
	if msg.Event.Category != "system" {
		t.Errorf("got category %q", msg.Event.Category)
	}
	if msg.Event.Data["code"] != 500 {
		t.Error("data lost")
	}
	if msg.Source != "api" {
		t.Errorf("got source %q", msg.Source)
	}
}
