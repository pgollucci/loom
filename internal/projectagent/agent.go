package projectagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/pkg/models"
)

// Config holds the configuration for a project agent
type Config struct {
	ProjectID         string
	ControlPlaneURL   string
	WorkDir           string
	HeartbeatInterval time.Duration
}

// Agent is a lightweight agent that runs inside a project container
type Agent struct {
	config       Config
	httpClient   *http.Client
	currentTask  *TaskExecution
	taskResultCh chan *TaskResult
}

// TaskRequest represents a task sent from the control plane
type TaskRequest struct {
	TaskID    string                 `json:"task_id"`
	BeadID    string                 `json:"bead_id"`
	Action    string                 `json:"action"`
	ProjectID string                 `json:"project_id"`
	Params    map[string]interface{} `json:"params"`
}

// TaskResult represents the result of executing a task
type TaskResult struct {
	TaskID   string                 `json:"task_id"`
	BeadID   string                 `json:"bead_id"`
	Success  bool                   `json:"success"`
	Output   string                 `json:"output"`
	Error    string                 `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TaskExecution tracks the currently executing task
type TaskExecution struct {
	Request   *TaskRequest
	StartTime time.Time
	Context   context.Context
	Cancel    context.CancelFunc
}

// New creates a new project agent
func New(config Config) (*Agent, error) {
	if config.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	if config.ControlPlaneURL == "" {
		return nil, fmt.Errorf("control_plane_url is required")
	}

	if config.WorkDir == "" {
		config.WorkDir = "/workspace"
	}

	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}

	return &Agent{
		config: config,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		taskResultCh: make(chan *TaskResult, 10),
	}, nil
}

// Start begins the agent's background tasks (heartbeat, result reporter)
func (a *Agent) Start(ctx context.Context) error {
	// Send initial registration
	if err := a.register(ctx); err != nil {
		log.Printf("Warning: Failed to register with control plane: %v", err)
	}

	// Start heartbeat ticker
	heartbeatTicker := time.NewTicker(a.config.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Start result reporter
	go a.resultReporter(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-heartbeatTicker.C:
			if err := a.sendHeartbeat(ctx); err != nil {
				log.Printf("Heartbeat error: %v", err)
			}
		}
	}
}

// RegisterHandlers registers HTTP handlers for task reception
func (a *Agent) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/task", a.handleTask)
	mux.HandleFunc("/status", a.handleStatus)
}

// handleHealth returns agent health status
func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"project_id": a.config.ProjectID,
		"work_dir":   a.config.WorkDir,
	})
}

// handleTask receives and executes a task from the control plane
func (a *Agent) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate project ID matches
	if req.ProjectID != a.config.ProjectID {
		http.Error(w, "Project ID mismatch", http.StatusBadRequest)
		return
	}

	log.Printf("Received task: %s (bead: %s, action: %s)", req.TaskID, req.BeadID, req.Action)

	// Execute task asynchronously
	go a.executeTask(&req)

	// Return accepted status immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"task_id": req.TaskID,
	})
}

// handleStatus returns current task execution status
func (a *Agent) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := map[string]interface{}{
		"project_id": a.config.ProjectID,
		"work_dir":   a.config.WorkDir,
		"busy":       a.currentTask != nil,
	}

	if a.currentTask != nil {
		status["current_task"] = map[string]interface{}{
			"task_id":  a.currentTask.Request.TaskID,
			"bead_id":  a.currentTask.Request.BeadID,
			"action":   a.currentTask.Request.Action,
			"duration": time.Since(a.currentTask.StartTime).String(),
		}
	}

	json.NewEncoder(w).Encode(status)
}

// executeTask executes a task in the project's working directory
func (a *Agent) executeTask(req *TaskRequest) {
	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	a.currentTask = &TaskExecution{
		Request:   req,
		StartTime: startTime,
		Context:   ctx,
		Cancel:    cancel,
	}
	defer func() { a.currentTask = nil }()

	result := &TaskResult{
		TaskID: req.TaskID,
		BeadID: req.BeadID,
	}

	// Execute action based on type
	var output string
	var err error

	switch req.Action {
	case "bash":
		output, err = a.executeBash(ctx, req.Params)
	case "git_commit":
		output, err = a.executeGitCommit(ctx, req.Params)
	case "git_push":
		output, err = a.executeGitPush(ctx, req.Params)
	case "read":
		output, err = a.executeRead(ctx, req.Params)
	case "write":
		output, err = a.executeWrite(ctx, req.Params)
	case "scope":
		output, err = a.executeScope(ctx, req.Params)
	default:
		err = fmt.Errorf("unsupported action: %s", req.Action)
	}

	result.Duration = time.Since(startTime)
	result.Success = (err == nil)
	result.Output = output

	if err != nil {
		result.Error = err.Error()
		log.Printf("Task %s failed: %v", req.TaskID, err)
	} else {
		log.Printf("Task %s completed successfully in %v", req.TaskID, result.Duration)
	}

	// Send result to control plane
	a.taskResultCh <- result
}

// executeBash executes a bash command in the work directory
func (a *Agent) executeBash(ctx context.Context, params map[string]interface{}) (string, error) {
	command, ok := params["command"].(string)
	if !ok {
		return "", fmt.Errorf("command parameter required")
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = a.config.WorkDir

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// executeGitCommit creates a git commit
func (a *Agent) executeGitCommit(ctx context.Context, params map[string]interface{}) (string, error) {
	message, ok := params["message"].(string)
	if !ok {
		return "", fmt.Errorf("message parameter required")
	}

	// Git add
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = a.config.WorkDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return string(output), fmt.Errorf("git add failed: %w", err)
	}

	// Git commit
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	commitCmd.Dir = a.config.WorkDir
	output, err := commitCmd.CombinedOutput()
	return string(output), err
}

// executeGitPush pushes commits to remote
func (a *Agent) executeGitPush(ctx context.Context, params map[string]interface{}) (string, error) {
	pushCmd := exec.CommandContext(ctx, "git", "push")
	pushCmd.Dir = a.config.WorkDir
	output, err := pushCmd.CombinedOutput()
	return string(output), err
}

// executeRead reads a file from the project
func (a *Agent) executeRead(ctx context.Context, params map[string]interface{}) (string, error) {
	path, ok := params["path"].(string)
	if !ok {
		return "", fmt.Errorf("path parameter required")
	}

	fullPath := filepath.Join(a.config.WorkDir, path)
	cmd := exec.CommandContext(ctx, "cat", fullPath)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// executeWrite writes content to a file
func (a *Agent) executeWrite(ctx context.Context, params map[string]interface{}) (string, error) {
	path, ok := params["path"].(string)
	if !ok {
		return "", fmt.Errorf("path parameter required")
	}

	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("content parameter required")
	}

	fullPath := filepath.Join(a.config.WorkDir, path)
	cmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("cat > %s", fullPath))
	cmd.Stdin = strings.NewReader(content)
	cmd.Dir = a.config.WorkDir

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// executeScope lists files in the project directory
func (a *Agent) executeScope(ctx context.Context, params map[string]interface{}) (string, error) {
	path := "."
	if p, ok := params["path"].(string); ok {
		path = p
	}

	fullPath := filepath.Join(a.config.WorkDir, path)
	cmd := exec.CommandContext(ctx, "ls", "-la", fullPath)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// register announces the agent to the control plane
func (a *Agent) register(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/project-agents/register", a.config.ControlPlaneURL)

	payload := map[string]interface{}{
		"project_id": a.config.ProjectID,
		"work_dir":   a.config.WorkDir,
		"agent_url":  fmt.Sprintf("http://%s:8090", a.config.ProjectID), // Container name as hostname
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	log.Printf("Successfully registered with control plane")
	return nil
}

// sendHeartbeat sends periodic heartbeat to control plane
func (a *Agent) sendHeartbeat(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/project-agents/%s/heartbeat", a.config.ControlPlaneURL, a.config.ProjectID)

	payload := map[string]interface{}{
		"project_id": a.config.ProjectID,
		"busy":       a.currentTask != nil,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("heartbeat failed with status %d", resp.StatusCode)
	}

	return nil
}

// resultReporter sends task results back to control plane
func (a *Agent) resultReporter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-a.taskResultCh:
			if err := a.sendResult(ctx, result); err != nil {
				log.Printf("Failed to send result for task %s: %v", result.TaskID, err)
				// TODO: Retry logic
			}
		}
	}
}

// sendResult sends a task result to the control plane
func (a *Agent) sendResult(ctx context.Context, result *TaskResult) error {
	url := fmt.Sprintf("%s/api/v1/project-agents/%s/results", a.config.ControlPlaneURL, a.config.ProjectID)

	body, err := json.Marshal(result)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("result submission failed with status %d", resp.StatusCode)
	}

	log.Printf("Successfully sent result for task %s", result.TaskID)
	return nil
}
