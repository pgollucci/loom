package dispatch

import (
	"testing"

	"github.com/jordanhubbard/loom/pkg/messages"
)

func TestResultHandler_TrackAndHandle(t *testing.T) {
	rh := &ResultHandler{pending: make(map[string]*PendingTask)}

	rh.Track("corr-1", "proj-1", "bead-1", "agent-1", "coder")
	rh.Track("corr-2", "proj-1", "bead-2", "agent-2", "reviewer")

	if rh.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", rh.PendingCount())
	}

	// Handle success result removes from pending
	result := &messages.ResultMessage{
		CorrelationID: "corr-1",
		Result:        messages.ResultData{Status: "success"},
	}
	pt := rh.HandleResult(result)
	if pt == nil {
		t.Fatal("expected non-nil pending task")
	}
	if pt.CorrelationID != "corr-1" {
		t.Errorf("got corr %q", pt.CorrelationID)
	}
	if pt.BeadID != "bead-1" {
		t.Errorf("got bead %q", pt.BeadID)
	}
	if pt.Role != "coder" {
		t.Errorf("got role %q", pt.Role)
	}

	if rh.PendingCount() != 1 {
		t.Errorf("expected 1 pending after success, got %d", rh.PendingCount())
	}
}

func TestResultHandler_HandleFailureRemoves(t *testing.T) {
	rh := &ResultHandler{pending: make(map[string]*PendingTask)}
	rh.Track("corr-1", "proj-1", "bead-1", "agent-1", "coder")

	result := &messages.ResultMessage{
		CorrelationID: "corr-1",
		Result:        messages.ResultData{Status: "failure"},
	}
	pt := rh.HandleResult(result)
	if pt == nil {
		t.Fatal("expected pending task for failure result")
	}
	if rh.PendingCount() != 0 {
		t.Errorf("expected 0 pending after failure, got %d", rh.PendingCount())
	}
}

func TestResultHandler_HandleInProgressKeepsTracking(t *testing.T) {
	rh := &ResultHandler{pending: make(map[string]*PendingTask)}
	rh.Track("corr-1", "proj-1", "bead-1", "agent-1", "coder")

	result := &messages.ResultMessage{
		CorrelationID: "corr-1",
		Result:        messages.ResultData{Status: "in_progress"},
	}
	pt := rh.HandleResult(result)
	if pt == nil {
		t.Fatal("expected pending task for in_progress")
	}
	if rh.PendingCount() != 1 {
		t.Errorf("expected 1 still pending, got %d", rh.PendingCount())
	}
}

func TestResultHandler_HandleUnknownCorrelation(t *testing.T) {
	rh := &ResultHandler{pending: make(map[string]*PendingTask)}

	result := &messages.ResultMessage{
		CorrelationID: "unknown",
		Result:        messages.ResultData{Status: "success"},
	}
	pt := rh.HandleResult(result)
	if pt != nil {
		t.Error("expected nil for unknown correlation")
	}
}

func TestResultHandler_GetPending(t *testing.T) {
	rh := &ResultHandler{pending: make(map[string]*PendingTask)}
	rh.Track("corr-1", "proj-1", "bead-1", "agent-1", "coder")
	rh.Track("corr-2", "proj-1", "bead-2", "agent-2", "qa")

	pending := rh.GetPending()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}
}

func TestResultHandler_EmptyPending(t *testing.T) {
	rh := &ResultHandler{pending: make(map[string]*PendingTask)}

	if rh.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", rh.PendingCount())
	}
	pending := rh.GetPending()
	if len(pending) != 0 {
		t.Errorf("expected empty list, got %d", len(pending))
	}
}
