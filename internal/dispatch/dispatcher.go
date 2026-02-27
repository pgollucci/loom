package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/agent"
	"github.com/jordanhubbard/loom/internal/beads"
	"github.com/jordanhubbard/loom/internal/containers"
	"github.com/jordanhubbard/loom/internal/database"
	"github.com/jordanhubbard/loom/internal/eventbus"
	"github.com/jordanhubbard/loom/internal/gitops"
	"github.com/jordanhubbard/loom/internal/memory"
	"github.com/jordanhubbard/loom/internal/project"
	"github.com/jordanhubbard/loom/internal/provider"
	"github.com/jordanhubbard/loom/internal/swarm"
	"github.com/jordanhubbard/loom/internal/telemetry"
	"github.com/jordanhubbard/loom/internal/worker"
	"github.com/jordanhubbard/loom/internal/workflow"
	"github.com/jordanhubbard/loom/pkg/messages"
	"github.com/jordanhubbard/loom/pkg/models"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type StatusState string

const (
	StatusActive StatusState = "active"
	StatusParked StatusState = "parked"
)

type ReadinessMode string

const (
	ReadinessBlock ReadinessMode = "block"
	ReadinessWarn  ReadinessMode = "warn"
)

type SystemStatus struct {
	State     StatusState `json:"state"`
	Reason    string      `json:"reason"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type DispatchResult struct {
	Dispatched bool   `json:"dispatched"`
	ProjectID  string `json:"project_id,omitempty"`
	BeadID     string `json:"bead_id,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Dispatcher is responsible for selecting ready work and executing it using agents/providers.
// For now it focuses on turning beads into LLM tasks and storing the output back into bead context.
type Dispatcher struct {
	beads           *beads.Manager
	projects        *project.Manager
	agents          *agent.WorkerManager
	providers       *provider.Registry
	db              *database.Database
	eventBus        *eventbus.EventBus
	messageBus      MessageBus // NATS message bus for async agent communication
	workflowEngine  *workflow.Engine
	containerOrch   *containers.Orchestrator   // Per-project container orchestration
	worktreeManager *gitops.GitWorktreeManager // Per-agent worktree isolation
	swarmMgr        *swarm.Manager             // Dynamic service discovery
	memoryMgr       *memory.MemoryManager      // Per-project memory for context injection
	personaMatcher  *PersonaMatcher
	autoBugRouter   *AutoBugRouter
	readinessCheck  func(context.Context, string) (bool, []string)
	readinessMode   ReadinessMode
	escalator       Escalator
	maxDispatchHops int
	loopDetector    *LoopDetector

	// Commit serialization (Gap #2)
	commitLock        sync.RWMutex       // Global commit lock
	commitQueue       chan commitRequest // Queue for waiting commits
	commitLockTimeout time.Duration      // Max time to hold lock (5 min)
	commitInProgress  *commitState       // Current commit state
	commitStateMutex  sync.RWMutex       // Protects commitInProgress

	mu     sync.RWMutex
	status SystemStatus

	inflightMu sync.Mutex
	inflight   map[string]struct{} // bead IDs currently being executed

	// useNATSDispatch controls whether tasks are routed exclusively to NATS
	// container agents instead of in-process workers. Set via SetUseNATSDispatch.
	useNATSDispatch bool

	// lifecycleCtx is the dispatcher's lifecycle context, used for graceful shutdown.
	// Task goroutines derive their context from this, not from request contexts.
	lifecycleCtx context.Context

	// taskTimeout is the maximum duration for a single task execution.
	// Defaults to 30 minutes if not set.
	taskTimeout time.Duration
}

// MessageBus defines the interface for publishing task messages
type MessageBus interface {
	PublishTask(ctx context.Context, projectID string, task *messages.TaskMessage) error
	PublishTaskForRole(ctx context.Context, projectID, role string, task *messages.TaskMessage) error
}

// commitRequest represents a request to acquire the commit lock
type commitRequest struct {
	BeadID    string
	AgentID   string
	Timestamp time.Time
	ResultCh  chan error // Send result back to requester
}

// commitState tracks the current commit in progress
type commitState struct {
	BeadID    string
	AgentID   string
	StartedAt time.Time
	Node      *workflow.WorkflowNode
}

// Escalator provides CEO escalation for dispatcher guardrails.
type Escalator interface {
	EscalateBeadToCEO(beadID, reason, returnedTo string) (*models.DecisionBead, error)
}

func NewDispatcher(beadsMgr *beads.Manager, projMgr *project.Manager, agentMgr *agent.WorkerManager, registry *provider.Registry, eb *eventbus.EventBus) *Dispatcher {
	d := &Dispatcher{
		beads:             beadsMgr,
		projects:          projMgr,
		agents:            agentMgr,
		providers:         registry,
		eventBus:          eb,
		personaMatcher:    NewPersonaMatcher(),
		autoBugRouter:     NewAutoBugRouter(),
		loopDetector:      NewLoopDetector(),
		readinessMode:     ReadinessWarn,
		commitQueue:       make(chan commitRequest, 100),
		commitLockTimeout: 5 * time.Minute,
		inflight:          make(map[string]struct{}),
		status: SystemStatus{
			State:     StatusParked,
			Reason:    "not started",
			UpdatedAt: time.Now(),
		},
	}

	// Start commit queue processor goroutine
	go d.processCommitQueue()

	return d
}

func (d *Dispatcher) GetSystemStatus() SystemStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.status
}

// SetUseNATSDispatch enables or disables NATS-only task routing for this
// dispatcher instance. This replaces the former package-level UseNATSDispatch
// global, allowing per-instance configuration and safe test isolation.
func (d *Dispatcher) SetUseNATSDispatch(enabled bool) {
	d.mu.Lock()
	d.useNATSDispatch = enabled
	d.mu.Unlock()
}

// SetDatabase sets the database for conversation context management
func (d *Dispatcher) SetDatabase(db *database.Database) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.db = db
}

// SetMessageBus sets the message bus for async agent communication
func (d *Dispatcher) SetMessageBus(mb MessageBus) {
	d.mu.Lock()
	d.messageBus = mb
	d.mu.Unlock()

	log.Printf("[Dispatcher] Message bus configured for async task publishing")

	// Subscribe to task results if the message bus supports it
	if nmb, ok := mb.(interface {
		SubscribeResults(func(*messages.ResultMessage)) error
	}); ok {
		if err := nmb.SubscribeResults(d.handleTaskResult); err != nil {
			log.Printf("[Dispatcher] Warning: Failed to subscribe to task results: %v", err)
		} else {
			log.Printf("[Dispatcher] Subscribed to NATS task results")
		}
	}
}

// handleTaskResult processes a task result received via NATS
func (d *Dispatcher) handleTaskResult(result *messages.ResultMessage) {
	log.Printf("[Dispatcher] Received NATS result: bead=%s agent=%s status=%s correlation=%s",
		result.BeadID, result.AgentID, result.Result.Status, result.CorrelationID)

	if result.Result.Status == "in_progress" {
		log.Printf("[Dispatcher] Task in progress for bead %s: %s", result.BeadID, result.Result.Output)
		return
	}

	// Check if bead has an active workflow — let the workflow engine govern
	// the lifecycle instead of naively closing the bead.
	hasWorkflow := false
	if d.workflowEngine != nil {
		if exec, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(result.BeadID); err == nil && exec != nil {
			hasWorkflow = true
			condition := workflow.EdgeConditionSuccess
			if result.Result.Status == "failure" {
				condition = workflow.EdgeConditionFailure
			}
			resultData := map[string]string{
				"agent_id":       result.AgentID,
				"output":         result.Result.Output,
				"correlation_id": result.CorrelationID,
			}
			if err := d.workflowEngine.AdvanceWorkflow(exec.ID, condition, result.AgentID, resultData); err != nil {
				log.Printf("[Dispatcher] Failed to advance workflow for bead %s: %v", result.BeadID, err)
			} else {
				log.Printf("[Dispatcher] Advanced workflow for bead %s (condition=%s)", result.BeadID, condition)
			}
		}
	}

	updates := make(map[string]interface{})
	switch result.Result.Status {
	case "success":
		if hasWorkflow {
			updates["context"] = map[string]string{
				"last_output":          result.Result.Output,
				"completed_at":         time.Now().UTC().Format(time.RFC3339),
				"correlation_id":       result.CorrelationID,
				"redispatch_requested": "true",
			}
		} else {
			updates["status"] = models.BeadStatusClosed
			updates["context"] = map[string]string{
				"last_output":    result.Result.Output,
				"completed_at":   time.Now().UTC().Format(time.RFC3339),
				"correlation_id": result.CorrelationID,
			}
		}
		log.Printf("[Dispatcher] Task completed for bead %s (workflow=%v)", result.BeadID, hasWorkflow)
	case "failure":
		updates["status"] = models.BeadStatusOpen
		updates["context"] = map[string]string{
			"last_error":           result.Result.Error,
			"failed_at":            time.Now().UTC().Format(time.RFC3339),
			"correlation_id":       result.CorrelationID,
			"redispatch_requested": "true",
		}
		log.Printf("[Dispatcher] Task failed for bead %s: %s", result.BeadID, result.Result.Error)
	}

	if len(updates) > 0 {
		if err := d.beads.UpdateBead(result.BeadID, updates); err != nil {
			log.Printf("[Dispatcher] Failed to update bead %s after result: %v", result.BeadID, err)
		}
	}

	if d.eventBus != nil {
		eventType := eventbus.EventTypeBeadCompleted
		if result.Result.Status == "failure" {
			eventType = eventbus.EventTypeBeadStatusChange
		}
		_ = d.eventBus.PublishBeadEvent(eventType, result.BeadID, result.ProjectID, map[string]interface{}{
			"agent_id": result.AgentID,
			"duration": result.Result.Duration,
		})
	}
}

// SetWorkflowEngine sets the workflow engine for workflow-aware dispatching
func (d *Dispatcher) SetWorkflowEngine(engine *workflow.Engine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.workflowEngine = engine
}

// SetContainerOrchestrator sets the container orchestrator for per-project containers
func (d *Dispatcher) SetContainerOrchestrator(orch *containers.Orchestrator) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containerOrch = orch
}

// SetSwarmManager sets the swarm manager used for dynamic service discovery.
// When set, the dispatcher consults the swarm registry to route tasks to
// remote agent instances before falling back to in-process workers.
func (d *Dispatcher) SetSwarmManager(mgr *swarm.Manager) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.swarmMgr = mgr
}

// SetMemoryManager injects the per-project memory manager used for context enrichment.
func (d *Dispatcher) SetMemoryManager(mgr *memory.MemoryManager) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.memoryMgr = mgr
}

// SetWorktreeManager sets the git worktree manager for parallel agent isolation.
func (d *Dispatcher) SetWorktreeManager(wm *gitops.GitWorktreeManager) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.worktreeManager = wm
}

// SetEscalator sets the escalator used for CEO escalation.
func (d *Dispatcher) SetEscalator(escalator Escalator) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.escalator = escalator
}

// SetMaxDispatchHops configures the max hop limit before escalation.
func (d *Dispatcher) SetMaxDispatchHops(maxHops int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maxDispatchHops = maxHops
}

func (d *Dispatcher) SetReadinessCheck(check func(context.Context, string) (bool, []string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readinessCheck = check
}

func (d *Dispatcher) SetReadinessMode(mode ReadinessMode) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if mode != ReadinessBlock && mode != ReadinessWarn {
		return // Keep current default if mode is unrecognized/empty
	}
	d.readinessMode = mode
}

// SetLifecycleContext sets the dispatcher's lifecycle context for graceful shutdown.
// Task goroutines derive their context from this, enabling cancellation propagation
// when Loom is shutting down.
func (d *Dispatcher) SetLifecycleContext(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lifecycleCtx = ctx
}

// SetTaskTimeout sets the maximum duration for a single task execution.
// If not set, defaults to 30 minutes.
func (d *Dispatcher) SetTaskTimeout(timeout time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.taskTimeout = timeout
}

// DefaultTaskTimeout is the default maximum duration for task execution.
const DefaultTaskTimeout = 30 * time.Minute

// processCommitQueue processes commit requests sequentially to prevent git conflicts
func (d *Dispatcher) processCommitQueue() {
	for req := range d.commitQueue {
		// Acquire global commit lock
		d.commitLock.Lock()
		defer d.commitLock.Unlock()

		// Set commit state
		d.commitStateMutex.Lock()
		d.commitInProgress = &commitState{
			BeadID:    req.BeadID,
			AgentID:   req.AgentID,
			StartedAt: time.Now(),
		}
		d.commitStateMutex.Unlock()

		log.Printf("[Commit] Processing commit for bead %s (agent %s)", req.BeadID, req.AgentID)

		// Signal that lock is acquired (requester can proceed with commit)
		select {
		case req.ResultCh <- nil:
		default:
		}

		// Lock will be released by releaseCommitLock() after commit completes
	}
}

// acquireCommitLock acquires the global commit lock for a bead
func (d *Dispatcher) acquireCommitLock(ctx context.Context, beadID, agentID string) error {
	// Check for timeout from previous commit
	d.commitStateMutex.RLock()
	if d.commitInProgress != nil {
		elapsed := time.Since(d.commitInProgress.StartedAt)
		if elapsed > d.commitLockTimeout {
			log.Printf("[Commit] WARNING: Previous commit by agent %s timed out after %v, forcibly releasing lock",
				d.commitInProgress.AgentID, elapsed)
			d.commitStateMutex.RUnlock()
			d.releaseCommitLock()
		} else {
			d.commitStateMutex.RUnlock()
		}
	} else {
		d.commitStateMutex.RUnlock()
	}

	// Send commit request to queue
	req := commitRequest{
		BeadID:    beadID,
		AgentID:   agentID,
		Timestamp: time.Now(),
		ResultCh:  make(chan error, 1),
	}

	select {
	case d.commitQueue <- req:
		log.Printf("[Commit] Bead %s queued for commit (agent %s)", beadID, agentID)
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for commit queue")
	}

	// Wait for commit to be processed
	select {
	case err := <-req.ResultCh:
		return err
	case <-ctx.Done():
		// processCommitQueue may have already acquired the lock and sent on ResultCh
		// (buffered, so the send won't block). If so, release it now to avoid an
		// indefinite hold; otherwise the 5-minute timeout rescue will clean it up.
		select {
		case lockErr := <-req.ResultCh:
			if lockErr == nil {
				d.releaseCommitLock()
			}
		default:
			// Lock not yet acquired; no action needed.
		}
		return fmt.Errorf("context cancelled while waiting for commit")
	}
}

// releaseCommitLock releases the global commit lock
func (d *Dispatcher) releaseCommitLock() {
	d.commitStateMutex.Lock()
	if d.commitInProgress != nil {
		log.Printf("[Commit] Releasing commit lock for bead %s (held for %v)",
			d.commitInProgress.BeadID, time.Since(d.commitInProgress.StartedAt))
		d.commitInProgress = nil
	}
	d.commitStateMutex.Unlock()

	d.commitLock.Unlock()
}

// DispatchOnce finds at most one ready bead and asks an idle agent to work on it.
func (d *Dispatcher) DispatchOnce(ctx context.Context, projectID string) (*DispatchResult, error) {
	ctx, span := telemetry.Tracer.Start(ctx, "dispatch.DispatchOnce")
	defer span.End()

	startTime := time.Now()
	span.SetAttributes(attribute.String("project_id", projectID))

	activeProviders := d.providers.ListActive()
	log.Printf("[Dispatcher] DispatchOnce called for project=%s, active_providers=%d", projectID, len(activeProviders))
	span.SetAttributes(attribute.Int("active_providers", len(activeProviders)))

	if len(activeProviders) == 0 {
		log.Printf("[Dispatcher] Parked - no active providers")
		d.setStatus(StatusParked, "no active providers registered")
		span.SetStatus(codes.Error, "no active providers")
		return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
	}

	ready, err := d.beads.GetReadyBeads(projectID)
	if err != nil {
		d.setStatus(StatusParked, "failed to list ready beads")
		return nil, err
	}

	// Project-level readiness gate
	blocked, earlyResult := d.checkProjectReadiness(ctx, projectID)
	if blocked {
		return earlyResult, nil
	}

	// Per-bead readiness filtering
	d.mu.RLock()
	readinessCheck := d.readinessCheck
	readinessMode := d.readinessMode
	d.mu.RUnlock()
	ready = d.filterBeadsByReadiness(ctx, ready, readinessCheck, readinessMode)
	if readinessMode == ReadinessBlock && len(ready) == 0 {
		d.setStatus(StatusParked, "project readiness failed")
		return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
	}

	log.Printf("[Dispatcher] GetReadyBeads returned %d beads for project %s", len(ready), projectID)

	sortReadyBeads(ready)

	idleAgents := d.filterIdleAgents(d.agents.GetIdleAgentsByProject(projectID))
	idleByID, allByID := d.buildAgentMaps(projectID, idleAgents)

	sel := d.selectCandidate(ctx, ready, idleAgents, idleByID, allByID)
	candidate := sel.Bead
	ag := sel.Agent

	if len(sel.SkippedReasons) > 0 {
		log.Printf("[Dispatcher] Skipped beads: %+v", sel.SkippedReasons)
	}

	if candidate == nil {
		reasonsJSON, _ := json.Marshal(sel.SkippedReasons)
		log.Printf("[Dispatcher] No dispatchable beads found (ready: %d, idle agents: %d, skipped: %s)", len(ready), len(idleAgents), string(reasonsJSON))
		d.setStatus(StatusParked, "no dispatchable beads")
		return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
	}

	// Claim the inflight slot immediately after candidate selection.
	// This is the authoritative gate that prevents two concurrent DispatchOnce
	// calls from both processing the same bead. The selectCandidate check is
	// advisory; this is the atomic compare-and-set.
	d.inflightMu.Lock()
	if _, alreadyRunning := d.inflight[candidate.ID]; alreadyRunning {
		d.inflightMu.Unlock()
		log.Printf("[Dispatcher] Bead %s lost inflight race, skipping duplicate dispatch", candidate.ID)
		return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
	}
	d.inflight[candidate.ID] = struct{}{}
	d.inflightMu.Unlock()

	releaseInflight := func() {
		d.inflightMu.Lock()
		delete(d.inflight, candidate.ID)
		d.inflightMu.Unlock()
	}

	selectedProjectID := projectID
	if selectedProjectID == "" {
		selectedProjectID = candidate.ProjectID
	}
	if ag == nil {
		releaseInflight()
		d.setStatus(StatusParked, "no idle agents with active providers")
		return &DispatchResult{Dispatched: false, ProjectID: selectedProjectID}, nil
	}

	providerID := d.selectProviderForTask(candidate, ag)
	if providerID == "" {
		releaseInflight()
		d.setStatus(StatusParked, "no active providers available")
		return &DispatchResult{Dispatched: false, ProjectID: selectedProjectID, AgentID: ag.ID}, nil
	}

	// Apply the selected provider to the agent so the worker uses the correct endpoint.
	// The dispatcher selects the best provider each dispatch, but the agent/worker
	// retains the provider from spawn time unless explicitly updated here.
	if ag.ProviderID != providerID {
		if err := d.agents.UpdateAgentProvider(ag.ID, providerID); err != nil {
			log.Printf("[Dispatcher] Warning: failed to switch agent %s to provider %s: %v", ag.ID, providerID, err)
		} else {
			ag.ProviderID = providerID // keep local pointer in sync
		}
	}

	if err := d.claimAndAssign(candidate, ag, selectedProjectID); err != nil {
		releaseInflight()
		log.Printf("[Dispatcher] Skipping bead %s (claim failed for agent %s, project=%s): %v", candidate.ID, ag.ID, selectedProjectID, err)
		return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
	}

	dispatchCount := dispatchCountForBead(candidate)
	d.publishDispatchedTask(ctx, candidate, ag, selectedProjectID, dispatchCount)

	proj, _ := d.projects.GetProject(selectedProjectID)

	var conversationSession *models.ConversationContext
	if d.db != nil {
		conversationSession, err = d.getOrCreateConversationSession(candidate, selectedProjectID)
		if err != nil {
			log.Printf("[Dispatcher] Warning: Failed to get/create conversation session for bead %s: %v", candidate.ID, err)
		} else if conversationSession != nil {
			log.Printf("[Dispatcher] Using conversation session %s for bead %s (messages: %d)",
				conversationSession.SessionID, candidate.ID, len(conversationSession.Messages))
		}
	}

	task := &worker.Task{
		ID:                  fmt.Sprintf("task-%s-%d", candidate.ID, time.Now().UnixNano()),
		Description:         buildBeadDescription(candidate),
		Context:             buildBeadContext(candidate, proj),
		BeadID:              candidate.ID,
		ProjectID:           selectedProjectID,
		ConversationSession: conversationSession,
	}

	d.setStatus(StatusActive, fmt.Sprintf("dispatching %s", candidate.ID))
	dispatchResult := &DispatchResult{Dispatched: true, ProjectID: selectedProjectID, BeadID: candidate.ID, AgentID: ag.ID, ProviderID: ag.ProviderID}

	// NATS-only and swarm routing removed: the TaskExecutor handles all bead
	// execution directly via worker.ExecuteTaskWithLoop. The dispatcher is kept
	// for legacy compatibility but always falls through to in-process execution.

	go func() {
		defer func() {
			d.inflightMu.Lock()
			delete(d.inflight, candidate.ID)
			d.inflightMu.Unlock()
		}()

		// Use lifecycle context with task timeout instead of request context.
		// This enables graceful shutdown cancellation and prevents goroutine leaks.
		d.mu.RLock()
		baseCtx := d.lifecycleCtx
		timeout := d.taskTimeout
		d.mu.RUnlock()
		if baseCtx == nil {
			baseCtx = ctx
		}
		if timeout == 0 {
			timeout = DefaultTaskTimeout
		}
		taskCtx, cancel := context.WithTimeout(baseCtx, timeout)
		defer cancel()

		if d.workflowEngine != nil {
			execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(candidate.ID)
			if err == nil && execution != nil {
				node, err := d.workflowEngine.GetCurrentNode(execution.ID)
				if err == nil && node != nil && node.NodeType == workflow.NodeTypeCommit {
					if err := d.acquireCommitLock(taskCtx, candidate.ID, ag.ID); err != nil {
						log.Printf("[Commit] Failed to acquire commit lock for bead %s: %v", candidate.ID, err)
					} else {
						defer d.releaseCommitLock()
						log.Printf("[Commit] Acquired commit lock for bead %s (agent %s)", candidate.ID, ag.ID)
					}
				}
			}
		}

		result, execErr := d.agents.ExecuteTask(taskCtx, ag.ID, task)
		if execErr != nil {
			d.processTaskError(candidate, ag, selectedProjectID, execErr)
			return
		}
		d.processTaskSuccess(candidate, ag, selectedProjectID, result)
	}()

	latency := float64(time.Since(startTime).Milliseconds())
	telemetry.DispatchLatency.Record(ctx, latency)
	span.SetAttributes(
		attribute.String("bead_id", candidate.ID),
		attribute.String("agent_id", ag.ID),
		attribute.String("provider_id", ag.ProviderID),
		attribute.Bool("dispatched", true),
	)
	span.SetStatus(codes.Ok, "dispatch successful")

	return dispatchResult, nil
}

func buildDispatchHistory(bead *models.Bead, agentID string) (historyJSON string, loopDetected bool, loopReason string) {
	history := make([]string, 0)
	if bead != nil && bead.Context != nil {
		if raw := bead.Context["dispatch_history"]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &history); err != nil {
				_ = err // malformed history is non-fatal; start fresh
			}
		}
	}
	history = append(history, agentID)
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	b, _ := json.Marshal(history)
	historyJSON = string(b)

	if len(history) < 6 {
		return historyJSON, false, ""
	}
	last := history[len(history)-6:]
	unique := map[string]struct{}{}
	for _, id := range last {
		unique[id] = struct{}{}
	}
	if len(unique) != 2 {
		return historyJSON, false, ""
	}
	if last[0] == last[1] {
		return historyJSON, false, ""
	}
	for i := 2; i < len(last); i++ {
		if last[i] != last[i%2] {
			return historyJSON, false, ""
		}
	}
	return historyJSON, true, "dispatch alternated between two agents for 6 runs"
}

func (d *Dispatcher) setStatus(state StatusState, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status = SystemStatus{State: state, Reason: reason, UpdatedAt: time.Now()}
}

// getOrCreateConversationSession retrieves an existing conversation session for a bead,
// or creates a new one if none exists or the existing one is expired
func (d *Dispatcher) getOrCreateConversationSession(bead *models.Bead, projectID string) (*models.ConversationContext, error) {
	if d.db == nil {
		return nil, nil
	}

	// Check if bead context has a session_id
	var sessionID string
	if bead.Context != nil {
		sessionID = bead.Context["conversation_session_id"]
	}

	// Try to load existing session if we have a session ID
	if sessionID != "" {
		session, err := d.db.GetConversationContext(sessionID)
		if err == nil && session != nil {
			// Check if session is expired
			if !session.IsExpired() {
				log.Printf("[Dispatcher] Resuming conversation session %s for bead %s", sessionID, bead.ID)
				return session, nil
			}
			log.Printf("[Dispatcher] Conversation session %s expired, creating new session", sessionID)
		} else {
			log.Printf("[Dispatcher] Failed to load conversation session %s: %v", sessionID, err)
		}
	}

	// No session or expired/invalid - create new session
	newSessionID := uuid.New().String()
	session := models.NewConversationContext(
		newSessionID,
		bead.ID,
		projectID,
		24*time.Hour, // Default 24h expiration
	)

	// Store agent/provider info in metadata if available
	if bead.Context != nil {
		if agentID := bead.Context["agent_id"]; agentID != "" {
			session.Metadata["agent_id"] = agentID
		}
		if agentName := bead.Context["agent_name"]; agentName != "" {
			session.Metadata["agent_name"] = agentName
		}
		if providerID := bead.Context["provider_id"]; providerID != "" {
			session.Metadata["provider_id"] = providerID
		}
	}

	// Save session to database
	if err := d.db.CreateConversationContext(session); err != nil {
		return nil, fmt.Errorf("failed to create conversation context: %w", err)
	}

	// Store session_id in bead context
	if bead.Context == nil {
		bead.Context = make(map[string]string)
	}
	bead.Context["conversation_session_id"] = newSessionID

	// Update bead with session ID (if beads manager is available)
	if d.beads != nil {
		updates := map[string]interface{}{
			"context": bead.Context,
		}
		if err := d.beads.UpdateBead(bead.ID, updates); err != nil {
			log.Printf("[Dispatcher] Warning: Failed to update bead %s with session ID: %v", bead.ID, err)
			// Don't fail - session is created, just not stored in bead yet
		}
	}

	log.Printf("[Dispatcher] Created new conversation session %s for bead %s", newSessionID, bead.ID)
	return session, nil
}

func (d *Dispatcher) providersModel(providerID string) string {
	p, err := d.providers.Get(providerID)
	if err != nil || p == nil || p.Config == nil {
		return ""
	}
	return p.Config.Model
}

func buildBeadDescription(b *models.Bead) string {
	return fmt.Sprintf("Work on bead %s: %s\n\n%s", b.ID, b.Title, b.Description)
}

func buildBeadContext(b *models.Bead, p *models.Project) string {
	var sb strings.Builder

	// Project identity and context
	if p != nil {
		sb.WriteString(fmt.Sprintf("Project: %s (%s)\nBranch: %s\n", p.Name, p.ID, p.Branch))

		// Build/test commands
		if len(p.Context) > 0 {
			for k, v := range p.Context {
				sb.WriteString(fmt.Sprintf("%s: %s\n", k, v))
			}
		}
		sb.WriteString("\n")

		// Find project work directory: WorkDir if set, otherwise standard clone path
		workDir := p.WorkDir
		if workDir == "" {
			// Standard clone location inside container
			workDir = filepath.Join("data", "projects", p.ID)
		}

		// Read AGENTS.md from project (like Claude Code reads it automatically)
		agentsMD := readProjectFile(workDir, "AGENTS.md", 4000)
		if agentsMD != "" {
			sb.WriteString("## Project Instructions (AGENTS.md)\n\n")
			sb.WriteString(agentsMD)
			sb.WriteString("\n\n")
		}
	}

	// Bead metadata
	sb.WriteString(fmt.Sprintf("Bead: %s (P%d %s)\n", b.ID, b.Priority, b.Type))

	if len(b.Context) > 0 {
		for k, v := range b.Context {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}

	// Directive: act, don't plan
	sb.WriteString(`
## Instructions

You are an autonomous coding agent. Your job is to MAKE CHANGES, COMMIT, and PUSH.

WORKFLOW:
1. Locate: scope + read relevant files (iterations 1-3)
2. Change: edit or write files (iterations 4-15)
3. Verify: build and test (iterations 16-18)
4. Land: git_commit, git_push, done (iterations 19-21)

CRITICAL RULES:
- You have 25 iterations. Use them.
- ALWAYS git_commit after making changes.
- ALWAYS git_push after committing.
- ALWAYS build and test before pushing.
- Uncommitted work is LOST work.
`)

	return sb.String()
}

// readProjectFile reads a file from the project work directory, truncated to maxLen.
func readProjectFile(workDir, filename string, maxLen int) string {
	path := filepath.Join(workDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > maxLen {
		content = content[:maxLen] + "\n... (truncated)"
	}
	return content
}

// ensureBeadHasWorkflow checks if a bead has a workflow execution, and if not, starts one
func (d *Dispatcher) ensureBeadHasWorkflow(ctx context.Context, bead *models.Bead) (*workflow.WorkflowExecution, error) {
	if d.workflowEngine == nil {
		return nil, nil // Workflow engine not available
	}

	// Check if bead already has a workflow
	execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(bead.ID)
	if err != nil {
		log.Printf("[Workflow] Error checking workflow for bead %s: %v", bead.ID, err)
		return nil, err
	}

	if execution != nil && execution.Status != workflow.ExecutionStatusCompleted {
		// Bead already has an active workflow
		return execution, nil
	}
	if execution != nil && execution.Status == workflow.ExecutionStatusCompleted {
		// Old workflow completed — delete it so a fresh one can start
		log.Printf("[Workflow] Resetting completed workflow %s for bead %s", execution.ID, bead.ID)
		_ = d.workflowEngine.ResetWorkflowForBead(bead.ID)
	}

	// Determine workflow type - check for self-improvement first
	var workflowType string
	title := strings.ToLower(bead.Title)

	// Check if bead is tagged for self-improvement (highest priority)
	isSelfImprovement := false
	for _, tag := range bead.Tags {
		tagLower := strings.ToLower(tag)
		if tagLower == "self-improvement" || tagLower == "code-review" ||
			tagLower == "maintainability" || tagLower == "refactoring" {
			isSelfImprovement = true
			break
		}
	}

	// Also check title for self-improvement keywords
	if strings.Contains(title, "[code review]") || strings.Contains(title, "[refactor]") ||
		strings.Contains(title, "[optimization]") || strings.Contains(title, "[self-improvement]") ||
		strings.Contains(title, "[maintenance]") {
		isSelfImprovement = true
	}

	if isSelfImprovement {
		workflowType = "self-improvement"
		log.Printf("[Workflow] Matched bead %s to self-improvement workflow (tags: %v)", bead.ID, bead.Tags)
	} else if strings.Contains(title, "feature") || strings.Contains(title, "enhancement") {
		workflowType = "feature"
	} else if strings.Contains(title, "ui") || strings.Contains(title, "design") || strings.Contains(title, "css") || strings.Contains(title, "html") {
		workflowType = "ui"
	} else {
		workflowType = "bug" // Default
	}

	// Get workflow for this type
	workflows, err := d.workflowEngine.GetDatabase().ListWorkflows(workflowType, bead.ProjectID)
	if err != nil || len(workflows) == 0 {
		log.Printf("[Workflow] No workflow found for type %s, bead %s", workflowType, bead.ID)
		return nil, nil // No workflow available
	}

	// Start workflow for this bead
	execution, err = d.workflowEngine.StartWorkflow(bead.ID, workflows[0].ID, bead.ProjectID)
	if err != nil {
		log.Printf("[Workflow] Failed to start workflow for bead %s: %v", bead.ID, err)
		return nil, err
	}

	// Automatically advance to first node
	if err := d.workflowEngine.AdvanceWorkflow(execution.ID, workflow.EdgeConditionSuccess, "dispatcher", nil); err != nil {
		log.Printf("[Workflow] Warning: failed to advance bead %s to first node: %v", bead.ID, err)
		// Don't fail - the workflow is created, just needs manual advancement
	} else {
		// Refresh execution to get updated current node
		execution, _ = d.workflowEngine.GetDatabase().GetWorkflowExecution(execution.ID)
	}

	log.Printf("[Workflow] Started workflow %s for bead %s at node %s", workflows[0].Name, bead.ID, execution.CurrentNodeKey)
	return execution, nil
}

// getWorkflowRoleRequirement returns the role required for the current workflow node, if any
func (d *Dispatcher) getWorkflowRoleRequirement(execution *workflow.WorkflowExecution) string {
	if d.workflowEngine == nil || execution == nil {
		return ""
	}

	// If at workflow start (no current node), get first node
	if execution.CurrentNodeKey == "" {
		// Get first node from workflow
		wf, err := d.workflowEngine.GetDatabase().GetWorkflow(execution.WorkflowID)
		if err != nil {
			return ""
		}

		// Find edges from start (empty FromNodeKey)
		for _, edge := range wf.Edges {
			if edge.FromNodeKey == "" && edge.Condition == workflow.EdgeConditionSuccess {
				// Found start edge, get target node
				for _, node := range wf.Nodes {
					if node.NodeKey == edge.ToNodeKey {
						// Enforce Engineering Manager for commit nodes
						if node.NodeType == workflow.NodeTypeCommit {
							return "Engineering Manager"
						}
						return node.RoleRequired
					}
				}
			}
		}
		return ""
	}

	// Get current node
	node, err := d.workflowEngine.GetCurrentNode(execution.ID)
	if err != nil || node == nil {
		return ""
	}

	// Enforce Engineering Manager for commit nodes
	if node.NodeType == workflow.NodeTypeCommit {
		return "Engineering Manager"
	}

	return node.RoleRequired
}

func normalizeRoleName(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return ""
	}

	if strings.Contains(role, "/") {
		parts := strings.Split(role, "/")
		role = parts[len(parts)-1]
	}

	if idx := strings.Index(role, "("); idx != -1 {
		role = strings.TrimSpace(role[:idx])
	}

	role = strings.ReplaceAll(role, "_", "-")
	role = strings.ReplaceAll(role, " ", "-")
	for strings.Contains(role, "--") {
		role = strings.ReplaceAll(role, "--", "-")
	}
	role = strings.Trim(role, "-")
	return role
}

// rolesMatch compares two role names after normalization.
// Use this instead of direct string comparison to ensure consistent role matching.
func rolesMatch(a, b string) bool {
	return normalizeRoleName(a) == normalizeRoleName(b)
}

// inferAgentRole determines the NATS role subject based on the agent and bead.
func (d *Dispatcher) inferAgentRole(ag *models.Agent, bead *models.Bead) string {
	role := normalizeRoleName(ag.Role)
	switch {
	case strings.Contains(role, "coder"), strings.Contains(role, "engineer"),
		strings.Contains(role, "developer"), strings.Contains(role, "programmer"):
		return "coder"
	case strings.Contains(role, "reviewer"), strings.Contains(role, "code-review"):
		return "reviewer"
	case strings.Contains(role, "qa"), strings.Contains(role, "quality"),
		strings.Contains(role, "tester"), strings.Contains(role, "test"):
		return "qa"
	case strings.Contains(role, "pm"), strings.Contains(role, "product-manager"),
		strings.Contains(role, "project-manager"):
		return "pm"
	case strings.Contains(role, "architect"), strings.Contains(role, "cto"):
		return "architect"
	}

	if bead != nil {
		beadType := strings.ToLower(string(bead.Type))
		if strings.Contains(beadType, "bug") || strings.Contains(beadType, "feature") || strings.Contains(beadType, "task") {
			return "coder"
		}
	}

	return ""
}

// hasTag checks if a bead has a specific tag
func (d *Dispatcher) hasTag(bead *models.Bead, tag string) bool {
	if bead == nil || len(bead.Tags) == 0 {
		return false
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	for _, t := range bead.Tags {
		if strings.ToLower(strings.TrimSpace(t)) == tag {
			return true
		}
	}
	return false
}

// findDefaultTriageAgent returns the ID of the best default triage agent for a project.
// Preference: CTO > Engineering Manager > any project agent.
func (d *Dispatcher) findDefaultTriageAgent(projectID string) string {
	if d.agents == nil {
		return ""
	}
	agents := d.agents.ListAgentsByProject(projectID)
	if len(agents) == 0 {
		agents = d.agents.ListAgents()
	}
	var fallback string
	for _, ag := range agents {
		role := normalizeRoleName(ag.Role)
		if role == "cto" || role == "chief-technology-officer" {
			return ag.ID
		}
		if role == "engineering-manager" && fallback == "" {
			fallback = ag.ID
		}
	}
	if fallback != "" {
		return fallback
	}
	for _, ag := range agents {
		if ag.ProjectID == projectID || ag.ProjectID == "" {
			return ag.ID
		}
	}
	return ""
}

// createRemediationBead creates a P0 remediation bead when an agent gets stuck.
func (d *Dispatcher) createRemediationBead(stuckBead *models.Bead, stuckAgent *models.Agent, result *worker.TaskResult) {
	// Skip remediation for provider/infrastructure errors that will resolve on their own.
	// These errors indicate transient issues (rate limits, auth problems, network issues)
	// rather than agent logic problems that remediation could fix.
	if isProviderError(result.Error) {
		log.Printf("[Remediation] Skipping remediation bead creation due to provider/infrastructure error: %s", result.Error)
		return
	}
	// Also check bead context for provider errors - the result.Error may contain
	// generic messages like "detected stuck inner loop" while the actual provider
	// errors are recorded in the bead context (last_run_error, error_history).
	if beadHasProviderErrors(stuckBead.Context) {
		log.Printf("[Remediation] Skipping remediation bead creation - bead context indicates provider/infrastructure errors")
		return
	}
	if d.beads == nil {
		log.Printf("[Remediation] Cannot create remediation bead: beads manager not available")
		return
	}

	// Skip remediation for already-closed beads
	if stuckBead.Status == models.BeadStatusClosed {
		log.Printf("[Remediation] Skipping remediation for %s — bead is already closed", stuckBead.ID)
		return
	}

	// Prevent remediation cascades by computing the chain depth.
	// Walk the remediation_for chain back to the original bead. If the
	// depth exceeds 1 we refuse to create another remediation bead.
	const maxRemediationDepth = 1
	depth := 0
	cur := stuckBead
	for cur != nil {
		isRemediation := strings.Contains(cur.Title, "Remediation:") ||
			(cur.Context != nil && cur.Context["remediation_for"] != "") ||
			(cur.Context != nil && cur.Context["created_by"] == "dispatcher_auto_remediation")
		if !isRemediation {
			break
		}
		depth++
		parentID := ""
		if cur.Context != nil {
			parentID = cur.Context["remediation_for"]
		}
		if parentID == "" || depth > maxRemediationDepth {
			break
		}
		parentBead, err := d.beads.GetBead(parentID)
		if err != nil || parentBead == nil {
			break
		}
		cur = parentBead
	}
	if depth > 0 {
		log.Printf("[Remediation] Skipping remediation for %s — already at depth %d (max %d)", stuckBead.ID, depth, maxRemediationDepth)
		if d.escalator != nil {
			reason := fmt.Sprintf("Remediation chain at depth %d for bead %s — agent cannot self-heal", depth, stuckBead.ID)
			if _, err := d.escalator.EscalateBeadToCEO(stuckBead.ID, reason, ""); err != nil {
				log.Printf("[Remediation] CEO escalation failed for %s: %v", stuckBead.ID, err)
			} else {
				log.Printf("[Remediation] Escalated %s to CEO — remediation chain exhausted", stuckBead.ID)
			}
		}
		return
	}

	const remediationCooldown = 15 * time.Minute
	if stuckBead.Context != nil {
		if lastStr := stuckBead.Context["last_remediation_at"]; lastStr != "" {
			if lastTime, err := time.Parse(time.RFC3339, lastStr); err == nil {
				if time.Since(lastTime) < remediationCooldown {
					log.Printf("[Remediation] Skipping remediation for %s — cooldown (last filed %s ago)",
						stuckBead.ID, time.Since(lastTime).Truncate(time.Second))
					return
				}
			}
		}
	}

	const maxRemediationsPerBead = 3
	remCount := 0
	if stuckBead.Context != nil {
		if countStr := stuckBead.Context["remediation_count"]; countStr != "" {
			_, _ = fmt.Sscanf(countStr, "%d", &remCount)
		}
	}
	if remCount >= maxRemediationsPerBead {
		log.Printf("[Remediation] Skipping remediation for %s — max remediations reached (%d/%d)",
			stuckBead.ID, remCount, maxRemediationsPerBead)
		if d.escalator != nil {
			reason := fmt.Sprintf("Bead %s has exhausted %d remediation attempts — requires human review", stuckBead.ID, remCount)
			if _, err := d.escalator.EscalateBeadToCEO(stuckBead.ID, reason, ""); err != nil {
				log.Printf("[Remediation] CEO escalation failed for %s: %v", stuckBead.ID, err)
			}
		}
		return
	}

	// Extract progress metrics if available
	var progressMetrics string
	var stagnationReason string
	var actionTypeCounts string

	// Try to extract metadata from result (which might be a LoopResult)
	if loopResult, ok := interface{}(result).(*worker.LoopResult); ok && loopResult.Metadata != nil {
		if metrics, exists := loopResult.Metadata["progress_metrics"]; exists {
			if metricsJSON, err := json.MarshalIndent(metrics, "  ", "  "); err == nil {
				progressMetrics = string(metricsJSON)
			}
		}
		if reason, exists := loopResult.Metadata["stagnation_reason"]; exists {
			stagnationReason = fmt.Sprintf("%v", reason)
		}
		if counts, exists := loopResult.Metadata["action_type_counts"]; exists {
			if countsJSON, err := json.MarshalIndent(counts, "  ", "  "); err == nil {
				actionTypeCounts = string(countsJSON)
			}
		}
	}

	// Build comprehensive description for remediation agent
	description := fmt.Sprintf(`## Remediation Request: Agent Stuck on Bead %s

**Stuck Pattern Detected:** %s
**Reason:** %s

### Stuck Agent Details
- Agent ID: %s
- Agent Name: %s
- Agent Role: %s
- Persona: %s

### Stuck Bead Details
- Bead ID: %s
- Title: %s
- Status: %s
- Priority: %v
- Iterations: %d
- Terminal Reason: %s

### Progress Analysis
%s

### Action Type Distribution
%s

### Last Agent Output
%s

### Task for Remediation Specialist

You are a meta-level debugging specialist. Your task is to:

1. **Analyze** the stuck agent's conversation history (bead %s)
2. **Diagnose** the root cause:
   - Is the agent blind to output? (missing data in action results)
   - Is the persona instruction unclear or misleading?
   - Is there a bug in an action handler?
   - Is a required capability missing?
   - Is the task itself ill-defined?

3. **Fix** the blocker:
   - Modify code if there's a bug
   - Update persona if instructions are unclear
   - Add missing capabilities if needed
   - Improve feedback/error messages
   - Clarify task description if needed

4. **Verify** the fix prevents future occurrences

Work singlemindedly until the blocker is resolved. You have full access to:
- Read the stuck agent's conversation history
- Modify code, personas, and configuration
- Test fixes before deploying
- Create follow-up remediation beads if needed

**Priority:** This is blocking agent progress. Fix it as quickly as possible.
`,
		stuckBead.ID,
		result.LoopTerminalReason,
		stagnationReason,
		stuckAgent.ID,
		stuckAgent.Name,
		stuckAgent.Role,
		stuckAgent.PersonaName,
		stuckBead.ID,
		stuckBead.Title,
		stuckBead.Status,
		stuckBead.Priority,
		result.LoopIterations,
		result.LoopTerminalReason,
		progressMetrics,
		actionTypeCounts,
		truncateString(result.Response, 500),
		stuckBead.ID,
	)

	// Create remediation bead using the manager's CreateBead method
	title := fmt.Sprintf("Remediation: Fix agent stuck on %s", stuckBead.ID)
	remediationBead, err := d.beads.CreateBead(
		title,
		description,
		models.BeadPriorityP0, // Highest priority
		"task",                // Remediation is a task
		stuckBead.ProjectID,
	)
	if err != nil {
		log.Printf("[Remediation] Failed to create remediation bead for %s: %v", stuckBead.ID, err)
		return
	}

	// Update context with remediation metadata
	contextUpdates := map[string]interface{}{
		"context": map[string]string{
			"remediation_for":       stuckBead.ID,
			"stuck_agent_id":        stuckAgent.ID,
			"stuck_terminal_reason": result.LoopTerminalReason,
			"stuck_iterations":      fmt.Sprintf("%d", result.LoopIterations),
			"created_by":            "dispatcher_auto_remediation",
			"requires_persona":      "remediation-expert",
		},
	}
	if err := d.beads.UpdateBead(remediationBead.ID, contextUpdates); err != nil {
		log.Printf("[Remediation] Warning: Failed to update remediation bead context: %v", err)
	}

	// Track remediation metadata on the source bead for cooldown/counter checks.
	sourceCtx := map[string]interface{}{
		"context": map[string]string{
			"last_remediation_at":   time.Now().UTC().Format(time.RFC3339),
			"remediation_count":     fmt.Sprintf("%d", remCount+1),
			"last_remediation_bead": remediationBead.ID,
		},
	}
	if err := d.beads.UpdateBead(stuckBead.ID, sourceCtx); err != nil {
		log.Printf("[Remediation] Warning: Failed to update source bead %s with remediation metadata: %v", stuckBead.ID, err)
	}

	log.Printf("[Remediation] Created remediation bead %s for stuck bead %s (reason: %s)",
		remediationBead.ID, stuckBead.ID, result.LoopTerminalReason)

	// Publish event if event bus available
	if d.eventBus != nil {
		_ = d.eventBus.PublishBeadEvent(
			eventbus.EventTypeBeadCreated,
			remediationBead.ID,
			stuckBead.ProjectID,
			map[string]interface{}{
				"type":            "remediation",
				"remediation_for": stuckBead.ID,
				"priority":        "P0",
			},
		)
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}
