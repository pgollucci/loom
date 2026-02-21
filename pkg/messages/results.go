package messages

import "time"

// ResultMessage represents a task result message sent via NATS
type ResultMessage struct {
	Type          string                 `json:"type"` // "task.completed", "task.failed", "task.progress"
	ProjectID     string                 `json:"project_id"`
	BeadID        string                 `json:"bead_id"`
	AgentID       string                 `json:"agent_id"`
	Result        ResultData             `json:"result"`
	CorrelationID string                 `json:"correlation_id"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ResultData contains the task execution result
type ResultData struct {
	Status     string                 `json:"status"` // "success", "failure", "in_progress"
	Output     string                 `json:"output"` // Agent output/response
	Error      string                 `json:"error,omitempty"`
	Commits    []string               `json:"commits,omitempty"`     // Git commit hashes
	Artifacts  []string               `json:"artifacts,omitempty"`   // Paths to artifacts
	Duration   int64                  `json:"duration,omitempty"`    // Execution time in ms
	NextAction string                 `json:"next_action,omitempty"` // "redispatch", "escalate", "close"
	Context    map[string]interface{} `json:"context,omitempty"`
}

// TaskCompleted creates a task.completed message
func TaskCompleted(projectID, beadID, agentID string, result ResultData, correlationID string) *ResultMessage {
	return &ResultMessage{
		Type:          "task.completed",
		ProjectID:     projectID,
		BeadID:        beadID,
		AgentID:       agentID,
		Result:        result,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}

// TaskFailed creates a task.failed message
func TaskFailed(projectID, beadID, agentID string, result ResultData, correlationID string) *ResultMessage {
	return &ResultMessage{
		Type:          "task.failed",
		ProjectID:     projectID,
		BeadID:        beadID,
		AgentID:       agentID,
		Result:        result,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}

// TaskProgress creates a task.progress message
func TaskProgress(projectID, beadID, agentID string, result ResultData, correlationID string) *ResultMessage {
	return &ResultMessage{
		Type:          "task.progress",
		ProjectID:     projectID,
		BeadID:        beadID,
		AgentID:       agentID,
		Result:        result,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}
