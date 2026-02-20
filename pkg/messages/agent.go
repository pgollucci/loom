package messages

import "time"

// AgentCommunicationMessage represents an agent-to-agent message sent via NATS.
// This bridges the in-memory AgentMessageBus to cross-container communication.
type AgentCommunicationMessage struct {
	MessageID        string                 `json:"message_id"`
	Type             string                 `json:"type"`               // "agent_message", "broadcast", "request", "response", "notification", "consensus_request", "consensus_vote"
	FromAgentID      string                 `json:"from_agent_id"`
	ToAgentID        string                 `json:"to_agent_id,omitempty"`
	ToAgentIDs       []string               `json:"to_agent_ids,omitempty"`
	Subject          string                 `json:"subject,omitempty"`
	Body             string                 `json:"body,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
	Priority         string                 `json:"priority"`
	RequiresResponse bool                   `json:"requires_response"`
	InReplyTo        string                 `json:"in_reply_to,omitempty"`
	Context          map[string]interface{} `json:"context,omitempty"`
	Timestamp        time.Time              `json:"timestamp"`
	SourceContainer  string                 `json:"source_container,omitempty"`
}

// PlanMessage represents a Plan/Document/Act plan published via NATS
type PlanMessage struct {
	Type          string                 `json:"type"` // "plan.created", "plan.updated", "plan.completed"
	PlanID        string                 `json:"plan_id"`
	ProjectID     string                 `json:"project_id"`
	BeadID        string                 `json:"bead_id"`
	CreatedBy     string                 `json:"created_by"`
	Plan          PlanData               `json:"plan"`
	CorrelationID string                 `json:"correlation_id"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// PlanData contains the structured plan
type PlanData struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Steps       []PlanStep `json:"steps"`
	Priority    int        `json:"priority"`
}

// PlanStep describes a single step in a plan
type PlanStep struct {
	StepID      string                 `json:"step_id"`
	Role        string                 `json:"role"`    // "coder", "reviewer", "qa", "pm", "architect"
	Action      string                 `json:"action"`  // "implement", "review", "test", "plan", "document"
	Description string                 `json:"description"`
	DependsOn   []string               `json:"depends_on,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
	Status      string                 `json:"status"` // "pending", "in_progress", "completed", "failed"
}

// ReviewMessage represents a code review request via NATS
type ReviewMessage struct {
	Type          string                 `json:"type"` // "review.requested", "review.completed", "review.approved", "review.rejected"
	ProjectID     string                 `json:"project_id"`
	BeadID        string                 `json:"bead_id"`
	ReviewerID    string                 `json:"reviewer_id,omitempty"`
	Review        ReviewData             `json:"review"`
	CorrelationID string                 `json:"correlation_id"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ReviewData contains the review details
type ReviewData struct {
	Commits     []string               `json:"commits,omitempty"`
	FilesChanged []string              `json:"files_changed,omitempty"`
	Diff        string                 `json:"diff,omitempty"`
	Score       int                    `json:"score,omitempty"`       // 0-100
	Decision    string                 `json:"decision,omitempty"`    // "approve", "request_changes", "comment"
	Comments    []ReviewComment        `json:"comments,omitempty"`
	Summary     string                 `json:"summary,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

// ReviewComment is a single review comment
type ReviewComment struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"` // "critical", "warning", "info"
	Body     string `json:"body"`
}

// NewPlanCreated creates a plan.created message
func NewPlanCreated(projectID, beadID, planID, createdBy string, plan PlanData, correlationID string) *PlanMessage {
	return &PlanMessage{
		Type:          "plan.created",
		PlanID:        planID,
		ProjectID:     projectID,
		BeadID:        beadID,
		CreatedBy:     createdBy,
		Plan:          plan,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}

// NewReviewRequested creates a review.requested message
func NewReviewRequested(projectID, beadID string, review ReviewData, correlationID string) *ReviewMessage {
	return &ReviewMessage{
		Type:          "review.requested",
		ProjectID:     projectID,
		BeadID:        beadID,
		Review:        review,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}

// NewReviewCompleted creates a review.completed message
func NewReviewCompleted(projectID, beadID, reviewerID string, review ReviewData, correlationID string) *ReviewMessage {
	return &ReviewMessage{
		Type:          "review.completed",
		ProjectID:     projectID,
		BeadID:        beadID,
		ReviewerID:    reviewerID,
		Review:        review,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	}
}
