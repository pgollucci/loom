package messages

import "time"

// TaskMessage represents a task assignment message sent via NATS
type TaskMessage struct {
	Type          string                 `json:"type"` // "task.assigned", "task.updated", "task.cancelled"
	ProjectID     string                 `json:"project_id"`
	BeadID        string                 `json:"bead_id"`
	AssignedTo    string                 `json:"assigned_to"` // Agent ID
	TaskData      TaskData               `json:"task_data"`
	CorrelationID string                 `json:"correlation_id"` // For request tracking
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// TaskData contains the actual task information
type TaskData struct {
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	Priority      int                    `json:"priority"`
	Type          string                 `json:"type"` // "task", "bug", "feature", etc.
	Context       map[string]interface{} `json:"context,omitempty"`
	WorkDir       string                 `json:"work_dir,omitempty"`       // Project working directory
	MemoryContext string                 `json:"memory_context,omitempty"` // Pre-built markdown summary from MemoryManager
}

// TaskAssigned creates a task.assigned message
func TaskAssigned(projectID, beadID, agentID string, taskData TaskData, correlationID string) *TaskMessage {
	return &TaskMessage{
		Type:          "task.assigned",
		ProjectID:     projectID,
		BeadID:        beadID,
		AssignedTo:    agentID,
		TaskData:      taskData,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}

// TaskUpdated creates a task.updated message
func TaskUpdated(projectID, beadID, agentID string, taskData TaskData, correlationID string) *TaskMessage {
	return &TaskMessage{
		Type:          "task.updated",
		ProjectID:     projectID,
		BeadID:        beadID,
		AssignedTo:    agentID,
		TaskData:      taskData,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}

// TaskCancelled creates a task.cancelled message
func TaskCancelled(projectID, beadID, agentID string, correlationID string) *TaskMessage {
	return &TaskMessage{
		Type:          "task.cancelled",
		ProjectID:     projectID,
		BeadID:        beadID,
		AssignedTo:    agentID,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}
