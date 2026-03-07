package meetings

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager manages meetings
type Manager struct {
	mu       sync.RWMutex
	meetings map[string]*Meeting
}

// NewManager creates a new meetings manager
func NewManager() *Manager {
	return &Manager{
		meetings: make(map[string]*Meeting),
	}
}

// CreateMeeting creates a new meeting
func (m *Manager) CreateMeeting(req *CreateMeetingRequest) (*Meeting, error) {
	if req == nil {
		return nil, fmt.Errorf("create meeting request cannot be nil")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("meeting title is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meeting := &Meeting{
		ID:           uuid.New().String(),
		Title:        req.Title,
		Status:       MeetingStatusScheduled,
		Participants: req.Participants,
		AgendaItems:  req.AgendaItems,
		Transcript:   []TranscriptEntry{},
		ActionItems:  []ActionItem{},
		CreatedAt:    time.Now().UTC(),
	}

	m.meetings[meeting.ID] = meeting
	return meeting, nil
}

// GetMeeting retrieves a meeting by ID
func (m *Manager) GetMeeting(id string) (*Meeting, error) {
	if id == "" {
		return nil, fmt.Errorf("meeting id is required")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	meeting, ok := m.meetings[id]
	if !ok {
		return nil, fmt.Errorf("meeting not found: %s", id)
	}

	return meeting, nil
}

// ListMeetings lists recent meetings (up to limit)
func (m *Manager) ListMeetings(limit int) ([]*Meeting, error) {
	if limit <= 0 {
		limit = 50 // Default to 50
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var meetings []*Meeting
	for _, meeting := range m.meetings {
		meetings = append(meetings, meeting)
	}

	// Sort by created_at descending (most recent first)
	for i := 0; i < len(meetings)-1; i++ {
		for j := i + 1; j < len(meetings); j++ {
			if meetings[j].CreatedAt.After(meetings[i].CreatedAt) {
				meetings[i], meetings[j] = meetings[j], meetings[i]
			}
		}
	}

	// Limit results
	if len(meetings) > limit {
		meetings = meetings[:limit]
	}

	return meetings, nil
}

// ListActiveMeetings lists meetings currently in progress
func (m *Manager) ListActiveMeetings() ([]*Meeting, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []*Meeting
	for _, meeting := range m.meetings {
		if meeting.Status == MeetingStatusInProgress {
			active = append(active, meeting)
		}
	}

	return active, nil
}

// UpdateMeeting updates a meeting
func (m *Manager) UpdateMeeting(id string, req *UpdateMeetingRequest) (*Meeting, error) {
	if id == "" {
		return nil, fmt.Errorf("meeting id is required")
	}
	if req == nil {
		return nil, fmt.Errorf("update meeting request cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meeting, ok := m.meetings[id]
	if !ok {
		return nil, fmt.Errorf("meeting not found: %s", id)
	}

	if req.Title != nil {
		meeting.Title = *req.Title
	}
	if req.Status != nil {
		meeting.Status = *req.Status
		if *req.Status == MeetingStatusCompleted && meeting.CompletedAt == nil {
			now := time.Now().UTC()
			meeting.CompletedAt = &now
		}
	}
	if req.Summary != nil {
		meeting.Summary = *req.Summary
	}
	if len(req.AgendaItems) > 0 {
		meeting.AgendaItems = req.AgendaItems
	}

	return meeting, nil
}

// AddTranscriptEntry adds a transcript entry to a meeting
func (m *Manager) AddTranscriptEntry(meetingID string, req *AddTranscriptEntryRequest) (*TranscriptEntry, error) {
	if meetingID == "" {
		return nil, fmt.Errorf("meeting id is required")
	}
	if req == nil {
		return nil, fmt.Errorf("add transcript entry request cannot be nil")
	}
	if req.Speaker == "" {
		return nil, fmt.Errorf("speaker is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meeting, ok := m.meetings[meetingID]
	if !ok {
		return nil, fmt.Errorf("meeting not found: %s", meetingID)
	}

	entry := TranscriptEntry{
		ID:        uuid.New().String(),
		Speaker:   req.Speaker,
		Content:   req.Content,
		Timestamp: time.Now().UTC(),
	}

	meeting.Transcript = append(meeting.Transcript, entry)
	return &entry, nil
}

// AddActionItem adds an action item to a meeting
func (m *Manager) AddActionItem(meetingID string, req *AddActionItemRequest) (*ActionItem, error) {
	if meetingID == "" {
		return nil, fmt.Errorf("meeting id is required")
	}
	if req == nil {
		return nil, fmt.Errorf("add action item request cannot be nil")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("action item title is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meeting, ok := m.meetings[meetingID]
	if !ok {
		return nil, fmt.Errorf("meeting not found: %s", meetingID)
	}

	item := ActionItem{
		ID:        uuid.New().String(),
		Title:     req.Title,
		Owner:     req.Owner,
		DueDate:   req.DueDate,
		Status:    "open",
		CreatedAt: time.Now().UTC(),
	}

	meeting.ActionItems = append(meeting.ActionItems, item)
	return &item, nil
}

// StartMeeting transitions a meeting to in_progress status
func (m *Manager) StartMeeting(id string) (*Meeting, error) {
	if id == "" {
		return nil, fmt.Errorf("meeting id is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meeting, ok := m.meetings[id]
	if !ok {
		return nil, fmt.Errorf("meeting not found: %s", id)
	}

	meeting.Status = MeetingStatusInProgress
	return meeting, nil
}

// CompleteMeeting transitions a meeting to completed status
func (m *Manager) CompleteMeeting(id string, summary string) (*Meeting, error) {
	if id == "" {
		return nil, fmt.Errorf("meeting id is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meeting, ok := m.meetings[id]
	if !ok {
		return nil, fmt.Errorf("meeting not found: %s", id)
	}

	meeting.Status = MeetingStatusCompleted
	meeting.Summary = summary
	now := time.Now().UTC()
	meeting.CompletedAt = &now

	return meeting, nil
}

// CallMeeting creates a meeting from the given parameters, satisfying the actions.MeetingCaller interface.
func (m *Manager) CallMeeting(_ context.Context, _, title, _, _ string, participants []string, agenda []struct{ Topic, Description string }) (string, error) {
	agendaItems := make([]AgendaItem, len(agenda))
	for i, a := range agenda {
		agendaItems[i] = AgendaItem{Title: a.Topic, Description: a.Description}
	}
	ps := make([]Participant, len(participants))
	for i, p := range participants {
		ps[i] = Participant{ID: p, Role: "participant"}
	}
	meeting, err := m.CreateMeeting(&CreateMeetingRequest{
		Title:        title,
		Participants: ps,
		AgendaItems:  agendaItems,
	})
	if err != nil {
		return "", err
	}
	return meeting.ID, nil
}
