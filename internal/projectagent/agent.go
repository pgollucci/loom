package projectagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/internal/messagebus"
	"github.com/jordanhubbard/loom/internal/swarm"
	"github.com/jordanhubbard/loom/pkg/messages"
)

// Config holds the configuration for a project agent
type Config struct {
	ProjectID         string
	ControlPlaneURL   string
	WorkDir           string
	HeartbeatInterval time.Duration
	NatsURL           string // NATS server URL (optional, for NATS-based communication)

	// Service identity for swarm registration
	ServiceID  string // e.g. "agent-loom" (injected via SERVICE_ID env var)
	InstanceID string // e.g. "agent-loom-abc123" (injected via INSTANCE_ID env var)

	// Role-based agent service configuration
	Role              string // "coder", "reviewer", "qa", "pm", "architect"
	ProviderEndpoint  string // LLM provider endpoint (e.g., "http://llm:8000/v1")
	ProviderModel     string // LLM model to use
	ProviderAPIKey    string // API key for the provider
	PersonaPath       string // Path to persona instructions file
	ActionLoopEnabled bool   // Whether to use multi-turn action loop
	MaxLoopIterations int    // Max action loop iterations (default: 20)
}

// Agent is a full-featured agent service that runs inside a project container.
// It supports LLM-driven action loops, role-based behavior, and NATS communication.
type Agent struct {
	config       Config
	httpClient   *http.Client
	currentTask  *TaskExecution
	taskResultCh chan *TaskResult
	messageBus   *messagebus.NatsMessageBus
	swarmMgr     *swarm.Manager // announces this agent to the control plane via NATS swarm
	resultStore  sync.Map       // taskID -> *TaskResult, for /results/{taskID} polling

	role                string // cached from config
	personaInstructions string
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

	if config.MaxLoopIterations <= 0 {
		config.MaxLoopIterations = 20
	}

	agent := &Agent{
		config: config,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		taskResultCh: make(chan *TaskResult, 10),
		role:         config.Role,
	}

	// Load persona instructions from file if specified
	if config.PersonaPath != "" {
		data, err := readFileContent(config.PersonaPath)
		if err != nil {
			log.Printf("Warning: Failed to load persona from %s: %v", config.PersonaPath, err)
		} else {
			agent.personaInstructions = data
			log.Printf("Loaded persona instructions from %s (%d bytes)", config.PersonaPath, len(data))
		}
	}

	// Initialize NATS if URL is provided
	if config.NatsURL != "" {
		mb, err := messagebus.NewNatsMessageBus(messagebus.Config{
			URL:        config.NatsURL,
			StreamName: "LOOM",
			Timeout:    10 * time.Second,
		})
		if err != nil {
			log.Printf("Warning: Failed to connect to NATS at %s: %v", config.NatsURL, err)
			log.Printf("Agent will use HTTP-only communication")
		} else {
			agent.messageBus = mb
			log.Printf("Connected to NATS message bus at %s", config.NatsURL)
		}
	}

	if config.Role != "" {
		log.Printf("Agent role: %s", config.Role)
	}
	if config.ProviderEndpoint != "" {
		log.Printf("LLM provider: %s (model: %s)", config.ProviderEndpoint, config.ProviderModel)
	}

	return agent, nil
}

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Start begins the agent's background tasks (heartbeat, result reporter, NATS subscription)
func (a *Agent) Start(ctx context.Context) error {
	// Register with control plane, retrying until successful or context cancelled.
	// The control plane (loom) may still be initializing when the container starts,
	// so we retry with backoff rather than giving up on the first attempt.
	go func() {
		backoff := 2 * time.Second
		const maxBackoff = 30 * time.Second
		for {
			if err := a.register(ctx); err == nil {
				return
			} else {
				log.Printf("Warning: Failed to register with control plane: %v (retrying in %s)", err, backoff)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		}
	}()

	// Subscribe to NATS tasks if message bus is available
	if a.messageBus != nil {
		if err := a.subscribeToTasks(); err != nil {
			log.Printf("Warning: Failed to subscribe to NATS tasks: %v", err)
		} else {
			log.Printf("Subscribed to NATS tasks for project %s", a.config.ProjectID)
		}

		// Announce this agent via the swarm protocol so the control plane can
		// discover it dynamically without relying solely on HTTP registration.
		serviceID := a.config.ServiceID
		if serviceID == "" {
			serviceID = fmt.Sprintf("agent-%s", a.config.ProjectID)
		}
		instanceID := a.config.InstanceID
		if instanceID == "" {
			instanceID = serviceID
		}
		serviceType := "agent"
		if a.config.Role != "" {
			serviceType = "agent-" + a.config.Role
		}
		agentURL := fmt.Sprintf("http://loom-project-%s:8090", a.config.ProjectID)
		a.swarmMgr = swarm.NewManager(a.messageBus, serviceID, serviceType)
		// Override the instance ID to use our injected value.
		if err := a.swarmMgr.StartWithInstanceID(ctx, instanceID, []string{a.config.Role}, []string{a.config.ProjectID}, agentURL); err != nil {
			log.Printf("Warning: Failed to start swarm manager: %v", err)
		} else {
			log.Printf("[Agent] Announced to swarm as %s (instance=%s)", serviceID, instanceID)
		}
	}

	// Start heartbeat ticker
	heartbeatTicker := time.NewTicker(a.config.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Start HTTP result reporter only if NATS is not available
	// When NATS is available, results are published via NATS in executeTaskWithNats()
	if a.messageBus == nil {
		log.Printf("NATS unavailable - using HTTP for result reporting")
		go a.resultReporter(ctx)
	} else {
		log.Printf("NATS available - using message bus for result reporting")
	}

	for {
		select {
		case <-ctx.Done():
			// Clean up NATS connection
			if a.messageBus != nil {
				a.messageBus.Close()
			}
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
	mux.HandleFunc("/exec", a.handleExec)
	mux.HandleFunc("/status", a.handleStatus)
	mux.HandleFunc("/results/", a.handleResults)
	mux.HandleFunc("/files/write", a.handleFileWrite)
	mux.HandleFunc("/files/read", a.handleFileRead)
	mux.HandleFunc("/files/tree", a.handleFileTree)
	mux.HandleFunc("/files/search", a.handleFileSearch)
	mux.HandleFunc("/git/commit", a.handleGitCommit)
	mux.HandleFunc("/git/push", a.handleGitPush)
	mux.HandleFunc("/git/status", a.handleGitStatus)
	mux.HandleFunc("/git/diff", a.handleGitDiff)
}

// handleResults returns the result of a completed task by task ID.
// GET /results/{taskID}
func (a *Agent) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/results/")
	if taskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}
	val, ok := a.resultStore.Load(taskID)
	if !ok {
		http.Error(w, "result not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(val)
}

// handleExec executes a command synchronously and returns stdout/stderr/exit-code inline.
func (a *Agent) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Command    string `json:"command"`
		WorkingDir string `json:"working_dir"`
		Timeout    int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
	defer cancel()

	workDir := req.WorkingDir
	if workDir == "" {
		workDir = a.config.WorkDir
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", req.Command)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	runErr := cmd.Run()
	durationMs := time.Since(startTime).Milliseconds()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	log.Printf("[exec] command=%q exit=%d dur=%dms", req.Command, exitCode, durationMs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"stdout":      stdout.String(),
		"stderr":      stderr.String(),
		"exit_code":   exitCode,
		"duration_ms": durationMs,
		"success":     exitCode == 0,
	})
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

	// When the action loop is enabled and a provider is configured,
	// delegate to the multi-turn LLM loop instead of simple action dispatch.
	var output string
	var err error

	if a.config.ActionLoopEnabled && a.config.ProviderEndpoint != "" && req.Action == "action_loop" {
		title, _ := req.Params["title"].(string)
		desc, _ := req.Params["description"].(string)
		if title == "" {
			title = req.BeadID
		}
		output, err = a.RunActionLoop(ctx, title, desc, ActionLoopConfig{
			MaxIterations:       a.config.MaxLoopIterations,
			ProviderEndpoint:    a.config.ProviderEndpoint,
			ProviderModel:       a.config.ProviderModel,
			ProviderAPIKey:      a.config.ProviderAPIKey,
			PersonaInstructions: a.personaInstructions,
		})
	} else {
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

	// Store result for /results/{taskID} polling by the control plane.
	a.resultStore.Store(req.TaskID, result)

	// Send result - prefer NATS if available, fallback to HTTP
	if a.messageBus != nil {
		// Publish result to NATS
		var resultMsg *messages.ResultMessage
		if err != nil {
			resultMsg = messages.TaskFailed(
				req.ProjectID,
				req.BeadID,
				a.config.ProjectID,
				messages.ResultData{
					Status:   "failure",
					Output:   output,
					Error:    err.Error(),
					Duration: result.Duration.Milliseconds(),
				},
				req.TaskID, // Use task ID as correlation ID
			)
		} else {
			resultMsg = messages.TaskCompleted(
				req.ProjectID,
				req.BeadID,
				a.config.ProjectID,
				messages.ResultData{
					Status:   "success",
					Output:   output,
					Duration: result.Duration.Milliseconds(),
				},
				req.TaskID,
			)
		}

		if publishErr := a.messageBus.PublishResult(context.Background(), req.ProjectID, resultMsg); publishErr != nil {
			log.Printf("Failed to publish result to NATS: %v, falling back to HTTP", publishErr)
			a.taskResultCh <- result // Fallback to HTTP
		} else {
			log.Printf("Published result to NATS for task %s", req.TaskID)
		}
	} else {
		// Send result via HTTP (NATS not available)
		a.taskResultCh <- result
	}
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

// ensureWorkspaceReady performs minimal workspace sanity checks before
// executing a task. It verifies git is configured and the workspace has a
// .git directory. This runs inside the container so direct exec is fine.
func (a *Agent) ensureWorkspaceReady() {
	gitDir := filepath.Join(a.config.WorkDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		log.Printf("[Agent] Workspace %s has no .git - task may fail", a.config.WorkDir)
	}
	// Ensure git safe.directory is set
	cmd := exec.Command("git", "config", "--global", "--get-all", "safe.directory")
	if out, err := cmd.Output(); err != nil || !strings.Contains(string(out), "*") {
		_ = exec.Command("git", "config", "--global", "--add", "safe.directory", "*").Run()
	}
}

// register announces the agent to the control plane
func (a *Agent) register(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/project-agents/register", a.config.ControlPlaneURL)

	payload := map[string]interface{}{
		"project_id": a.config.ProjectID,
		"work_dir":   a.config.WorkDir,
		"agent_url":  fmt.Sprintf("http://loom-project-%s:8090", a.config.ProjectID), // Container name on Docker network
		"roles":      []string{a.config.Role},
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

// resultReporter sends task results back to control plane with exponential backoff retry.
func (a *Agent) resultReporter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-a.taskResultCh:
			if err := a.sendResultWithRetry(ctx, result); err != nil {
				log.Printf("Exhausted retries sending result for task %s: %v", result.TaskID, err)
			}
		}
	}
}

// sendResultWithRetry attempts to send a result up to 5 times with exponential backoff.
func (a *Agent) sendResultWithRetry(ctx context.Context, result *TaskResult) error {
	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second
	for attempt := 1; attempt <= 5; attempt++ {
		if err := a.sendResult(ctx, result); err == nil {
			return nil
		} else if attempt < 5 {
			log.Printf("Failed to send result for task %s (attempt %d/5): %v, retrying in %v", result.TaskID, attempt, err, backoff)
		} else {
			return fmt.Errorf("failed after 5 attempts: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
	return nil
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

// subscribeToTasks subscribes to NATS task messages for this project.
// When a role is configured, it also subscribes to the role-specific subject
// so the dispatcher can route tasks directly to the right agent type.
func (a *Agent) subscribeToTasks() error {
	handler := func(taskMsg *messages.TaskMessage) {
		log.Printf("Received NATS task: type=%s bead=%s correlation=%s",
			taskMsg.Type, taskMsg.BeadID, taskMsg.CorrelationID)

		switch taskMsg.Type {
		case "task.assigned":
			a.handleNatsTask(taskMsg)
		case "task.updated":
			log.Printf("Received task update for bead %s", taskMsg.BeadID)
		case "task.cancelled":
			log.Printf("Received task cancellation for bead %s", taskMsg.BeadID)
		default:
			log.Printf("Unknown task message type: %s", taskMsg.Type)
		}
	}

	// Subscribe to project-level tasks
	if err := a.messageBus.SubscribeTasks(a.config.ProjectID, handler); err != nil {
		return err
	}

	// Also subscribe to role-specific subject when a role is configured
	if a.config.Role != "" {
		if err := a.messageBus.SubscribeTasksForRole(a.config.ProjectID, a.config.Role, handler); err != nil {
			log.Printf("Warning: Failed to subscribe to role-specific tasks (%s): %v", a.config.Role, err)
		} else {
			log.Printf("Subscribed to role-specific NATS tasks: loom.tasks.%s.%s", a.config.ProjectID, a.config.Role)
		}
	}

	return nil
}

// handleNatsTask processes a task received via NATS
func (a *Agent) handleNatsTask(taskMsg *messages.TaskMessage) {
	// Ensure workspace is git-ready before processing any task.
	a.ensureWorkspaceReady()

	action := "bash"
	params := map[string]interface{}{
		"correlation_id": taskMsg.CorrelationID,
		"task_data":      taskMsg.TaskData,
		"work_dir":       taskMsg.TaskData.WorkDir,
	}

	// When the agent has a provider configured and the action loop is enabled,
	// use the full LLM-driven action loop instead of simple bash execution.
	if a.config.ActionLoopEnabled && a.config.ProviderEndpoint != "" {
		action = "action_loop"
		params["title"] = taskMsg.TaskData.Title
		params["description"] = taskMsg.TaskData.Description
		if taskMsg.TaskData.MemoryContext != "" {
			params["memory_context"] = taskMsg.TaskData.MemoryContext
		}
	}

	req := &TaskRequest{
		TaskID:    taskMsg.BeadID,
		BeadID:    taskMsg.BeadID,
		Action:    action,
		ProjectID: taskMsg.ProjectID,
		Params:    params,
	}

	go a.executeTaskWithNats(req, taskMsg.CorrelationID)
}

// executeTaskWithNats executes a task and publishes result to NATS
func (a *Agent) executeTaskWithNats(req *TaskRequest, correlationID string) {
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

	// Execute the task (reuse existing execution logic)
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
	case "action_loop":
		title, _ := req.Params["title"].(string)
		desc, _ := req.Params["description"].(string)
		memCtx, _ := req.Params["memory_context"].(string)
		if title == "" {
			title = "Task from bead " + req.BeadID
		}
		loopCfg := ActionLoopConfig{
			MaxIterations:       a.config.MaxLoopIterations,
			ProviderEndpoint:    a.config.ProviderEndpoint,
			ProviderModel:       a.config.ProviderModel,
			ProviderAPIKey:      a.config.ProviderAPIKey,
			PersonaInstructions: a.personaInstructions,
			MemoryContext:       memCtx,
		}
		output, err = a.RunActionLoop(ctx, title, desc, loopCfg)
	default:
		err = fmt.Errorf("unsupported action: %s", req.Action)
	}

	duration := time.Since(startTime)

	// Publish result to NATS
	var resultMsg *messages.ResultMessage
	if err != nil {
		log.Printf("Task %s failed: %v", req.TaskID, err)
		resultMsg = messages.TaskFailed(
			req.ProjectID,
			req.BeadID,
			a.config.ProjectID, // Use project ID as agent ID
			messages.ResultData{
				Status:   "failure",
				Output:   output,
				Error:    err.Error(),
				Duration: duration.Milliseconds(),
			},
			correlationID,
		)
	} else {
		log.Printf("Task %s completed successfully in %v", req.TaskID, duration)
		resultMsg = messages.TaskCompleted(
			req.ProjectID,
			req.BeadID,
			a.config.ProjectID,
			messages.ResultData{
				Status:   "success",
				Output:   output,
				Duration: duration.Milliseconds(),
			},
			correlationID,
		)
	}

	// Publish to NATS
	if a.messageBus != nil {
		if err := a.messageBus.PublishResult(context.Background(), req.ProjectID, resultMsg); err != nil {
			log.Printf("Failed to publish result to NATS: %v", err)
			// Fall back to HTTP result reporting
			result := &TaskResult{
				TaskID:   req.TaskID,
				BeadID:   req.BeadID,
				Success:  err == nil,
				Output:   output,
				Duration: duration,
			}
			if err != nil {
				result.Error = err.Error()
			}
			a.taskResultCh <- result
		} else {
			log.Printf("Published result to NATS for task %s", req.TaskID)
		}
	}
}
