// Package statusboard provides a simple status board where agents post
// meeting notes, status reports, and announcements visible to the CEO and UI.
package statusboard

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Entry is a single post on the status board.
type Entry struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Category  string    `json:"category"` // meeting_notes, status_report, announcement, feedback
	Content   string    `json:"content"`
	AuthorID  string    `json:"author_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Board is the in-memory status board.
type Board struct {
	entries []*Entry
	mu      sync.RWMutex
	nextID  int
}

// New creates a new status board.
func New() *Board {
	return &Board{
		entries: make([]*Entry, 0),
	}
}

// Post adds an entry to the board.
func (b *Board) Post(_ context.Context, projectID, category, content, authorID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	b.entries = append(b.entries, &Entry{
		ID:        fmt.Sprintf("sb-%04d", b.nextID),
		ProjectID: projectID,
		Category:  category,
		Content:   content,
		AuthorID:  authorID,
		CreatedAt: time.Now().UTC(),
	})

	if len(b.entries) > 500 {
		b.entries = b.entries[len(b.entries)-500:]
	}

	return nil
}

// PostToBoard implements actions.StatusBoardPoster.
func (b *Board) PostToBoard(ctx context.Context, projectID, category, content, authorID string) error {
	return b.Post(ctx, projectID, category, content, authorID)
}

// List returns entries filtered by project and optional category.
func (b *Board) List(projectID, category string, limit int) []*Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []*Entry
	for i := len(b.entries) - 1; i >= 0 && len(result) < limit; i-- {
		e := b.entries[i]
		if projectID != "" && e.ProjectID != projectID {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		result = append(result, e)
	}
	return result
}
