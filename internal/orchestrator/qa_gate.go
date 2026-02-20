package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/jordanhubbard/loom/pkg/messages"
)

// QABus abstracts message publishing for the QA gate.
type QABus interface {
	PublishTaskForRole(ctx context.Context, projectID, role string, task *messages.TaskMessage) error
}

// QAGate automatically creates build+test tasks after review approval.
// It listens for completed reviews and dispatches QA work to qa agents.
type QAGate struct {
	bus         QABus
	beadCreator BeadCreator
}

// NewQAGate creates a new QA gate
func NewQAGate(bus QABus, beadCreator BeadCreator) *QAGate {
	return &QAGate{
		bus:         bus,
		beadCreator: beadCreator,
	}
}

// CreateQATask creates a QA bead and dispatches it to a QA agent.
func (qg *QAGate) CreateQATask(ctx context.Context, projectID, reviewBeadID, correlationID string, reviewResult *messages.ResultMessage) (string, error) {
	tags := []string{"auto-qa", "review:" + reviewBeadID}
	title := fmt.Sprintf("Build & test after review %s", reviewBeadID)
	desc := fmt.Sprintf("Build and run all tests to validate the reviewed changes.\nReview bead: %s\nReview status: %s",
		reviewBeadID, reviewResult.Result.Status)

	beadID, err := qg.beadCreator.CreateBead(projectID, title, desc, "test", 2, tags, reviewBeadID)
	if err != nil {
		return "", fmt.Errorf("failed to create QA bead: %w", err)
	}

	taskMsg := messages.TaskAssigned(projectID, beadID, "", messages.TaskData{
		Title:       title,
		Description: desc,
		Priority:    2,
		Type:        "test",
		Context: map[string]interface{}{
			"review_bead": reviewBeadID,
			"commands":    []string{"make test", "make lint"},
		},
	}, correlationID)

	if err := qg.bus.PublishTaskForRole(ctx, projectID, "qa", taskMsg); err != nil {
		log.Printf("[QAGate] Warning: Failed to dispatch QA task: %v", err)
	}

	log.Printf("[QAGate] Created QA bead %s after review %s", beadID, reviewBeadID)
	return beadID, nil
}
