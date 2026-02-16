package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/agent"
	"github.com/jordanhubbard/loom/internal/beads"
	"github.com/jordanhubbard/loom/internal/database"
	"github.com/jordanhubbard/loom/internal/observability"
	"github.com/jordanhubbard/loom/internal/project"
	"github.com/jordanhubbard/loom/internal/provider"
	"github.com/jordanhubbard/loom/internal/telemetry"
	"github.com/jordanhubbard/loom/internal/temporal/eventbus"
	"github.com/jordanhubbard/loom/internal/worker"
	"github.com/jordanhubbard/loom/internal/workflow"
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
	beads               *beads.Manager
	projects            *project.Manager
	agents              *agent.WorkerManager
	providers           *provider.Registry
	db                  *database.Database
	eventBus            *eventbus.EventBus
	workflowEngine      *workflow.Engine
	personaMatcher      *PersonaMatcher
	autoBugRouter       *AutoBugRouter
	complexityEstimator *provider.ComplexityEstimator
	readinessCheck      func(context.Context, string) (bool, []string)
	readinessMode       ReadinessMode
	escalator           Escalator
	maxDispatchHops     int
	loopDetector        *LoopDetector

	// Commit serialization (Gap #2)
	commitLock        sync.Mutex        // Global commit lock
	commitQueue       chan commitRequest // Queue for waiting commits
	commitLockTimeout time.Duration     // Max time to hold lock (5 min)
	commitInProgress  *commitState      // Current commit state
	commitStateMutex  sync.RWMutex      // Protects commitInProgress

	mu     sync.RWMutex
	status SystemStatus
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
		beads:               beadsMgr,
		projects:            projMgr,
		agents:              agentMgr,
		providers:           registry,
		eventBus:            eb,
		personaMatcher:      NewPersonaMatcher(),
		autoBugRouter:       NewAutoBugRouter(),
		complexityEstimator: provider.NewComplexityEstimator(),
		loopDetector:        NewLoopDetector(),
		readinessMode:       ReadinessWarn,
		commitQueue:         make(chan commitRequest, 100), // Buffer 100 waiting commits
		commitLockTimeout:   5 * time.Minute,
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

// SetDatabase sets the database for conversation context management
func (d *Dispatcher) SetDatabase(db *database.Database) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.db = db
}

// SetWorkflowEngine sets the workflow engine for workflow-aware dispatching
func (d *Dispatcher) SetWorkflowEngine(engine *workflow.Engine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.workflowEngine = engine
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

// processCommitQueue processes commit requests sequentially to prevent git conflicts
func (d *Dispatcher) processCommitQueue() {
	for req := range d.commitQueue {
		// Acquire global commit lock
		d.commitLock.Lock()

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
		req.ResultCh <- nil

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
	// Create tracing span for dispatch operation
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
	d.mu.RLock()
	readinessCheck := d.readinessCheck
	readinessMode := d.readinessMode
	d.mu.RUnlock()

	if readinessCheck != nil {
		if projectID != "" {
			readyOK, issues := readinessCheck(ctx, projectID)
			if !readyOK && readinessMode == ReadinessBlock {
				reason := "project readiness failed"
				if len(issues) > 0 {
					reason = fmt.Sprintf("project readiness failed: %s", strings.Join(issues, "; "))
				}
				d.setStatus(StatusParked, reason)
				return &DispatchResult{Dispatched: false, ProjectID: projectID, Error: reason}, nil
			}
		}

		projectReadiness := make(map[string]bool)
		if readinessMode == ReadinessBlock {
			filtered := make([]*models.Bead, 0, len(ready))
			for _, bead := range ready {
				if bead == nil {
					filtered = append(filtered, bead)
					continue
				}
				if _, ok := projectReadiness[bead.ProjectID]; !ok {
					okReady, _ := readinessCheck(ctx, bead.ProjectID)
					projectReadiness[bead.ProjectID] = okReady
				}
				if projectReadiness[bead.ProjectID] {
					filtered = append(filtered, bead)
				}
			}
			ready = filtered
			if len(ready) == 0 {
				d.setStatus(StatusParked, "project readiness failed")
				return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
			}
		} else {
			for _, bead := range ready {
				if bead == nil {
					continue
				}
				if _, ok := projectReadiness[bead.ProjectID]; ok {
					continue
				}
				okReady, _ := readinessCheck(ctx, bead.ProjectID)
				projectReadiness[bead.ProjectID] = okReady
			}
		}
	}

	log.Printf("[Dispatcher] GetReadyBeads returned %d beads for project %s", len(ready), projectID)
	os.WriteFile("/tmp/dispatch-ready-beads.txt", []byte(fmt.Sprintf("ready=%d project=%s\n", len(ready), projectID)), 0644)

	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i] == nil {
			return false
		}
		if ready[j] == nil {
			return true
		}
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority < ready[j].Priority
		}
		return ready[i].UpdatedAt.After(ready[j].UpdatedAt)
	})

	// Only auto-dispatch non-P0 task/epic beads.
	idleAgents := d.agents.GetIdleAgentsByProject(projectID)
	filteredAgents := make([]*models.Agent, 0, len(idleAgents))
	for _, candidateAgent := range idleAgents {
		if candidateAgent == nil {
			continue
		}
		// If agent already has a provider, verify it's active.
		// If agent has no provider, auto-assign one from the active pool.
		// Note: Final provider selection happens per-bead based on complexity.
		if candidateAgent.ProviderID != "" {
			if !d.providers.IsActive(candidateAgent.ProviderID) {
				continue
			}
		} else {
			// Assign a default provider; actual routing happens per-bead
			activeProviders := d.providers.ListActive() // sorted by capability score
			if len(activeProviders) > 0 {
				best := activeProviders[0]
				candidateAgent.ProviderID = best.Config.ID
				log.Printf("[Dispatcher] Auto-assigned default provider %s (score=%.0f, latency=%dms) to agent %s",
					best.Config.ID, best.Config.CapabilityScore, best.Config.LastHeartbeatLatencyMs, candidateAgent.Name)
			} else {
				continue
			}
		}
		// Promote paused agents to idle now that they have a provider.
		if candidateAgent.Status == "paused" {
			candidateAgent.Status = "idle"
			log.Printf("[Dispatcher] Promoted agent %s from paused to idle", candidateAgent.Name)
		}
		filteredAgents = append(filteredAgents, candidateAgent)
	}
	idleAgents = filteredAgents
	os.WriteFile("/tmp/dispatch-idle-agents.txt", []byte(fmt.Sprintf("idle=%d\n", len(idleAgents))), 0644)
	idleByID := make(map[string]*models.Agent, len(idleAgents))
	for _, a := range idleAgents {
		if a != nil {
			idleByID[a.ID] = a
		}
	}

	var candidate *models.Bead
	var ag *models.Agent
	skippedReasons := make(map[string]int)
	for _, b := range ready {
		if b == nil {
			skippedReasons["nil_bead"]++
			continue
		}

		// Skip beads that require human configuration (SSH keys, infrastructure, etc.)
		// These should be handled manually or escalated to CEO, not auto-assigned to agents
		if d.hasTag(b, "requires-human-config") {
			skippedReasons["requires_human_config"]++
			log.Printf("[Dispatcher] Skipping bead %s: requires human configuration", b.ID)
			continue
		}

		// Check if this is an auto-filed bug that needs routing
		if routeInfo := d.autoBugRouter.AnalyzeBugForRouting(b); routeInfo.ShouldRoute {
			log.Printf("[Dispatcher] Auto-bug detected: %s - routing to %s (%s)", b.ID, routeInfo.PersonaHint, routeInfo.RoutingReason)

			// Update the bead with persona hint in title
			updates := map[string]interface{}{
				"title": routeInfo.UpdatedTitle,
			}
			if err := d.beads.UpdateBead(b.ID, updates); err != nil {
				log.Printf("[Dispatcher] Failed to update bead %s with persona hint: %v", b.ID, err)
			} else {
				// Refresh the bead to get updated title
				b.Title = routeInfo.UpdatedTitle
			}
		}

		// P0 beads are dispatched like any other priority.
		// CEO escalation is handled via the escalate_ceo action, not by
		// filtering at dispatch time.

		if b.Type == "decision" {
			skippedReasons["decision_type"]++
			continue
		}

		if b.Status == models.BeadStatusOpen || b.Status == models.BeadStatusInProgress {
			if b.Context == nil {
				b.Context = make(map[string]string)
			}
			if b.Context["redispatch_requested"] != "true" {
				b.Context["redispatch_requested"] = "true"
				b.Context["redispatch_requested_at"] = time.Now().UTC().Format(time.RFC3339)
				if err := d.beads.UpdateBead(b.ID, map[string]interface{}{"context": b.Context}); err != nil {
					log.Printf("[Dispatcher] Failed to auto-enable redispatch for bead %s: %v", b.ID, err)
				}
			}
		}

		dispatchCount := 0
		if b.Context != nil {
			if dispatchCountStr := b.Context["dispatch_count"]; dispatchCountStr != "" {
				_, _ = fmt.Sscanf(dispatchCountStr, "%d", &dispatchCount)
			}
		}

		maxHops := d.maxDispatchHops
		if maxHops <= 0 {
			maxHops = 20
		}

		if dispatchCount >= maxHops {
			if b.Context != nil && b.Context["escalated_to_ceo_decision_id"] != "" {
				skippedReasons["dispatch_limit_escalated"]++
				continue
			}

			// Use smart loop detection to differentiate stuck loops from productive investigation
			stuck, loopReason := d.loopDetector.IsStuckInLoop(b)

			if !stuck {
				// Making progress - allow to continue beyond hop limit
				log.Printf("[Dispatcher] Bead %s has %d dispatches but is making progress, allowing to continue. Progress: %s",
					b.ID, dispatchCount, d.loopDetector.GetProgressSummary(b))
				skippedReasons["dispatch_limit_but_progressing"]++
				// Don't continue - allow this bead to be dispatched
			} else {
				// Ralph auto-block: stuck in loop — block autonomously instead of CEO escalation
				reason := fmt.Sprintf("dispatch_count=%d exceeded max_hops=%d, stuck in loop: %s",
					dispatchCount, maxHops, loopReason)
				log.Printf("[Ralph] Bead %s is stuck after %d dispatches, auto-blocking: %s",
					b.ID, dispatchCount, loopReason)

				progressSummary := d.loopDetector.GetProgressSummary(b)

				// Attempt auto-revert of agent commits if commit range is known
				revertStatus := "not_attempted"
				firstSHA, _, commitCount := d.loopDetector.GetAgentCommitRange(b)
				if firstSHA != "" && commitCount > 0 {
					log.Printf("[Ralph] Attempting auto-revert of %d agent commits for bead %s (from %s)",
						commitCount, b.ID, firstSHA)
					// Record intent — actual revert requires git.GitService which
					// is project-scoped. The revert metadata tells the next handler
					// (or human) exactly what to revert.
					revertStatus = fmt.Sprintf("revert_recommended: %d commits from %s", commitCount, firstSHA)
				}

				ctxUpdates := map[string]string{
					"redispatch_requested": "false",
					"ralph_blocked_at":     time.Now().UTC().Format(time.RFC3339),
					"ralph_blocked_reason": reason,
					"loop_detection_reason": loopReason,
					"progress_summary":     progressSummary,
					"revert_status":        revertStatus,
				}
				if sessionID := b.Context["conversation_session_id"]; sessionID != "" {
					ctxUpdates["conversation_session_id"] = sessionID
				}

				triageAgent := d.findDefaultTriageAgent(b.ProjectID)
				updates := map[string]interface{}{
					"status":      models.BeadStatusBlocked,
					"assigned_to": triageAgent,
					"context":     ctxUpdates,
				}
				if err := d.beads.UpdateBead(b.ID, updates); err != nil {
					log.Printf("[Ralph] Failed to block bead %s: %v", b.ID, err)
				} else if triageAgent != "" {
					log.Printf("[Ralph] Blocked bead %s reassigned to triage agent %s", b.ID, triageAgent)
				}

				if d.eventBus != nil {
					_ = d.eventBus.PublishBeadEvent(eventbus.EventTypeBeadStatusChange, b.ID, b.ProjectID,
						map[string]interface{}{
							"status":        string(models.BeadStatusBlocked),
							"ralph_reason":  reason,
							"revert_status": revertStatus,
						})
				}

				skippedReasons["ralph_auto_blocked"]++
				continue
			}
		}

		if dispatchCount >= maxHops-1 {
			log.Printf("[Dispatcher] WARNING: Bead %s has been dispatched %d times", b.ID, dispatchCount)
		}

		// Skip beads that recently failed — cooldown prevents re-dispatching
		// the same broken bead 50 times in a single ralph beat.
		if b.Context != nil && b.Context["last_failed_at"] != "" {
			if lastFailed, err := time.Parse(time.RFC3339, b.Context["last_failed_at"]); err == nil {
				if time.Since(lastFailed) < 2*time.Minute {
					skippedReasons["cooldown_after_failure"]++
					continue
				}
			}
		}

		// Skip beads that have already run UNLESS:
		// 1. They explicitly request redispatch, OR
		// 2. They are still in_progress (multi-step work not complete)
		if b.Context != nil {
			if b.Context["redispatch_requested"] != "true" &&
				b.Status != "in_progress" &&
				b.Context["last_run_at"] != "" {
				skippedReasons["already_run"]++
				continue
			}
		}

		// If bead is assigned to an agent, only dispatch to that agent.
		if b.AssignedTo != "" {
			assigned, ok := idleByID[b.AssignedTo]
			if !ok {
				skippedReasons["assigned_agent_not_idle"]++
				continue
			}
			ag = assigned
			candidate = b
			break
		}

		// Check if bead has a workflow and needs specific role
		var workflowRoleRequired string
		if d.workflowEngine != nil {
			execution, err := d.ensureBeadHasWorkflow(ctx, b)
			if err != nil {
				log.Printf("[Workflow] Error ensuring workflow for bead %s: %v", b.ID, err)
			} else if execution != nil {
				// Check for timeout before processing
				isReady := d.workflowEngine.IsNodeReady(execution)
				os.WriteFile("/tmp/dispatch-workflow-check.txt", []byte(fmt.Sprintf("bead=%s execution_id=%s current_node=%s status=%s is_ready=%v\n", b.ID, execution.ID, execution.CurrentNodeKey, execution.Status, isReady)), 0644)

				// Allow dispatch for escalated workflows (they need manual intervention anyway)
				// Only block if workflow is active but node is not ready (timeout case)
				if !isReady && execution.Status != "escalated" {
					skippedReasons["workflow_node_not_ready"]++
					log.Printf("[Workflow] Bead %s workflow node not ready (may have timed out)", b.ID)
					continue
				} else if execution.Status == "escalated" {
					log.Printf("[Workflow] Bead %s workflow is escalated, allowing dispatch for manual intervention", b.ID)
				}

				workflowRoleRequired = d.getWorkflowRoleRequirement(execution)
				if workflowRoleRequired != "" {
					requiredRoleKey := normalizeRoleName(workflowRoleRequired)
					// Find agent with matching role
					for _, agent := range idleAgents {
						if agent != nil && normalizeRoleName(agent.Role) == requiredRoleKey {
							ag = agent
							candidate = b
							log.Printf("[Workflow] Matched bead %s to agent %s by workflow role %s", b.ID, agent.Name, workflowRoleRequired)
							break
						}
					}

					if ag != nil {
						break // Found workflow-matched agent
					}

					// No agent with exact role — fall through to persona/any-agent dispatch
					log.Printf("[Dispatcher] Bead %s needs workflow role %q but no idle agent has it - falling through to any-agent dispatch", b.ID, workflowRoleRequired)
				}
			}
		}

		// Try persona-based routing first, but fall back to any idle agent
		personaHint := d.personaMatcher.ExtractPersonaHint(b)
		if personaHint != "" {
			matchedAgent := d.personaMatcher.FindAgentByPersonaHint(personaHint, idleAgents)
			if matchedAgent != nil {
				ag = matchedAgent
				candidate = b
				log.Printf("[Dispatcher] Matched bead %s to agent %s via persona hint '%s'", b.ID, matchedAgent.Name, personaHint)
				break
			}
			// Persona hint found but no match - log it but fall through to assign any idle agent
			log.Printf("[Dispatcher] Bead %s has persona hint '%s' but no exact match - will assign to any idle agent", b.ID, personaHint)
		}

		// Pick an idle agent for this bead's project.
		// Prefer Engineering Manager as default assignee for unassigned beads.
		var matchedAgent *models.Agent
		var fallbackAgent *models.Agent
		for _, a := range idleAgents {
			if a.ProjectID == b.ProjectID || a.ProjectID == "" || b.ProjectID == "" {
				if fallbackAgent == nil {
					fallbackAgent = a
				}
				if normalizeRoleName(a.Role) == "engineering-manager" {
					matchedAgent = a
					break
				}
			}
		}
		if matchedAgent == nil {
			matchedAgent = fallbackAgent
		}
		if matchedAgent == nil {
			skippedReasons["no_idle_agents_for_project"]++
			continue
		}
		log.Printf("[Dispatcher] Assigning bead %s (project %s) to agent %s", b.ID, b.ProjectID, matchedAgent.Name)
		ag = matchedAgent
		candidate = b
		break
	}

	if len(skippedReasons) > 0 {
		log.Printf("[Dispatcher] Skipped beads: %+v", skippedReasons)
	}

	if candidate == nil {
		reasonsJSON, _ := json.Marshal(skippedReasons)
		log.Printf("[Dispatcher] No dispatchable beads found (ready: %d, idle agents: %d, skipped: %s)", len(ready), len(idleAgents), string(reasonsJSON))
		os.WriteFile("/tmp/dispatch-no-candidate.txt", []byte(fmt.Sprintf("ready=%d idle=%d skipped=%s\n", len(ready), len(idleAgents), string(reasonsJSON))), 0644)
		d.setStatus(StatusParked, "no dispatchable beads")
		return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
	}

	selectedProjectID := projectID
	if selectedProjectID == "" {
		selectedProjectID = candidate.ProjectID
	}
	if ag == nil {
		d.setStatus(StatusParked, "no idle agents with active providers")
		return &DispatchResult{Dispatched: false, ProjectID: selectedProjectID}, nil
	}

	// Estimate task complexity for smart provider routing
	complexity := d.estimateBeadComplexity(candidate)

	// Select provider based on complexity - match model size to task difficulty
	if ag.ProviderID == "" || complexity != provider.ComplexityMedium {
		// Use complexity-aware selection for all tasks (not just unassigned agents)
		activeProviders := d.providers.ListActiveForComplexity(complexity)
		if len(activeProviders) > 0 {
			best := activeProviders[0]
			prevProvider := ag.ProviderID
			ag.ProviderID = best.Config.ID
			log.Printf("[Dispatcher] Selected provider %s (params=%.0fB, score=%.0f) for %s complexity task %s (prev=%s)",
				best.Config.ID, best.Config.ModelParamsB, best.Config.CapabilityScore,
				complexity.String(), candidate.ID, prevProvider)
		} else if ag.ProviderID == "" {
			d.setStatus(StatusParked, "no active providers available")
			return &DispatchResult{Dispatched: false, ProjectID: selectedProjectID, AgentID: ag.ID}, nil
		}
	}

	// Ensure bead is claimed/assigned.
	if candidate.AssignedTo == "" {
		if err := d.beads.ClaimBead(candidate.ID, ag.ID); err != nil {
			d.setStatus(StatusParked, "failed to claim bead")
			observability.Error("dispatch.claim", map[string]interface{}{
				"agent_id":   ag.ID,
				"bead_id":    candidate.ID,
				"project_id": candidate.ProjectID,
			}, err)
			return &DispatchResult{Dispatched: false, ProjectID: projectID}, nil
		}
		observability.Info("dispatch.claim", map[string]interface{}{
			"agent_id":   ag.ID,
			"bead_id":    candidate.ID,
			"project_id": candidate.ProjectID,
		})
	}

	// Increment dispatch count for tracking multi-step investigations
	dispatchCount := 0
	if candidate.Context != nil {
		if countStr := candidate.Context["dispatch_count"]; countStr != "" {
			_, _ = fmt.Sscanf(countStr, "%d", &dispatchCount)
		}
	}
	dispatchCount++

	// Update bead context with incremented dispatch count
	countUpdates := map[string]interface{}{
		"context": map[string]string{
			"dispatch_count": fmt.Sprintf("%d", dispatchCount),
		},
	}
	if err := d.beads.UpdateBead(candidate.ID, countUpdates); err != nil {
		log.Printf("[Dispatcher] WARNING: Failed to update dispatch count for bead %s: %v", candidate.ID, err)
		// Don't fail dispatch on this error - just log it
	}
	log.Printf("[Dispatcher] Bead %s dispatch count: %d", candidate.ID, dispatchCount)

	// FIX #7: Log errors instead of silently discarding them
	if err := d.agents.AssignBead(ag.ID, candidate.ID); err != nil {
		log.Printf("[Dispatcher] CRITICAL: Failed to assign bead %s to agent %s: %v", candidate.ID, ag.ID, err)
		// Continue anyway - the task will still be submitted to the worker
	}
	observability.Info("dispatch.assign", map[string]interface{}{
		"agent_id":    ag.ID,
		"bead_id":     candidate.ID,
		"project_id":  selectedProjectID,
		"provider_id": ag.ProviderID,
	})
	if d.eventBus != nil {
		if err := d.eventBus.PublishBeadEvent(eventbus.EventTypeBeadAssigned, candidate.ID, selectedProjectID, map[string]interface{}{"assigned_to": ag.ID}); err != nil {
			log.Printf("[Dispatcher] Warning: Failed to publish bead assigned event for %s: %v", candidate.ID, err)
		}
		if err := d.eventBus.PublishBeadEvent(eventbus.EventTypeBeadStatusChange, candidate.ID, selectedProjectID, map[string]interface{}{"status": string(models.BeadStatusInProgress)}); err != nil {
			log.Printf("[Dispatcher] Warning: Failed to publish bead status change event for %s: %v", candidate.ID, err)
		}
	}

	proj, _ := d.projects.GetProject(selectedProjectID)

	// Get or create conversation session for multi-turn conversation support
	var conversationSession *models.ConversationContext
	if d.db != nil {
		var err error
		conversationSession, err = d.getOrCreateConversationSession(candidate, selectedProjectID)
		if err != nil {
			log.Printf("[Dispatcher] Warning: Failed to get/create conversation session for bead %s: %v", candidate.ID, err)
			// Continue without conversation session (falls back to single-shot mode)
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

	// Return immediately — execute the task asynchronously so the dispatch
	// loop can assign other agents in the same tick. The agent's status is
	// set to "working" by ExecuteTask before the LLM call starts, so the
	// next DispatchOnce won't re-assign it.
	dispatchResult := &DispatchResult{Dispatched: true, ProjectID: selectedProjectID, BeadID: candidate.ID, AgentID: ag.ID, ProviderID: ag.ProviderID}

	go func() {
		// Create independent context for task execution - don't inherit cancellation from dispatch loop
		// The task should run to completion even if the dispatch loop moves on
		taskCtx := context.Background()

		// Check if this is a commit node that needs serialization (Gap #2)
		if d.workflowEngine != nil {
			execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(candidate.ID)
			if err == nil && execution != nil {
				node, err := d.workflowEngine.GetCurrentNode(execution.ID)
				if err == nil && node != nil && node.NodeType == workflow.NodeTypeCommit {
					// Acquire commit lock before executing
					if err := d.acquireCommitLock(taskCtx, candidate.ID, ag.ID); err != nil {
						log.Printf("[Commit] Failed to acquire commit lock for bead %s: %v", candidate.ID, err)
						// Continue without lock (fallback behavior)
					} else {
						defer d.releaseCommitLock()
						log.Printf("[Commit] Acquired commit lock for bead %s (agent %s)", candidate.ID, ag.ID)
					}
				}
			}
		}

		result, execErr := d.agents.ExecuteTask(taskCtx, ag.ID, task)
	if execErr != nil {
		d.setStatus(StatusParked, "execution failed")
		observability.Error("dispatch.execute", map[string]interface{}{
			"agent_id":    ag.ID,
			"bead_id":     candidate.ID,
			"project_id":  selectedProjectID,
			"provider_id": ag.ProviderID,
		}, execErr)

		historyJSON, loopDetected, loopReason := buildDispatchHistory(candidate, ag.ID)

		// Check if the error is due to max_iterations - if so, don't redispatch
		shouldRedispatch := "true"
		if candidate.Context != nil && candidate.Context["terminal_reason"] == "max_iterations" {
			shouldRedispatch = "false"
			log.Printf("[Dispatcher] Bead %s previously hit max_iterations, not redispatching after error", candidate.ID)
		}

		ctxUpdates := map[string]string{
			"last_run_at":          time.Now().UTC().Format(time.RFC3339),
			"last_run_error":       execErr.Error(),
			"agent_id":             ag.ID,
			"provider_id":          ag.ProviderID,
			"redispatch_requested": shouldRedispatch,
			"dispatch_history":     historyJSON,
			"loop_detected":        fmt.Sprintf("%t", loopDetected),
		}
		if loopDetected {
			ctxUpdates["loop_detected_reason"] = loopReason
			ctxUpdates["loop_detected_at"] = time.Now().UTC().Format(time.RFC3339)
		}
		updates := map[string]interface{}{"context": ctxUpdates}
		if loopDetected {
			triageAgent := d.findDefaultTriageAgent(candidate.ProjectID)
			updates["priority"] = models.BeadPriorityP0
			updates["status"] = models.BeadStatusOpen
			updates["assigned_to"] = triageAgent
			log.Printf("[Dispatcher] Loop detected for bead %s, reassigning to triage agent %s", candidate.ID, triageAgent)
		}
		if err := d.beads.UpdateBead(candidate.ID, updates); err != nil {
			log.Printf("[Dispatcher] CRITICAL: Failed to update bead %s with context/loop detection: %v", candidate.ID, err)
		}
		if d.eventBus != nil {
			status := string(models.BeadStatusInProgress)
			if loopDetected {
				status = string(models.BeadStatusOpen)
			}
			if err := d.eventBus.PublishBeadEvent(eventbus.EventTypeBeadStatusChange, candidate.ID, selectedProjectID, map[string]interface{}{"status": status}); err != nil {
				log.Printf("[Dispatcher] Warning: Failed to publish bead status change event for %s: %v", candidate.ID, err)
			}
		}

		// Handle workflow failure
		if d.workflowEngine != nil {
			execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(candidate.ID)
			if err == nil && execution != nil {
				// Report failure to workflow
				if err := d.workflowEngine.FailNode(execution.ID, ag.ID, execErr.Error()); err != nil {
					log.Printf("[Workflow] Failed to report failure to workflow for bead %s: %v", candidate.ID, err)
				} else {
					log.Printf("[Workflow] Reported failure to workflow for bead %s", candidate.ID)
				}
			}
		}

		return
	}

	ctxUpdates := map[string]string{
		"last_run_at":          time.Now().UTC().Format(time.RFC3339),
		"agent_id":             ag.ID,
		"provider_id":          ag.ProviderID,
		"provider_model":       d.providersModel(ag.ProviderID),
		"agent_output":         result.Response,
		"agent_tokens":         fmt.Sprintf("%d", result.TokensUsed),
		"agent_task_id":        result.TaskID,
		"agent_worker_id":      result.WorkerID,
		"redispatch_requested": "true",
	}

	// Store action loop metadata if the task used the action loop
	if result.LoopIterations > 0 {
		ctxUpdates["loop_iterations"] = fmt.Sprintf("%d", result.LoopIterations)
		ctxUpdates["terminal_reason"] = result.LoopTerminalReason

		// If the loop completed successfully, the agent finished the work
		if result.LoopTerminalReason == "completed" {
			ctxUpdates["redispatch_requested"] = "false"
		}

		// If the agent hit max_iterations, disable redispatch to prevent infinite loops
		// The agent couldn't finish the work within the iteration limit, so continuing
		// to redispatch will just waste resources. Instead, escalate or block the bead.
		if result.LoopTerminalReason == "max_iterations" {
			ctxUpdates["redispatch_requested"] = "false"
			ctxUpdates["max_iterations_reached_at"] = time.Now().UTC().Format(time.RFC3339)
			log.Printf("[Dispatcher] Bead %s hit max_iterations, disabling redispatch to prevent infinite loop", candidate.ID)
		}

		// On failure, set cooldown to prevent re-dispatching the same bead
		// 50 times in a single ralph beat
		switch result.LoopTerminalReason {
		case "parse_failures", "validation_failures", "error":
			ctxUpdates["last_failed_at"] = time.Now().UTC().Format(time.RFC3339)
		}
	}

	historyJSON, loopDetected, loopReason := buildDispatchHistory(candidate, ag.ID)
	ctxUpdates["dispatch_history"] = historyJSON
	ctxUpdates["loop_detected"] = fmt.Sprintf("%t", loopDetected)
	if loopDetected {
		ctxUpdates["loop_detected_reason"] = loopReason
		ctxUpdates["loop_detected_at"] = time.Now().UTC().Format(time.RFC3339)
	}

	updates := map[string]interface{}{"context": ctxUpdates}
	if loopDetected {
		triageAgent := d.findDefaultTriageAgent(candidate.ProjectID)
		updates["priority"] = models.BeadPriorityP0
		updates["status"] = models.BeadStatusOpen
		updates["assigned_to"] = triageAgent
		log.Printf("[Dispatcher] Task failure loop for bead %s, reassigning to triage agent %s", candidate.ID, triageAgent)
	}
	if err := d.beads.UpdateBead(candidate.ID, updates); err != nil {
		log.Printf("[Dispatcher] CRITICAL: Failed to update bead %s after task failure: %v", candidate.ID, err)
	}
	if d.eventBus != nil {
		status := string(models.BeadStatusInProgress)
		if loopDetected {
			status = string(models.BeadStatusOpen)
		}
		if err := d.eventBus.PublishBeadEvent(eventbus.EventTypeBeadStatusChange, candidate.ID, selectedProjectID, map[string]interface{}{"status": status}); err != nil {
			log.Printf("[Dispatcher] Warning: Failed to publish bead status change event for %s: %v", candidate.ID, err)
		}
	}

	// Advance workflow after successful task execution
	if d.workflowEngine != nil && !loopDetected {
		execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(candidate.ID)
		if err == nil && execution != nil {
			// Advance workflow with success condition
			resultData := map[string]string{
				"agent_id":    ag.ID,
				"output":      result.Response,
				"tokens_used": fmt.Sprintf("%d", result.TokensUsed),
			}
			if err := d.workflowEngine.AdvanceWorkflow(execution.ID, workflow.EdgeConditionSuccess, ag.ID, resultData); err != nil {
				log.Printf("[Workflow] Failed to advance workflow for bead %s: %v", candidate.ID, err)
			} else {
				// Get updated execution to check status
				updatedExec, _ := d.workflowEngine.GetDatabase().GetWorkflowExecution(execution.ID)
				if updatedExec != nil {
					log.Printf("[Workflow] Advanced workflow for bead %s: status=%s, node=%s, cycle=%d",
						candidate.ID, updatedExec.Status, updatedExec.CurrentNodeKey, updatedExec.CycleCount)

					// Check if workflow was escalated and needs CEO bead
					if updatedExec.Status == workflow.ExecutionStatusEscalated && candidate.Context["escalation_bead_created"] != "true" {
						log.Printf("[Workflow] Creating CEO escalation bead for workflow %s (bead %s)", updatedExec.ID, candidate.ID)

						// Get escalation info from workflow engine
						title, description, err := d.workflowEngine.GetEscalationInfo(updatedExec)
						if err != nil {
							log.Printf("[Workflow] Failed to get escalation info for workflow %s: %v", updatedExec.ID, err)
						} else {
							// Create CEO escalation bead
							createdBead, err := d.beads.CreateBead(
								title,
								description,
								models.BeadPriorityP0,
								"decision",
								candidate.ProjectID,
							)
							if err != nil {
								log.Printf("[Workflow] Failed to create CEO escalation bead: %v", err)
							} else {
								log.Printf("[Workflow] Created CEO escalation bead %s for workflow %s", createdBead.ID, updatedExec.ID)

								// Update the escalation bead with tags and context
								escalationBeadUpdates := map[string]interface{}{
									"tags": []string{"workflow-escalation", "ceo-review", "urgent"},
									"context": map[string]string{
										"original_bead_id":      candidate.ID,
										"workflow_execution_id": updatedExec.ID,
										"escalation_reason":     candidate.Context["escalation_reason"],
										"escalated_at":          time.Now().UTC().Format(time.RFC3339),
									},
								}
								if err := d.beads.UpdateBead(createdBead.ID, escalationBeadUpdates); err != nil {
									log.Printf("[Workflow] Failed to update escalation bead with tags and context: %v", err)
								}

								// Mark original bead as having escalation bead created
								originalUpdates := map[string]interface{}{
									"context": map[string]string{
										"escalation_bead_created": "true",
										"escalation_bead_id":      createdBead.ID,
									},
								}
								if err := d.beads.UpdateBead(candidate.ID, originalUpdates); err != nil {
									log.Printf("[Workflow] Failed to update original bead with escalation info: %v", err)
								}
							}
						}
					}
				}
			}
		}
	}

	d.setStatus(StatusParked, "idle")
	observability.Info("dispatch.execute", map[string]interface{}{
		"agent_id":    ag.ID,
		"bead_id":     candidate.ID,
		"project_id":  selectedProjectID,
		"provider_id": ag.ProviderID,
		"status":      "success",
	})
	}() // end async goroutine

	// Record dispatch metrics
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
			_ = json.Unmarshal([]byte(raw), &history)
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

	if execution != nil {
		// Bead already has a workflow
		return execution, nil
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

// estimateBeadComplexity analyzes a bead to estimate task complexity for smart provider routing.
// Simple tasks (review, check) go to small models; complex tasks (design, architect) go to large models.
func (d *Dispatcher) estimateBeadComplexity(bead *models.Bead) provider.ComplexityLevel {
	if d.complexityEstimator == nil {
		return provider.ComplexityMedium // Default fallback
	}

	// Start with type-based estimation
	typeComplexity := d.complexityEstimator.EstimateFromBeadType(string(bead.Type))

	// Analyze content for more specific estimation
	description := bead.Description
	if bead.Context != nil {
		// Include any agent output or error messages in complexity analysis
		if agentOutput, ok := bead.Context["agent_output"]; ok {
			description += " " + agentOutput
		}
		if errorMsg, ok := bead.Context["error_message"]; ok {
			description += " " + errorMsg
		}
	}
	contentComplexity := d.complexityEstimator.EstimateComplexity(bead.Title, description)

	// Combine estimates (take the higher one)
	result := d.complexityEstimator.CombineEstimates(typeComplexity, contentComplexity)

	// Priority escalation: P0 tasks get at least medium complexity treatment
	if bead.Priority == models.BeadPriorityP0 && result < provider.ComplexityMedium {
		result = provider.ComplexityMedium
	}

	// Decision beads always require complex reasoning
	if bead.Type == "decision" {
		result = provider.ComplexityComplex
	}

	return result
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
