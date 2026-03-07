package feedback

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager handles feedback operations using in-memory storage.
// A persistent implementation should replace the map with database calls once
// the database schema supports it (see internal/database/migrations_feedback.go).
type Manager struct {
	mu       sync.RWMutex
	items    map[string]*Feedback
	eventBus eventPublisher
}

type eventPublisher interface {
	Publish(topic string, payload interface{}) error
}

// Feedback represents user feedback on a bead or agent action.
type Feedback struct {
	ID        string                 `json:"id"`
	BeadID    string                 `json:"bead_id,omitempty"`
	AgentID   string                 `json:"agent_id,omitempty"`
	AuthorID  string                 `json:"author_id"`
	Author    string                 `json:"author"`
	Rating    int                    `json:"rating"` // 1-5 scale
	Category  string                 `json:"category"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// NewManager creates a new feedback manager.
func NewManager(bus eventPublisher) *Manager {
	return &Manager{
		items:    make(map[string]*Feedback),
		eventBus: bus,
	}
}

// CreateFeedback creates new feedback.
func (m *Manager) CreateFeedback(beadID, agentID, authorID, author, category, content string, rating int, metadata map[string]interface{}) (*Feedback, error) {
	if rating < 1 || rating > 5 {
		return nil, fmt.Errorf("rating must be between 1 and 5")
	}
	if category == "" {
		return nil, fmt.Errorf("category is required")
	}

	now := time.Now()
	fb := &Feedback{
		ID:        uuid.New().String(),
		BeadID:    beadID,
		AgentID:   agentID,
		AuthorID:  authorID,
		Author:    author,
		Rating:    rating,
		Category:  category,
		Content:   content,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.mu.Lock()
	m.items[fb.ID] = fb
	m.mu.Unlock()

	if m.eventBus != nil {
		_ = m.eventBus.Publish("feedback.created", fb)
	}
	return fb, nil
}

// GetFeedback retrieves feedback by ID.
func (m *Manager) GetFeedback(feedbackID string) (*Feedback, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fb, ok := m.items[feedbackID]
	if !ok {
		return nil, fmt.Errorf("feedback not found: %s", feedbackID)
	}
	return fb, nil
}

// ListFeedback returns all feedback, optionally filtered.
func (m *Manager) ListFeedback(projectID, beadID, agentID string, limit int) ([]*Feedback, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Feedback
	for _, fb := range m.items {
		if beadID != "" && fb.BeadID != beadID {
			continue
		}
		if agentID != "" && fb.AgentID != agentID {
			continue
		}
		result = append(result, fb)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// UpdateFeedback updates existing feedback.
func (m *Manager) UpdateFeedback(feedbackID, content string, rating int) (*Feedback, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fb, ok := m.items[feedbackID]
	if !ok {
		return nil, fmt.Errorf("feedback not found: %s", feedbackID)
	}
	if content != "" {
		fb.Content = content
	}
	if rating >= 1 && rating <= 5 {
		fb.Rating = rating
	}
	fb.UpdatedAt = time.Now()
	return fb, nil
}

// DeleteFeedback removes feedback by ID.
func (m *Manager) DeleteFeedback(feedbackID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.items[feedbackID]; !ok {
		return fmt.Errorf("feedback not found: %s", feedbackID)
	}
	delete(m.items, feedbackID)
	return nil
}
