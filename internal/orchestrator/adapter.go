package orchestrator

import (
	"github.com/jordanhubbard/loom/internal/beads"
	"github.com/jordanhubbard/loom/pkg/models"
)

// BeadManagerAdapter adapts beads.Manager to the BeadCreator/BeadUpdater interfaces
// expected by the PDA orchestrator.
type BeadManagerAdapter struct {
	manager *beads.Manager
}

// NewBeadManagerAdapter wraps a beads.Manager for use by the orchestrator.
func NewBeadManagerAdapter(mgr *beads.Manager) *BeadManagerAdapter {
	return &BeadManagerAdapter{manager: mgr}
}

// CreateBead creates a new bead, adapting the orchestrator's signature to beads.Manager's.
func (a *BeadManagerAdapter) CreateBead(projectID, title, description, beadType string, priority int, tags []string, parentID string) (string, error) {
	bead, err := a.manager.CreateBead(title, description, models.BeadPriority(priority), beadType, projectID)
	if err != nil {
		return "", err
	}

	// Set tags and parent via update
	updates := make(map[string]interface{})
	if len(tags) > 0 {
		updates["tags"] = tags
	}
	if parentID != "" {
		updates["parent_id"] = parentID
	}
	if len(updates) > 0 {
		_ = a.manager.UpdateBead(bead.ID, updates)
	}

	return bead.ID, nil
}

// UpdateBead delegates to the underlying manager.
func (a *BeadManagerAdapter) UpdateBead(id string, updates map[string]interface{}) error {
	return a.manager.UpdateBead(id, updates)
}
