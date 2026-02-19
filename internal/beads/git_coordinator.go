package beads

import (
	"context"
	"log"
	"time"
)

// GitCoordinator replaces DoltCoordinator for git-based federation
// It periodically pulls the beads-sync branch to coordinate with other instances
type GitCoordinator struct {
	worktreeManager interface{} // GitWorktreeManager interface
	projectID       string
	syncInterval    time.Duration
}

// NewGitCoordinator creates a new git-based coordinator
func NewGitCoordinator(projectID string, wtManager interface{}, syncInterval time.Duration) *GitCoordinator {
	return &GitCoordinator{
		worktreeManager: wtManager,
		projectID:       projectID,
		syncInterval:    syncInterval,
	}
}

// StartSyncLoop pulls beads branch periodically for coordination
// This replaces Dolt-based federation with simple git pull/push
func (c *GitCoordinator) StartSyncLoop(ctx context.Context, beadsMgr *Manager) {
	ticker := time.NewTicker(c.syncInterval)
	defer ticker.Stop()

	log.Printf("[GitCoordinator] Starting sync loop for project %s (interval: %s)", c.projectID, c.syncInterval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[GitCoordinator] Stopping sync loop for project %s", c.projectID)
			return
		case <-ticker.C:
			if err := c.syncBeads(ctx, beadsMgr); err != nil {
				log.Printf("[GitCoordinator] Sync failed for %s: %v", c.projectID, err)
			} else {
				log.Printf("[GitCoordinator] Sync successful for %s", c.projectID)
			}
		}
	}
}

// syncBeads pulls latest beads from remote and reloads cache
func (c *GitCoordinator) syncBeads(ctx context.Context, beadsMgr *Manager) error {
	// Pull latest beads from remote using worktree manager
	type syncer interface {
		SyncBeadsBranch(string) error
	}
	if wt, ok := c.worktreeManager.(syncer); ok {
		if err := wt.SyncBeadsBranch(c.projectID); err != nil {
			return err
		}
	}

	// Reload beads into cache from filesystem
	// This picks up changes from other instances that pushed to git
	return beadsMgr.LoadBeadsFromFilesystem(c.projectID, beadsMgr.GetProjectBeadsPath(c.projectID))
}
