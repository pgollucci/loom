package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/jordanhubbard/loom/pkg/messages"
)

// ReviewBus abstracts message publishing for the review gate.
type ReviewBus interface {
	PublishReview(ctx context.Context, projectID string, review *messages.ReviewMessage) error
	PublishTaskForRole(ctx context.Context, projectID, role string, task *messages.TaskMessage) error
}

// ReviewGate automatically creates review requests when code changes are detected.
// It listens for results from coder agents and triggers reviewer agents.
type ReviewGate struct {
	bus         ReviewBus
	beadCreator BeadCreator
}

// NewReviewGate creates a new review gate
func NewReviewGate(bus ReviewBus, beadCreator BeadCreator) *ReviewGate {
	return &ReviewGate{
		bus:         bus,
		beadCreator: beadCreator,
	}
}

// CreateReview creates a review bead and dispatches it to a reviewer.
func (rg *ReviewGate) CreateReview(ctx context.Context, projectID, sourceBeadID, correlationID string, codeResult *messages.ResultMessage) (string, error) {
	if len(codeResult.Result.Commits) == 0 && len(codeResult.Result.Artifacts) == 0 {
		log.Printf("[ReviewGate] No commits/artifacts in result for bead %s, skipping review", sourceBeadID)
		return "", nil
	}

	tags := []string{"auto-review", "source:" + sourceBeadID}
	title := fmt.Sprintf("Review code changes for %s", sourceBeadID)
	desc := fmt.Sprintf("Review the following changes:\nCommits: %v\nArtifacts: %v",
		codeResult.Result.Commits, codeResult.Result.Artifacts)

	beadID, err := rg.beadCreator.CreateBead(projectID, title, desc, "review", 2, tags, sourceBeadID)
	if err != nil {
		return "", fmt.Errorf("failed to create review bead: %w", err)
	}

	reviewMsg := messages.NewReviewRequested(projectID, beadID, messages.ReviewData{
		Commits:      codeResult.Result.Commits,
		FilesChanged: codeResult.Result.Artifacts,
		Context: map[string]interface{}{
			"source_bead":  sourceBeadID,
			"source_agent": codeResult.AgentID,
		},
	}, correlationID)

	if err := rg.bus.PublishReview(ctx, projectID, reviewMsg); err != nil {
		log.Printf("[ReviewGate] Warning: Failed to publish review request: %v", err)
	}

	// Also publish as a task to the reviewer role
	taskMsg := messages.TaskAssigned(projectID, beadID, "", messages.TaskData{
		Title:       title,
		Description: desc,
		Priority:    2,
		Type:        "review",
		Context: map[string]interface{}{
			"commits":     codeResult.Result.Commits,
			"artifacts":   codeResult.Result.Artifacts,
			"source_bead": sourceBeadID,
		},
	}, correlationID)

	if err := rg.bus.PublishTaskForRole(ctx, projectID, "reviewer", taskMsg); err != nil {
		log.Printf("[ReviewGate] Warning: Failed to dispatch review task: %v", err)
	}

	log.Printf("[ReviewGate] Created review bead %s for source %s", beadID, sourceBeadID)
	return beadID, nil
}
