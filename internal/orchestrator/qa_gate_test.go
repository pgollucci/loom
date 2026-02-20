package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jordanhubbard/loom/pkg/messages"
)

type mockQABus struct {
	mu         sync.Mutex
	tasks      []*messages.TaskMessage
	taskRoles  []string
	publishErr error
}

func (m *mockQABus) PublishTaskForRole(_ context.Context, _ string, role string, task *messages.TaskMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.tasks = append(m.tasks, task)
	m.taskRoles = append(m.taskRoles, role)
	return nil
}

func TestQAGate_NewQAGate(t *testing.T) {
	bus := &mockQABus{}
	creator := &mockBeadCreator{}
	gate := NewQAGate(bus, creator)
	if gate == nil {
		t.Fatal("expected non-nil gate")
	}
	if gate.beadCreator == nil {
		t.Error("bead creator should be set")
	}
	if gate.bus == nil {
		t.Error("bus should be set")
	}
}

func TestQAGate_CreateQATask_Success(t *testing.T) {
	bus := &mockQABus{}
	creator := &mockBeadCreator{}
	gate := NewQAGate(bus, creator)

	reviewResult := &messages.ResultMessage{
		BeadID: "review-bead-1",
		Result: messages.ResultData{Status: "success"},
	}

	beadID, err := gate.CreateQATask(context.Background(), "proj-1", "review-bead-1", "corr-1", reviewResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if beadID == "" {
		t.Error("expected QA bead to be created")
	}

	creator.mu.Lock()
	if creator.count != 1 {
		t.Errorf("expected 1 bead, got %d", creator.count)
	}
	creator.mu.Unlock()

	bus.mu.Lock()
	if len(bus.tasks) != 1 {
		t.Errorf("expected 1 QA task published, got %d", len(bus.tasks))
	}
	if len(bus.taskRoles) > 0 && bus.taskRoles[0] != "qa" {
		t.Errorf("expected qa role, got %q", bus.taskRoles[0])
	}
	bus.mu.Unlock()
}

func TestQAGate_CreateQATask_BeadCreationError(t *testing.T) {
	bus := &mockQABus{}
	gate := NewQAGate(bus, &failingBeadCreator{})

	reviewResult := &messages.ResultMessage{
		Result: messages.ResultData{Status: "success"},
	}

	_, err := gate.CreateQATask(context.Background(), "proj-1", "review-1", "corr-1", reviewResult)
	if err == nil {
		t.Error("expected error from failing bead creator")
	}
}

func TestQAGate_CreateQATask_PublishError(t *testing.T) {
	bus := &mockQABus{publishErr: fmt.Errorf("publish failed")}
	gate := NewQAGate(bus, &mockBeadCreator{})

	reviewResult := &messages.ResultMessage{
		Result: messages.ResultData{Status: "success"},
	}

	beadID, err := gate.CreateQATask(context.Background(), "proj-1", "review-1", "corr-1", reviewResult)
	if err != nil {
		t.Fatalf("publish errors are logged, not returned: %v", err)
	}
	if beadID == "" {
		t.Error("bead should still be created even if publish fails")
	}
}
