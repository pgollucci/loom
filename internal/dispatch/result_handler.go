package dispatch

import (
	"log"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/pkg/messages"
)

// ResultHandler tracks dispatched tasks and correlates incoming results.
type ResultHandler struct {
	pending map[string]*PendingTask // correlationID -> pending task
	mu      sync.RWMutex
}

// PendingTask tracks a task that was dispatched and is awaiting a result
type PendingTask struct {
	CorrelationID string
	ProjectID     string
	BeadID        string
	AgentID       string
	Role          string
	DispatchedAt  time.Time
	LastUpdate    time.Time
}

// NewResultHandler creates a new result handler
func NewResultHandler() *ResultHandler {
	rh := &ResultHandler{
		pending: make(map[string]*PendingTask),
	}

	go rh.reapStale()

	return rh
}

// Track registers a dispatched task for correlation tracking
func (rh *ResultHandler) Track(correlationID, projectID, beadID, agentID, role string) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	rh.pending[correlationID] = &PendingTask{
		CorrelationID: correlationID,
		ProjectID:     projectID,
		BeadID:        beadID,
		AgentID:       agentID,
		Role:          role,
		DispatchedAt:  time.Now(),
		LastUpdate:    time.Now(),
	}
}

// HandleResult matches a result to its pending task and returns it.
// Returns nil if no matching pending task is found.
func (rh *ResultHandler) HandleResult(result *messages.ResultMessage) *PendingTask {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	pt, ok := rh.pending[result.CorrelationID]
	if !ok {
		return nil
	}

	pt.LastUpdate = time.Now()

	switch result.Result.Status {
	case "success", "failure":
		delete(rh.pending, result.CorrelationID)
	case "in_progress":
		// Keep tracking
	}

	return pt
}

// PendingCount returns the number of tasks currently awaiting results
func (rh *ResultHandler) PendingCount() int {
	rh.mu.RLock()
	defer rh.mu.RUnlock()
	return len(rh.pending)
}

// GetPending returns a copy of all pending tasks
func (rh *ResultHandler) GetPending() []*PendingTask {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	result := make([]*PendingTask, 0, len(rh.pending))
	for _, pt := range rh.pending {
		result = append(result, pt)
	}
	return result
}

// reapStale removes pending tasks older than 1 hour with no updates
func (rh *ResultHandler) reapStale() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rh.mu.Lock()
		cutoff := time.Now().Add(-1 * time.Hour)
		for id, pt := range rh.pending {
			if pt.LastUpdate.Before(cutoff) {
				log.Printf("[ResultHandler] Reaping stale pending task: correlation=%s bead=%s agent=%s",
					id, pt.BeadID, pt.AgentID)
				delete(rh.pending, id)
			}
		}
		rh.mu.Unlock()
	}
}
