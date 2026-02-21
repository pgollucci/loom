package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/pkg/messages"
)

// Helper functions for converting map[string]interface{} to TaskRequest
func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getMapFromMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if subMap, ok := v.(map[string]interface{}); ok {
			return subMap
		}
	}
	return nil
}

// MessageBus defines the interface for publishing task messages to NATS
type MessageBus interface {
	PublishTask(ctx context.Context, projectID string, task *messages.TaskMessage) error
}

// ProjectAgentClient communicates with project agent containers
type ProjectAgentClient struct {
	baseURL    string
	projectID  string
	roles      []string // roles running in this container (e.g. ["coder","reviewer","qa","pm","architect"])
	httpClient *http.Client
	messageBus MessageBus // NATS message bus for async task publishing
}

// TaskRequest represents a task to send to project agent
type TaskRequest struct {
	TaskID    string                 `json:"task_id"`
	BeadID    string                 `json:"bead_id"`
	Action    string                 `json:"action"`
	ProjectID string                 `json:"project_id"`
	Params    map[string]interface{} `json:"params"`
}

// TaskResult represents result from project agent
type TaskResult struct {
	TaskID   string                 `json:"task_id"`
	BeadID   string                 `json:"bead_id"`
	Success  bool                   `json:"success"`
	Output   string                 `json:"output"`
	Error    string                 `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// AgentStatus represents project agent status
type AgentStatus struct {
	ProjectID   string                 `json:"project_id"`
	WorkDir     string                 `json:"work_dir"`
	Busy        bool                   `json:"busy"`
	CurrentTask map[string]interface{} `json:"current_task,omitempty"`
}

// NewProjectAgentClient creates a new project agent client
func NewProjectAgentClient(baseURL, projectID string) *ProjectAgentClient {
	return &ProjectAgentClient{
		baseURL:   baseURL,
		projectID: projectID,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetMessageBus sets the NATS message bus for async task publishing
func (c *ProjectAgentClient) SetMessageBus(mb MessageBus) {
	c.messageBus = mb
}

// SetRoles records which agent roles are running inside this container.
func (c *ProjectAgentClient) SetRoles(roles []string) {
	c.roles = roles
}

// Roles returns the agent roles running inside this container.
func (c *ProjectAgentClient) Roles() []string {
	return c.roles
}

// Health checks if the project agent is healthy
func (c *ProjectAgentClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy status: %d", resp.StatusCode)
	}

	return nil
}

// Status returns the current status of the project agent
func (c *ProjectAgentClient) Status(ctx context.Context) (*AgentStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status request failed: %d - %s", resp.StatusCode, body)
	}

	var status AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}

	return &status, nil
}

// ExecuteTask sends a task to the project agent for execution
// Prefers NATS for async task publishing, falls back to HTTP if NATS unavailable
func (c *ProjectAgentClient) ExecuteTask(ctx context.Context, req interface{}) error {
	// Convert interface{} to TaskRequest if needed
	var taskReq *TaskRequest
	switch r := req.(type) {
	case *TaskRequest:
		taskReq = r
	case map[string]interface{}:
		// Convert map to TaskRequest
		taskReq = &TaskRequest{
			TaskID:    getStringFromMap(r, "task_id"),
			BeadID:    getStringFromMap(r, "bead_id"),
			Action:    getStringFromMap(r, "action"),
			ProjectID: getStringFromMap(r, "project_id"),
			Params:    getMapFromMap(r, "params"),
		}
	default:
		return fmt.Errorf("unsupported request type: %T", req)
	}

	// Ensure project ID matches
	taskReq.ProjectID = c.projectID

	// Try NATS first if message bus is available
	if c.messageBus != nil {
		log.Printf("[ProjectAgentClient] Publishing task %s to NATS for project %s", taskReq.TaskID, taskReq.ProjectID)

		// Convert TaskRequest to TaskMessage for NATS
		taskMsg := messages.TaskAssigned(
			taskReq.ProjectID,
			taskReq.BeadID,
			c.projectID, // Use project ID as agent ID for container agents
			messages.TaskData{
				Title:       fmt.Sprintf("Task %s", taskReq.TaskID),
				Description: fmt.Sprintf("Execute %s action", taskReq.Action),
				Type:        "task",
				Priority:    1,
				WorkDir:     getStringFromMap(taskReq.Params, "work_dir"),
			},
			uuid.New().String(), // Generate correlation ID
		)

		// Copy task parameters into TaskData metadata
		if taskReq.Params != nil {
			// Store original task request in task data for agent to access
			taskMsg.TaskData.Type = taskReq.Action // Use action as task type
		}

		if err := c.messageBus.PublishTask(ctx, taskReq.ProjectID, taskMsg); err != nil {
			log.Printf("[ProjectAgentClient] NATS publish failed: %v, falling back to HTTP", err)
			// Fall through to HTTP fallback
		} else {
			log.Printf("[ProjectAgentClient] Task %s published successfully via NATS", taskReq.TaskID)
			return nil
		}
	} else {
		log.Printf("[ProjectAgentClient] No message bus available, using HTTP for task %s", taskReq.TaskID)
	}

	// HTTP fallback (original implementation)
	body, err := json.Marshal(taskReq)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/task", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("task submission failed: %d - %s", resp.StatusCode, respBody)
	}

	log.Printf("[ProjectAgentClient] Task %s submitted via HTTP fallback", taskReq.TaskID)
	return nil
}

// ExecSync executes a command synchronously in the container via the /exec endpoint
// and returns stdout, stderr, exit code, and duration directly.
func (c *ProjectAgentClient) ExecSync(ctx context.Context, command, workingDir string, timeout int) (*ExecResult, error) {
	payload := map[string]interface{}{
		"command":     command,
		"working_dir": workingDir,
		"timeout":     timeout,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/exec", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Use a longer timeout for the HTTP client since the command may take a while
	client := &http.Client{Timeout: time.Duration(timeout+30) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("exec request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("exec failed with status %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
		ExitCode   int    `json:"exit_code"`
		DurationMs int64  `json:"duration_ms"`
		Success    bool   `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode exec response: %w", err)
	}

	return &ExecResult{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
	}, nil
}

// ExecuteTaskSync sends a task and waits for the result (blocking)
// This is a convenience method - in production, use ExecuteTask + result webhook
func (c *ProjectAgentClient) ExecuteTaskSync(ctx context.Context, req *TaskRequest, timeout time.Duration) (*TaskResult, error) {
	// Send task
	if err := c.ExecuteTask(ctx, req); err != nil {
		return nil, err
	}

	// Poll for completion (simplified - in production use webhooks)
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("task execution timeout after %v", timeout)
			}

			// Poll the agent's /results/{taskID} endpoint for the completed result.
			result, err := c.fetchResult(ctx, req.TaskID)
			if err == nil && result != nil {
				return result, nil
			}
			// 404 means still in progress; any other error is transient — keep polling.
		}
	}
}

// fetchResult retrieves a completed task result from the agent's /results/{taskID} endpoint.
// Returns (nil, nil) if the result is not yet available (404).
func (c *ProjectAgentClient) fetchResult(ctx context.Context, taskID string) (*TaskResult, error) {
	url := fmt.Sprintf("%s/results/%s", c.baseURL, taskID)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(reqHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // still in progress
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from /results/%s", resp.StatusCode, taskID)
	}

	var result TaskResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding result: %w", err)
	}
	return &result, nil
}
