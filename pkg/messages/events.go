package messages

import "time"

// EventMessage represents a system event message sent via NATS
type EventMessage struct {
	Type          string                 `json:"type"`   // "bead.created", "agent.started", "dispatch.cycle", etc.
	Source        string                 `json:"source"` // Service that generated the event
	ProjectID     string                 `json:"project_id,omitempty"`
	EntityID      string                 `json:"entity_id,omitempty"` // Bead ID, Agent ID, etc.
	Event         EventData              `json:"event"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// EventData contains the event-specific information
type EventData struct {
	Action      string                 `json:"action"`   // "created", "updated", "deleted", "started", "stopped"
	Category    string                 `json:"category"` // "bead", "agent", "dispatch", "system"
	Description string                 `json:"description,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
}

// BeadCreated creates a bead.created event
func BeadCreated(projectID, beadID, source string) *EventMessage {
	return &EventMessage{
		Type:      "bead.created",
		Source:    source,
		ProjectID: projectID,
		EntityID:  beadID,
		Event: EventData{
			Action:   "created",
			Category: "bead",
		},
		Timestamp: time.Now(),
	}
}

// BeadUpdated creates a bead.updated event
func BeadUpdated(projectID, beadID, source string, data map[string]interface{}) *EventMessage {
	return &EventMessage{
		Type:      "bead.updated",
		Source:    source,
		ProjectID: projectID,
		EntityID:  beadID,
		Event: EventData{
			Action:   "updated",
			Category: "bead",
			Data:     data,
		},
		Timestamp: time.Now(),
	}
}

// AgentStarted creates an agent.started event
func AgentStarted(agentID, source string) *EventMessage {
	return &EventMessage{
		Type:     "agent.started",
		Source:   source,
		EntityID: agentID,
		Event: EventData{
			Action:   "started",
			Category: "agent",
		},
		Timestamp: time.Now(),
	}
}

// DispatchCycle creates a dispatch.cycle event
func DispatchCycle(projectID, source string, data map[string]interface{}) *EventMessage {
	return &EventMessage{
		Type:      "dispatch.cycle",
		Source:    source,
		ProjectID: projectID,
		Event: EventData{
			Action:   "cycle",
			Category: "dispatch",
			Data:     data,
		},
		Timestamp: time.Now(),
	}
}

// SystemError creates a system.error event
func SystemError(source, description string, data map[string]interface{}) *EventMessage {
	return &EventMessage{
		Type:   "system.error",
		Source: source,
		Event: EventData{
			Action:      "error",
			Category:    "system",
			Description: description,
			Data:        data,
		},
		Timestamp: time.Now(),
	}
}
