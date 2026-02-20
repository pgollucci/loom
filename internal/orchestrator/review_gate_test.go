package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jordanhubbard/loom/pkg/messages"
)

type mockReviewBus struct {
	mu             sync.Mutex
	reviews        []*messages.ReviewMessage
	tasks          []*messages.TaskMessage
	taskRoles      []string
	publishErr     error
}

func (m *mockReviewBus) PublishReview(_ context.Context, _ string, review *messages.ReviewMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.reviews = append(m.reviews, review)
	return nil
}

func (m *mockReviewBus) PublishTaskForRole(_ context.Context, _ string, role string, task *messages.TaskMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.tasks = append(m.tasks, task)
	m.taskRoles = append(m.taskRoles, role)
	return nil
}

func TestReviewGate_NewReviewGate(t *testing.T) {
	bus := &mockReviewBus{}
	creator := &mockBeadCreator{}
	gate := NewReviewGate(bus, creator)
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

func TestReviewGate_CreateReview_Success(t *testing.T) {
	bus := &mockReviewBus{}
	creator := &mockBeadCreator{}
	gate := NewReviewGate(bus, creator)

	result := &messages.ResultMessage{
		AgentID: "coder-1",
		BeadID:  "source-bead",
		Result: messages.ResultData{
			Status:    "success",
			Commits:   []string{"abc123"},
			Artifacts: []string{"main.go", "test.go"},
		},
	}

	beadID, err := gate.CreateReview(context.Background(), "proj-1", "source-bead", "corr-1", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if beadID == "" {
		t.Error("expected bead to be created")
	}

	creator.mu.Lock()
	if creator.count != 1 {
		t.Errorf("expected 1 bead, got %d", creator.count)
	}
	creator.mu.Unlock()

	bus.mu.Lock()
	if len(bus.reviews) != 1 {
		t.Errorf("expected 1 review published, got %d", len(bus.reviews))
	}
	if len(bus.tasks) != 1 {
		t.Errorf("expected 1 task published, got %d", len(bus.tasks))
	}
	if len(bus.taskRoles) > 0 && bus.taskRoles[0] != "reviewer" {
		t.Errorf("expected reviewer role, got %q", bus.taskRoles[0])
	}
	bus.mu.Unlock()
}

func TestReviewGate_CreateReview_SkipEmpty(t *testing.T) {
	bus := &mockReviewBus{}
	gate := NewReviewGate(bus, &mockBeadCreator{})

	result := &messages.ResultMessage{
		Result: messages.ResultData{Commits: nil, Artifacts: nil},
	}

	beadID, err := gate.CreateReview(context.Background(), "proj-1", "bead-1", "corr-1", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if beadID != "" {
		t.Errorf("expected empty bead ID for skipped review, got %q", beadID)
	}

	bus.mu.Lock()
	if len(bus.reviews) != 0 {
		t.Error("should not publish anything when no commits/artifacts")
	}
	bus.mu.Unlock()
}

func TestReviewGate_CreateReview_BeadCreationError(t *testing.T) {
	bus := &mockReviewBus{}
	gate := NewReviewGate(bus, &failingBeadCreator{})

	result := &messages.ResultMessage{
		Result: messages.ResultData{Commits: []string{"abc"}},
	}

	_, err := gate.CreateReview(context.Background(), "proj-1", "bead-1", "corr-1", result)
	if err == nil {
		t.Error("expected error from failing bead creator")
	}
}

func TestReviewGate_CreateReview_PublishError(t *testing.T) {
	bus := &mockReviewBus{publishErr: fmt.Errorf("publish failed")}
	gate := NewReviewGate(bus, &mockBeadCreator{})

	result := &messages.ResultMessage{
		Result: messages.ResultData{Commits: []string{"abc"}},
	}

	beadID, err := gate.CreateReview(context.Background(), "proj-1", "bead-1", "corr-1", result)
	if err != nil {
		t.Fatalf("publish errors are logged, not returned: %v", err)
	}
	if beadID == "" {
		t.Error("bead should still be created even if publish fails")
	}
}

func TestReviewGate_CreateReview_WithOnlyArtifacts(t *testing.T) {
	bus := &mockReviewBus{}
	gate := NewReviewGate(bus, &mockBeadCreator{})

	result := &messages.ResultMessage{
		Result: messages.ResultData{Artifacts: []string{"file.go"}},
	}

	beadID, err := gate.CreateReview(context.Background(), "proj-1", "bead-1", "corr-1", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if beadID == "" {
		t.Error("should create review for artifacts-only result")
	}
}

type failingBeadCreator struct{}

func (f *failingBeadCreator) CreateBead(_, _, _, _ string, _ int, _ []string, _ string) (string, error) {
	return "", fmt.Errorf("bead creation failed")
}
