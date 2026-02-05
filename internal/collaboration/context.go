package collaboration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SharedBeadContext represents shared context for agents collaborating on a bead
type SharedBeadContext struct {
	BeadID           string                 `json:"bead_id"`
	ProjectID        string                 `json:"project_id"`
	CollaboratingAgents []string            `json:"collaborating_agents"`
	Data             map[string]interface{} `json:"data"`
	ActivityLog      []ActivityEntry        `json:"activity_log"`
	Version          int64                  `json:"version"` // For conflict resolution
	LastUpdated      time.Time              `json:"last_updated"`
	LastUpdatedBy    string                 `json:"last_updated_by"`
	mu               sync.RWMutex
}

// ActivityEntry represents an agent activity in the bead context
type ActivityEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	AgentID     string                 `json:"agent_id"`
	ActivityType string                `json:"activity_type"` // joined, updated, left, message, file_modified
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data,omitempty"`
}

// ContextStore manages shared bead contexts
type ContextStore struct {
	contexts  map[string]*SharedBeadContext // beadID -> context
	mu        sync.RWMutex
	updates   chan ContextUpdate // Channel for real-time updates
	listeners map[string][]chan ContextUpdate // beadID -> listeners
	listenerMu sync.RWMutex
}

// ContextUpdate represents a context update event
type ContextUpdate struct {
	BeadID    string
	UpdateType string                 // joined, left, data_changed, activity
	AgentID   string
	Data      map[string]interface{}
	Timestamp time.Time
	Version   int64
}

// ConflictError indicates a version conflict during update
type ConflictError struct {
	BeadID          string
	ExpectedVersion int64
	ActualVersion   int64
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("version conflict for bead %s: expected %d, got %d",
		e.BeadID, e.ExpectedVersion, e.ActualVersion)
}

// NewContextStore creates a new context store
func NewContextStore() *ContextStore {
	store := &ContextStore{
		contexts:  make(map[string]*SharedBeadContext),
		updates:   make(chan ContextUpdate, 1000),
		listeners: make(map[string][]chan ContextUpdate),
	}

	// Start update distributor
	go store.distributeUpdates()

	return store
}

// GetOrCreate gets existing context or creates new one
func (s *ContextStore) GetOrCreate(ctx context.Context, beadID, projectID string) (*SharedBeadContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.contexts[beadID]; ok {
		return existing, nil
	}

	// Create new context
	newCtx := &SharedBeadContext{
		BeadID:              beadID,
		ProjectID:           projectID,
		CollaboratingAgents: []string{},
		Data:                make(map[string]interface{}),
		ActivityLog:         []ActivityEntry{},
		Version:             1,
		LastUpdated:         time.Now(),
	}

	s.contexts[beadID] = newCtx
	return newCtx, nil
}

// Get retrieves a context by bead ID
func (s *ContextStore) Get(ctx context.Context, beadID string) (*SharedBeadContext, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ctx, ok := s.contexts[beadID]; ok {
		return ctx, nil
	}

	return nil, fmt.Errorf("context not found for bead: %s", beadID)
}

// JoinBead adds an agent to the bead context
func (s *ContextStore) JoinBead(ctx context.Context, beadID, agentID string) error {
	s.mu.Lock()
	beadCtx, exists := s.contexts[beadID]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("context not found for bead: %s", beadID)
	}
	s.mu.Unlock()

	beadCtx.mu.Lock()
	defer beadCtx.mu.Unlock()

	// Check if already in context
	for _, agent := range beadCtx.CollaboratingAgents {
		if agent == agentID {
			return nil // Already joined
		}
	}

	// Add agent
	beadCtx.CollaboratingAgents = append(beadCtx.CollaboratingAgents, agentID)
	beadCtx.Version++
	beadCtx.LastUpdated = time.Now()
	beadCtx.LastUpdatedBy = agentID

	// Add activity log
	beadCtx.ActivityLog = append(beadCtx.ActivityLog, ActivityEntry{
		Timestamp:    time.Now(),
		AgentID:      agentID,
		ActivityType: "joined",
		Description:  fmt.Sprintf("Agent %s joined collaboration", agentID),
	})

	// Notify listeners
	s.notifyUpdate(ContextUpdate{
		BeadID:     beadID,
		UpdateType: "joined",
		AgentID:    agentID,
		Timestamp:  time.Now(),
		Version:    beadCtx.Version,
	})

	return nil
}

// LeaveBead removes an agent from the bead context
func (s *ContextStore) LeaveBead(ctx context.Context, beadID, agentID string) error {
	s.mu.Lock()
	beadCtx, exists := s.contexts[beadID]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("context not found for bead: %s", beadID)
	}
	s.mu.Unlock()

	beadCtx.mu.Lock()
	defer beadCtx.mu.Unlock()

	// Remove agent
	newAgents := []string{}
	found := false
	for _, agent := range beadCtx.CollaboratingAgents {
		if agent != agentID {
			newAgents = append(newAgents, agent)
		} else {
			found = true
		}
	}

	if !found {
		return nil // Not in context
	}

	beadCtx.CollaboratingAgents = newAgents
	beadCtx.Version++
	beadCtx.LastUpdated = time.Now()
	beadCtx.LastUpdatedBy = agentID

	// Add activity log
	beadCtx.ActivityLog = append(beadCtx.ActivityLog, ActivityEntry{
		Timestamp:    time.Now(),
		AgentID:      agentID,
		ActivityType: "left",
		Description:  fmt.Sprintf("Agent %s left collaboration", agentID),
	})

	// Notify listeners
	s.notifyUpdate(ContextUpdate{
		BeadID:     beadID,
		UpdateType: "left",
		AgentID:    agentID,
		Timestamp:  time.Now(),
		Version:    beadCtx.Version,
	})

	return nil
}

// UpdateData updates the shared data with optimistic locking
func (s *ContextStore) UpdateData(ctx context.Context, beadID, agentID string, key string, value interface{}, expectedVersion int64) error {
	s.mu.Lock()
	beadCtx, exists := s.contexts[beadID]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("context not found for bead: %s", beadID)
	}
	s.mu.Unlock()

	beadCtx.mu.Lock()
	defer beadCtx.mu.Unlock()

	// Check version for conflict detection
	if expectedVersion > 0 && beadCtx.Version != expectedVersion {
		return &ConflictError{
			BeadID:          beadID,
			ExpectedVersion: expectedVersion,
			ActualVersion:   beadCtx.Version,
		}
	}

	// Update data
	beadCtx.Data[key] = value
	beadCtx.Version++
	beadCtx.LastUpdated = time.Now()
	beadCtx.LastUpdatedBy = agentID

	// Add activity log
	beadCtx.ActivityLog = append(beadCtx.ActivityLog, ActivityEntry{
		Timestamp:    time.Now(),
		AgentID:      agentID,
		ActivityType: "updated",
		Description:  fmt.Sprintf("Agent %s updated '%s'", agentID, key),
		Data: map[string]interface{}{
			"key":   key,
			"value": value,
		},
	})

	// Notify listeners
	s.notifyUpdate(ContextUpdate{
		BeadID:     beadID,
		UpdateType: "data_changed",
		AgentID:    agentID,
		Data: map[string]interface{}{
			"key":   key,
			"value": value,
		},
		Timestamp: time.Now(),
		Version:   beadCtx.Version,
	})

	return nil
}

// AddActivity adds an activity entry to the log
func (s *ContextStore) AddActivity(ctx context.Context, beadID, agentID, activityType, description string, data map[string]interface{}) error {
	s.mu.Lock()
	beadCtx, exists := s.contexts[beadID]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("context not found for bead: %s", beadID)
	}
	s.mu.Unlock()

	beadCtx.mu.Lock()
	defer beadCtx.mu.Unlock()

	entry := ActivityEntry{
		Timestamp:    time.Now(),
		AgentID:      agentID,
		ActivityType: activityType,
		Description:  description,
		Data:         data,
	}

	beadCtx.ActivityLog = append(beadCtx.ActivityLog, entry)
	beadCtx.Version++
	beadCtx.LastUpdated = time.Now()

	// Notify listeners
	s.notifyUpdate(ContextUpdate{
		BeadID:     beadID,
		UpdateType: "activity",
		AgentID:    agentID,
		Data: map[string]interface{}{
			"activity_type": activityType,
			"description":   description,
			"data":          data,
		},
		Timestamp: time.Now(),
		Version:   beadCtx.Version,
	})

	return nil
}

// Subscribe creates a listener channel for real-time updates
func (s *ContextStore) Subscribe(beadID string) chan ContextUpdate {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()

	ch := make(chan ContextUpdate, 100)
	s.listeners[beadID] = append(s.listeners[beadID], ch)

	return ch
}

// Unsubscribe removes a listener channel
func (s *ContextStore) Unsubscribe(beadID string, ch chan ContextUpdate) {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()

	listeners := s.listeners[beadID]
	newListeners := []chan ContextUpdate{}

	for _, listener := range listeners {
		if listener != ch {
			newListeners = append(newListeners, listener)
		} else {
			close(listener)
		}
	}

	s.listeners[beadID] = newListeners
}

// notifyUpdate sends update to listeners (must be called without holding locks)
func (s *ContextStore) notifyUpdate(update ContextUpdate) {
	select {
	case s.updates <- update:
	default:
		// Update channel full, drop update
	}
}

// distributeUpdates distributes updates to subscribed listeners
func (s *ContextStore) distributeUpdates() {
	for update := range s.updates {
		s.listenerMu.RLock()
		listeners := s.listeners[update.BeadID]
		s.listenerMu.RUnlock()

		for _, ch := range listeners {
			select {
			case ch <- update:
			default:
				// Listener channel full, skip
			}
		}
	}
}

// ExportContext exports context as JSON
func (s *ContextStore) ExportContext(ctx context.Context, beadID string) ([]byte, error) {
	beadCtx, err := s.Get(ctx, beadID)
	if err != nil {
		return nil, err
	}

	beadCtx.mu.RLock()
	defer beadCtx.mu.RUnlock()

	return json.Marshal(beadCtx)
}

// Close shuts down the context store
func (s *ContextStore) Close() {
	close(s.updates)

	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()

	for _, listeners := range s.listeners {
		for _, ch := range listeners {
			close(ch)
		}
	}

	s.listeners = make(map[string][]chan ContextUpdate)
}
