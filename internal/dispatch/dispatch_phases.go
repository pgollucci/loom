package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/internal/observability"
	"github.com/jordanhubbard/loom/internal/worker"
	"github.com/jordanhubbard/loom/internal/workflow"
	"github.com/jordanhubbard/loom/pkg/messages"
	"github.com/jordanhubbard/loom/pkg/models"
)

// candidateSelection holds the result of the bead/agent selection phase.
type candidateSelection struct {
	Bead           *models.Bead
	Agent          *models.Agent
	SkippedReasons map[string]int
}

// sortReadyBeads sorts beads by priority (ascending) then by recency (descending).
// Nil beads are pushed to the end.
func sortReadyBeads(ready []*models.Bead) {
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
}

// filterIdleAgents takes a list of idle agents and returns only those with
// a healthy provider. Agents whose provider is inactive are reassigned from
// the active pool. Paused agents with a valid provider are promoted to idle.
func (d *Dispatcher) filterIdleAgents(idleAgents []*models.Agent) []*models.Agent {
	filtered := make([]*models.Agent, 0, len(idleAgents))
	for _, candidateAgent := range idleAgents {
		if candidateAgent == nil {
			continue
		}
		needsProvider := candidateAgent.ProviderID == "" ||
			!d.providers.IsActive(candidateAgent.ProviderID)
		if needsProvider {
			activeProviders := d.providers.ListActive()
			if len(activeProviders) > 0 {
				best := activeProviders[0]
				prev := candidateAgent.ProviderID
				candidateAgent.ProviderID = best.Config.ID
				if prev != "" {
					log.Printf("[Dispatcher] Reassigned agent %s from failed provider %s to %s",
						candidateAgent.Name, prev, best.Config.ID)
				} else {
					log.Printf("[Dispatcher] Auto-assigned provider %s to agent %s",
						best.Config.ID, candidateAgent.Name)
				}
			} else {
				continue
			}
		}
		if candidateAgent.Status == "paused" {
			candidateAgent.Status = "idle"
			log.Printf("[Dispatcher] Promoted agent %s from paused to idle", candidateAgent.Name)
		}
		filtered = append(filtered, candidateAgent)
	}
	return filtered
}

// buildAgentMaps builds ID-keyed maps for idle agents and all project agents.
func (d *Dispatcher) buildAgentMaps(projectID string, idleAgents []*models.Agent) (idleByID, allByID map[string]*models.Agent) {
	idleByID = make(map[string]*models.Agent, len(idleAgents))
	for _, a := range idleAgents {
		if a != nil {
			idleByID[a.ID] = a
		}
	}

	allAgents := d.agents.ListAgentsByProject(projectID)
	if len(allAgents) == 0 {
		allAgents = d.agents.ListAgents()
	}
	allByID = make(map[string]*models.Agent, len(allAgents))
	for _, a := range allAgents {
		if a != nil {
			allByID[a.ID] = a
		}
	}
	return
}

// beadSkipCheck evaluates whether a bead should be skipped during candidate
// selection. It returns (skip bool, reason string). Pure logic; does not
// mutate state or call external services.
func beadSkipCheck(b *models.Bead, maxHops int) (skip bool, reason string) {
	if b == nil {
		return true, "nil_bead"
	}
	if b.Type == "decision" {
		return true, "decision_type"
	}

	if b.Context != nil {
		switch b.Context["terminal_reason"] {
		case "parse_failures", "max_iterations":
			return true, "terminal_" + b.Context["terminal_reason"]
		case "completed":
			return true, "terminal_completed"
		}
	}

	// Skip beads that recently failed (cooldown)
	if b.Context != nil && b.Context["last_failed_at"] != "" {
		if lastFailed, err := time.Parse(time.RFC3339, b.Context["last_failed_at"]); err == nil {
			if time.Since(lastFailed) < 2*time.Minute {
				return true, "cooldown_after_failure"
			}
		}
	}

	// Skip beads that completed a terminal status (done/closed/cancelled).
	// Beads still in "open" are eligible for retry even if they ran before —
	// the agent may have terminated without finishing (stagnant, parse failure, etc.).
	if b.Context != nil && b.Context["last_run_at"] != "" {
		switch b.Status {
		case "done", "closed", "cancelled":
			if b.Context["redispatch_requested"] != "true" {
				return true, "already_run"
			}
		case "open":
			// Open beads get a retry cooldown to avoid hammering the LLM
			if lastRun, err := time.Parse(time.RFC3339, b.Context["last_run_at"]); err == nil {
				if time.Since(lastRun) < 30*time.Second {
					return true, "retry_cooldown"
				}
			}
		}
	}

	return false, ""
}

// dispatchCountForBead reads the dispatch_count from a bead's context.
func dispatchCountForBead(b *models.Bead) int {
	if b == nil || b.Context == nil {
		return 0
	}
	count := 0
	if countStr := b.Context["dispatch_count"]; countStr != "" {
		_, _ = fmt.Sscanf(countStr, "%d", &count)
	}
	return count
}

// checkHopLimit evaluates whether a bead has exceeded its dispatch hop limit.
// Returns (exceeded bool, stuck bool, reason string).
// If exceeded is true and stuck is false, the bead is making progress and
// should still be dispatched.
func (d *Dispatcher) checkHopLimit(b *models.Bead, dispatchCount, maxHops int) (exceeded, stuck bool, reason string) {
	if dispatchCount < maxHops {
		return false, false, ""
	}

	if b.Context != nil && b.Context["escalated_to_ceo_decision_id"] != "" {
		return true, true, "dispatch_limit_escalated"
	}

	isStuck, loopReason := d.loopDetector.IsStuckInLoop(b)
	if !isStuck {
		log.Printf("[Dispatcher] Bead %s has %d dispatches but is making progress",
			b.ID, dispatchCount)
		return true, false, "dispatch_limit_but_progressing"
	}

	return true, true, loopReason
}

// ralphAutoBlock blocks a bead that is stuck in a dispatch loop. It updates
// the bead status to blocked, tags it with revert recommendations, and
// reassigns it to a triage agent.
func (d *Dispatcher) ralphAutoBlock(ctx context.Context, b *models.Bead, dispatchCount, maxHops int, loopReason string) {
	reason := fmt.Sprintf("dispatch_count=%d exceeded max_hops=%d, stuck in loop: %s",
		dispatchCount, maxHops, loopReason)
	log.Printf("[Ralph] Bead %s is stuck after %d dispatches, auto-blocking: %s",
		b.ID, dispatchCount, loopReason)

	progressSummary := d.loopDetector.GetProgressSummary(b)

	revertStatus := "not_attempted"
	firstSHA, _, commitCount := d.loopDetector.GetAgentCommitRange(b)
	if firstSHA != "" && commitCount > 0 {
		log.Printf("[Ralph] Attempting auto-revert of %d agent commits for bead %s (from %s)",
			commitCount, b.ID, firstSHA)
		revertStatus = fmt.Sprintf("revert_recommended: %d commits from %s", commitCount, firstSHA)
	}

	ctxUpdates := map[string]string{
		"redispatch_requested":  "false",
		"ralph_blocked_at":      time.Now().UTC().Format(time.RFC3339),
		"ralph_blocked_reason":  reason,
		"loop_detection_reason": loopReason,
		"progress_summary":      progressSummary,
		"revert_status":         revertStatus,
	}
	if b.Context != nil {
		if sessionID := b.Context["conversation_session_id"]; sessionID != "" {
			ctxUpdates["conversation_session_id"] = sessionID
		}
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
}

// matchAssignedAgent checks if the bead is already assigned and whether
// that agent is idle. Returns (matched *models.Agent, skip bool, reason string).
func matchAssignedAgent(b *models.Bead, idleByID, allByID map[string]*models.Agent) (matched *models.Agent, skip bool, reason string) {
	if b.AssignedTo == "" {
		return nil, false, ""
	}

	if _, exists := allByID[b.AssignedTo]; !exists {
		return nil, false, "dead_agent"
	}

	idle, ok := idleByID[b.AssignedTo]
	if !ok {
		return nil, true, "assigned_agent_not_idle"
	}
	return idle, false, ""
}

// matchAgentForBead picks the best idle agent for a bead, considering persona
// hints and preferring the engineering-manager role as a default.
func (d *Dispatcher) matchAgentForBead(b *models.Bead, idleAgents []*models.Agent) *models.Agent {
	// Try persona-based routing first
	personaHint := d.personaMatcher.ExtractPersonaHint(b)
	if personaHint != "" {
		matchedAgent := d.personaMatcher.FindAgentByPersonaHint(personaHint, idleAgents)
		if matchedAgent != nil {
			log.Printf("[Dispatcher] Matched bead %s to agent %s via persona hint '%s'",
				b.ID, matchedAgent.Name, personaHint)
			return matchedAgent
		}
		log.Printf("[Dispatcher] Bead %s has persona hint '%s' but no exact match",
			b.ID, personaHint)
	}

	// Prefer engineering manager, fallback to any project-compatible agent
	var matchedAgent, fallbackAgent *models.Agent
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
	return matchedAgent
}

// selectCandidate iterates through ready beads and picks the first one that
// can be dispatched along with its matching agent.
func (d *Dispatcher) selectCandidate(
	ctx context.Context,
	ready []*models.Bead,
	idleAgents []*models.Agent,
	idleByID, allByID map[string]*models.Agent,
) candidateSelection {
	skippedReasons := make(map[string]int)

	maxHops := d.maxDispatchHops
	if maxHops <= 0 {
		maxHops = 20
	}

	for _, b := range ready {
		if b == nil {
			skippedReasons["nil_bead"]++
			continue
		}

		// Skip beads that already have an in-flight goroutine executing them
		d.inflightMu.Lock()
		_, running := d.inflight[b.ID]
		d.inflightMu.Unlock()
		if running {
			skippedReasons["already_inflight"]++
			continue
		}

		if d.hasTag(b, "requires-human-config") {
			skippedReasons["requires_human_config"]++
			continue
		}

		// Auto-bug routing
		if routeInfo := d.autoBugRouter.AnalyzeBugForRouting(b); routeInfo.ShouldRoute {
			log.Printf("[Dispatcher] Auto-bug detected: %s - routing to %s (%s)",
				b.ID, routeInfo.PersonaHint, routeInfo.RoutingReason)
			if d.beads != nil {
				updates := map[string]interface{}{"title": routeInfo.UpdatedTitle}
				if err := d.beads.UpdateBead(b.ID, updates); err != nil {
					log.Printf("[Dispatcher] Failed to update bead %s with persona hint: %v", b.ID, err)
				} else {
					b.Title = routeInfo.UpdatedTitle
				}
			}
		}

		skip, reason := beadSkipCheck(b, maxHops)
		if skip {
			skippedReasons[reason]++
			continue
		}

		// Enable auto-redispatch for open/in-progress beads
		if b.Status == models.BeadStatusOpen || b.Status == models.BeadStatusInProgress {
			if b.Context == nil {
				b.Context = make(map[string]string)
			}
			if b.Context["redispatch_requested"] != "true" {
				b.Context["redispatch_requested"] = "true"
				b.Context["redispatch_requested_at"] = time.Now().UTC().Format(time.RFC3339)
				if d.beads != nil {
					if err := d.beads.UpdateBead(b.ID, map[string]interface{}{"context": b.Context}); err != nil {
						log.Printf("[Dispatcher] Failed to auto-enable redispatch for bead %s: %v", b.ID, err)
					}
				}
			}
		}

		dispatchCount := dispatchCountForBead(b)

		// Hard upper bound: no bead may exceed maxHops*10 regardless of
		// loop detection result. This catches runaway dispatches when all
		// other guards fail (e.g. bd-106: 11,329 dispatches).
		hardLimit := maxHops * 10
		if hardLimit < 200 {
			hardLimit = 200
		}
		if dispatchCount >= hardLimit {
			log.Printf("[Dispatcher] HARD LIMIT: Bead %s hit %d dispatches (hard_limit=%d), force-blocking",
				b.ID, dispatchCount, hardLimit)
			d.ralphAutoBlock(ctx, b, dispatchCount, maxHops, "hard_dispatch_limit_exceeded")
			skippedReasons["hard_dispatch_limit"]++
			continue
		}

		exceeded, stuck, hopReason := d.checkHopLimit(b, dispatchCount, maxHops)
		if exceeded {
			if stuck {
				d.ralphAutoBlock(ctx, b, dispatchCount, maxHops, hopReason)
				skippedReasons["ralph_auto_blocked"]++
				continue
			}
			skippedReasons["dispatch_limit_but_progressing"]++
		}

		if dispatchCount > maxHops {
			log.Printf("[Dispatcher] Bead %s dispatch_count=%d exceeds max_hops=%d — progressing but at risk",
				b.ID, dispatchCount, maxHops)
		} else if dispatchCount >= maxHops-1 {
			log.Printf("[Dispatcher] WARNING: Bead %s has been dispatched %d times", b.ID, dispatchCount)
		}

		// Check assigned agent
		matched, skipAssigned, assignReason := matchAssignedAgent(b, idleByID, allByID)
		if assignReason == "dead_agent" {
			log.Printf("[Dispatcher] Bead %s assigned to dead agent %s, clearing assignment", b.ID, b.AssignedTo)
			if d.beads != nil {
				updates := map[string]interface{}{
					"assigned_to": "",
					"status":      models.BeadStatusOpen,
				}
				if err := d.beads.UpdateBead(b.ID, updates); err != nil {
					log.Printf("[Dispatcher] Failed to clear dead agent assignment for bead %s: %v", b.ID, err)
				} else {
					b.AssignedTo = ""
					skippedReasons["dead_agent_cleared"]++
				}
			} else {
				b.AssignedTo = ""
				skippedReasons["dead_agent_cleared"]++
			}
		} else if skipAssigned {
			// Assigned agent is busy — skip regardless of bead status.
			// An open bead assigned to a busy agent should not be reassigned
			// to a different agent; doing so causes an infinite loop where
			// every dispatch cycle selects this bead, attempts to claim it,
			// fails (because the original agent is working on it), and never
			// advances to other ready beads.
			skippedReasons["assigned_agent_busy"]++
			continue
		} else if matched != nil {
			return candidateSelection{Bead: b, Agent: matched, SkippedReasons: skippedReasons}
		}

		// Workflow role matching (only for beads that explicitly opt in)
		enforceWorkflow := false
		if b.Tags != nil {
			for _, tag := range b.Tags {
				if tag == "workflow-required" || tag == "strict-workflow" {
					enforceWorkflow = true
					break
				}
			}
		}

		if d.workflowEngine != nil && enforceWorkflow {
			execution, err := d.ensureBeadHasWorkflow(ctx, b)
			if err != nil {
				log.Printf("[Workflow] Error ensuring workflow for bead %s: %v", b.ID, err)
			} else if execution != nil {
				isReady := d.workflowEngine.IsNodeReady(execution)
				if !isReady && execution.Status != "escalated" {
					skippedReasons["workflow_node_not_ready"]++
					continue
				}

				workflowRole := d.getWorkflowRoleRequirement(execution)
				if workflowRole != "" {
					requiredRoleKey := normalizeRoleName(workflowRole)
					for _, agent := range idleAgents {
						if agent != nil && normalizeRoleName(agent.Role) == requiredRoleKey {
							return candidateSelection{Bead: b, Agent: agent, SkippedReasons: skippedReasons}
						}
					}
					skippedReasons["workflow_role_not_available"]++
					continue
				}
			}
		}

		// General agent matching
		ag := d.matchAgentForBead(b, idleAgents)
		if ag == nil {
			skippedReasons["no_idle_agents_for_project"]++
			continue
		}
		return candidateSelection{Bead: b, Agent: ag, SkippedReasons: skippedReasons}
	}

	return candidateSelection{SkippedReasons: skippedReasons}
}

// selectProviderForTask chooses a provider based on task complexity using
// round-robin across healthy providers. Returns the selected provider ID
// or empty string if none available.
func (d *Dispatcher) selectProviderForTask(candidate *models.Bead, ag *models.Agent) string {
	activeProviders := d.providers.ListActive()
	if len(activeProviders) == 0 {
		return ""
	}
	return activeProviders[0].Config.ID
}

// claimAndAssign claims the bead for the agent, increments the dispatch count,
// and records the agent assignment.
func (d *Dispatcher) claimAndAssign(candidate *models.Bead, ag *models.Agent, selectedProjectID string) error {
	if candidate.AssignedTo == "" {
		if err := d.beads.ClaimBead(candidate.ID, ag.ID); err != nil {
			observability.Error("dispatch.claim", map[string]interface{}{
				"agent_id":   ag.ID,
				"bead_id":    candidate.ID,
				"project_id": candidate.ProjectID,
			}, err)
			return fmt.Errorf("claim bead: %w", err)
		}
		observability.Info("dispatch.claim", map[string]interface{}{
			"agent_id":   ag.ID,
			"bead_id":    candidate.ID,
			"project_id": candidate.ProjectID,
		})
	} else if candidate.AssignedTo != ag.ID {
		// Bead is already assigned to a different agent and in_progress.
		// Only re-assign if that original agent is no longer "working" on it
		// (i.e. it's gone idle or the bead was orphaned).
		originalAgentWorking := false
		if workerAgent, err := d.agents.GetAgent(candidate.AssignedTo); err == nil && workerAgent != nil {
			if workerAgent.Status == "working" && workerAgent.CurrentBead == candidate.ID {
				originalAgentWorking = true
			}
		}
		if originalAgentWorking {
			log.Printf("[Dispatcher] Bead %s already in progress by agent %s — skipping re-dispatch to %s",
				candidate.ID, candidate.AssignedTo, ag.ID)
			return fmt.Errorf("bead already in progress by %s", candidate.AssignedTo)
		}
		// Original agent is gone — forcibly reassign to the new agent.
		if err := d.beads.ReassignBead(candidate.ID, ag.ID, candidate.AssignedTo); err != nil {
			return fmt.Errorf("re-claim bead: %w", err)
		}
		log.Printf("[Dispatcher] Re-claimed bead %s from stale agent %s → %s",
			candidate.ID, candidate.AssignedTo, ag.ID)
	}

	dispatchCount := dispatchCountForBead(candidate) + 1
	countUpdates := map[string]interface{}{
		"context": map[string]string{
			"dispatch_count": fmt.Sprintf("%d", dispatchCount),
		},
	}
	if err := d.beads.UpdateBead(candidate.ID, countUpdates); err != nil {
		log.Printf("[Dispatcher] WARNING: Failed to update dispatch count for bead %s: %v", candidate.ID, err)
	}
	log.Printf("[Dispatcher] Bead %s dispatch count: %d", candidate.ID, dispatchCount)

	if err := d.agents.AssignBead(ag.ID, candidate.ID); err != nil {
		log.Printf("[Dispatcher] CRITICAL: Failed to assign bead %s to agent %s: %v", candidate.ID, ag.ID, err)
	}
	observability.Info("dispatch.assign", map[string]interface{}{
		"agent_id":    ag.ID,
		"bead_id":     candidate.ID,
		"project_id":  selectedProjectID,
		"provider_id": ag.ProviderID,
	})

	return nil
}

// publishDispatchedTask publishes the dispatched task to NATS and emits
// event bus notifications.
func (d *Dispatcher) publishDispatchedTask(ctx context.Context, candidate *models.Bead, ag *models.Agent, selectedProjectID string, dispatchCount int) {
	if d.messageBus != nil {
		correlationID := fmt.Sprintf("dispatch-%s-%d", candidate.ID, time.Now().UnixNano())

		// Determine work directory — use worktree if project supports parallel agents.
		workDir := ""
		if d.worktreeManager != nil {
			proj, _ := d.projects.GetProject(selectedProjectID)
			if proj != nil && proj.UseWorktrees {
				if wtPath, err := d.worktreeManager.SetupAgentWorktree(selectedProjectID, candidate.ID, proj.Branch); err != nil {
					log.Printf("[Dispatcher] Failed to setup worktree for bead %s: %v", candidate.ID, err)
				} else {
					workDir = wtPath
					log.Printf("[Dispatcher] Allocated worktree %s for bead %s", wtPath, candidate.ID)
				}
			}
		}

		taskMsg := messages.TaskAssigned(
			selectedProjectID,
			candidate.ID,
			ag.ID,
			messages.TaskData{
				Title:       candidate.Title,
				Description: candidate.Description,
				Priority:    int(candidate.Priority),
				Type:        string(candidate.Type),
				WorkDir:     workDir,
				Context: map[string]interface{}{
					"status":       string(candidate.Status),
					"assigned_at":  time.Now().UTC().Format(time.RFC3339),
					"dispatch_hop": dispatchCount,
				},
			},
			correlationID,
		)

		targetRole := d.inferAgentRole(ag, candidate)
		var publishErr error
		if targetRole != "" {
			publishErr = d.messageBus.PublishTaskForRole(ctx, selectedProjectID, targetRole, taskMsg)
		} else {
			publishErr = d.messageBus.PublishTask(ctx, selectedProjectID, taskMsg)
		}

		if publishErr != nil {
			log.Printf("[Dispatcher] Warning: Failed to publish task to NATS for bead %s: %v", candidate.ID, publishErr)
		} else {
			log.Printf("[Dispatcher] Published task to NATS: bead=%s agent=%s role=%s correlation=%s",
				candidate.ID, ag.ID, targetRole, correlationID)
		}
	}

	if d.eventBus != nil {
		_ = d.eventBus.PublishBeadEvent("bead.assigned", candidate.ID, selectedProjectID,
			map[string]interface{}{"assigned_to": ag.ID})
		_ = d.eventBus.PublishBeadEvent("bead.status_change", candidate.ID, selectedProjectID,
			map[string]interface{}{"status": string(models.BeadStatusInProgress)})
	}
}

// processTaskError handles the aftermath of a failed task execution,
// including bead context updates, loop detection, and workflow failure.
func (d *Dispatcher) processTaskError(candidate *models.Bead, ag *models.Agent, selectedProjectID string, execErr error) {
	// Reset bead to open for transient errors so it can be re-dispatched.
	// Without this, any early return leaves the bead in in_progress with
	// assigned_to still set — a zombie that no future dispatch will ever pick up.
	resetBeadToOpen := func() {
		if err := d.beads.UpdateBead(candidate.ID, map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
		}); err != nil {
			log.Printf("[Dispatcher] Warning: failed to reset bead %s to open after error: %v", candidate.ID, err)
		}
	}

	// Context cancellation: the dispatcher's parent context was cancelled (e.g.
	// loom shutdown, NATS disconnect). Reset the bead so it is immediately
	// re-dispatchable on the next heartbeat.
	if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
		log.Printf("[Dispatcher] Bead %s resetting to open after context cancellation: %v", candidate.ID, execErr)
		resetBeadToOpen()
		return
	}

	// Provider error (5xx, 502, budget exceeded, etc.): the provider is
	// temporarily unhealthy. Reset the bead so Ralph can re-dispatch it once
	// the provider recovers. Previously this returned early without resetting,
	// leaving beads permanently stuck as in_progress (bd-012).
	if isProviderError(execErr.Error()) {
		log.Printf("[Dispatcher] Bead %s resetting to open after provider error: %v", candidate.ID, execErr)
		resetBeadToOpen()
		return
	}

	d.setStatus(StatusParked, "execution failed")
	observability.Error("dispatch.execute", map[string]interface{}{
		"agent_id":    ag.ID,
		"bead_id":     candidate.ID,
		"project_id":  selectedProjectID,
		"provider_id": ag.ProviderID,
	}, execErr)

	historyJSON, loopDetected, loopReason := buildDispatchHistory(candidate, ag.ID)

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
		_ = d.eventBus.PublishBeadEvent("bead.status_change", candidate.ID, selectedProjectID,
			map[string]interface{}{"status": status})
	}

	d.advanceWorkflowOnFailure(candidate, ag.ID, execErr)
}

// processTaskSuccess handles the aftermath of a successful task execution,
// including bead context updates, loop metadata, and workflow advancement.
func (d *Dispatcher) processTaskSuccess(candidate *models.Bead, ag *models.Agent, selectedProjectID string, result *worker.TaskResult) {
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

	d.applyLoopMetadata(ctxUpdates, candidate, ag, result)

	historyJSON, loopDetected, loopReason := buildDispatchHistory(candidate, ag.ID)
	ctxUpdates["dispatch_history"] = historyJSON
	ctxUpdates["loop_detected"] = fmt.Sprintf("%t", loopDetected)
	if loopDetected {
		ctxUpdates["loop_detected_reason"] = loopReason
		ctxUpdates["loop_detected_at"] = time.Now().UTC().Format(time.RFC3339)
	}

	updates := map[string]interface{}{"context": ctxUpdates}
	// Check for successful completion FIRST — if the agent signaled done, close the bead
	// regardless of dispatch history. The agent completed its work successfully.
	if result.LoopTerminalReason == "completed" {
		// Agent signaled "done" — close the bead so it won't be re-dispatched.
		updates["status"] = models.BeadStatusClosed
		updates["assigned_to"] = ""
		log.Printf("[Dispatcher] Bead %s completed (agent signaled done), closing", candidate.ID)
	} else if loopDetected {
		// Dispatch-level loop detected (alternating agents) — reassign to triage.
		// This only triggers if the agent did NOT complete successfully.
		triageAgent := d.findDefaultTriageAgent(candidate.ProjectID)
		updates["priority"] = models.BeadPriorityP0
		updates["status"] = models.BeadStatusOpen
		updates["assigned_to"] = triageAgent
		log.Printf("[Dispatcher] Task failure loop for bead %s, reassigning to triage agent %s", candidate.ID, triageAgent)
	} else if result.LoopTerminalReason == "inner_loop" || result.LoopTerminalReason == "progress_stagnant" {
		// Agent is stuck — move bead back to open so it is not immediately re-dispatched.
		// A remediation bead has already been created by applyLoopMetadata; the original
		// bead must be cleared from in_progress to prevent the dispatcher from spinning on
		// it again before the remediation is addressed.
		ctxUpdates["redispatch_requested"] = "false"
		ctxUpdates["stuck_at"] = time.Now().UTC().Format(time.RFC3339)
		updates["status"] = models.BeadStatusOpen
		updates["assigned_to"] = ""
		log.Printf("[Dispatcher] Bead %s stuck (%s), opening for re-triage (remediation bead created)",
			candidate.ID, result.LoopTerminalReason)
	}
	if err := d.beads.UpdateBead(candidate.ID, updates); err != nil {
		log.Printf("[Dispatcher] CRITICAL: Failed to update bead %s after task: %v", candidate.ID, err)
	}

	if d.eventBus != nil {
		status := string(models.BeadStatusInProgress)
		// Mirror the condition order above: completed takes precedence over loopDetected
		if result.LoopTerminalReason == "completed" {
			status = string(models.BeadStatusClosed)
		} else if loopDetected {
			status = string(models.BeadStatusOpen)
		} else if result.LoopTerminalReason == "inner_loop" || result.LoopTerminalReason == "progress_stagnant" {
			status = string(models.BeadStatusOpen)
		}
		_ = d.eventBus.PublishBeadEvent("bead.status_change", candidate.ID, selectedProjectID,
			map[string]interface{}{"status": status})
	}

	// Advance workflow only if not in a dispatch loop AND not completed
	// (completed beads don't need workflow advancement)
	if !loopDetected && result.LoopTerminalReason != "completed" {
		d.advanceWorkflowOnSuccess(candidate, ag.ID, result)
	}

	d.setStatus(StatusParked, "idle")
	observability.Info("dispatch.execute", map[string]interface{}{
		"agent_id":    ag.ID,
		"bead_id":     candidate.ID,
		"project_id":  selectedProjectID,
		"provider_id": ag.ProviderID,
		"status":      "success",
	})
}

// applyLoopMetadata enriches context updates with action loop metadata
// (iteration count, terminal reason, cooldown, remediation).
func (d *Dispatcher) applyLoopMetadata(ctxUpdates map[string]string, candidate *models.Bead, ag *models.Agent, result *worker.TaskResult) {
	// Check if the result contains a provider error
	if result.Error != "" && isProviderError(result.Error) {
		log.Printf("[Dispatcher] Skipping loop metadata application for provider error: %v", result.Error)
		return
	}
	if result.LoopIterations <= 0 {
		return
	}

	ctxUpdates["loop_iterations"] = fmt.Sprintf("%d", result.LoopIterations)
	ctxUpdates["terminal_reason"] = result.LoopTerminalReason

	if result.LoopTerminalReason == "completed" {
		ctxUpdates["redispatch_requested"] = "false"
	}

	if result.LoopTerminalReason == "max_iterations" {
		maxIterRetries := 0
		if candidate.Context != nil {
			if retriesStr, ok := candidate.Context["max_iterations_retries"]; ok {
				fmt.Sscanf(retriesStr, "%d", &maxIterRetries)
			}
		}
		if maxIterRetries == 0 {
			ctxUpdates["redispatch_requested"] = "true"
			ctxUpdates["max_iterations_retries"] = "1"
			ctxUpdates["max_iterations_reached_at"] = time.Now().UTC().Format(time.RFC3339)
			log.Printf("[Dispatcher] Bead %s hit max_iterations (first time), allowing one retry", candidate.ID)
		} else {
			ctxUpdates["redispatch_requested"] = "false"
			ctxUpdates["max_iterations_retry_exhausted"] = "true"
			log.Printf("[Dispatcher] Bead %s hit max_iterations again after retry, disabling redispatch", candidate.ID)
		}
	}

	switch result.LoopTerminalReason {
	case "parse_failures", "validation_failures", "error":
		ctxUpdates["last_failed_at"] = time.Now().UTC().Format(time.RFC3339)
	case "progress_stagnant", "inner_loop":
		ctxUpdates["last_failed_at"] = time.Now().UTC().Format(time.RFC3339)
		ctxUpdates["remediation_needed"] = "true"
		ctxUpdates["remediation_requested_at"] = time.Now().UTC().Format(time.RFC3339)
		log.Printf("[Dispatcher] Agent stuck on bead %s (reason: %s), remediation needed",
			candidate.ID, result.LoopTerminalReason)
		go d.createRemediationBead(candidate, ag, result)
	}
}

// filterBeadsByReadiness applies project readiness checks to a list of beads.
// In block mode, only beads from ready projects are returned.
// In warn mode, all beads pass through (readiness is just logged).
func (d *Dispatcher) filterBeadsByReadiness(
	ctx context.Context,
	ready []*models.Bead,
	readinessCheck func(context.Context, string) (bool, []string),
	mode ReadinessMode,
) []*models.Bead {
	if readinessCheck == nil {
		return ready
	}

	projectReadiness := make(map[string]bool)

	if mode == ReadinessBlock {
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
		return filtered
	}

	// Warn mode: just evaluate readiness (for logging), return all beads
	for _, bead := range ready {
		if bead == nil {
			continue
		}
		if _, ok := projectReadiness[bead.ProjectID]; !ok {
			okReady, _ := readinessCheck(ctx, bead.ProjectID)
			projectReadiness[bead.ProjectID] = okReady
		}
	}
	return ready
}

// advanceWorkflowOnFailure reports a task failure to the workflow engine.
func (d *Dispatcher) advanceWorkflowOnFailure(candidate *models.Bead, agentID string, execErr error) {
	if d.workflowEngine == nil {
		return
	}
	execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(candidate.ID)
	if err != nil || execution == nil {
		return
	}

	failCondition := workflow.EdgeConditionFailure
	if currentNode, nodeErr := d.workflowEngine.GetCurrentNode(execution.ID); nodeErr == nil && currentNode != nil {
		if currentNode.NodeType == workflow.NodeTypeApproval || currentNode.NodeType == workflow.NodeTypeVerify {
			failCondition = workflow.EdgeConditionRejected
			log.Printf("[Workflow] %s node %s failed — advancing with 'rejected'",
				currentNode.NodeType, currentNode.NodeKey)
		}
	}

	resultData := map[string]string{"failure_reason": execErr.Error()}
	if err := d.workflowEngine.AdvanceWorkflow(execution.ID, failCondition, agentID, resultData); err != nil {
		log.Printf("[Workflow] Failed to report failure to workflow for bead %s: %v", candidate.ID, err)
	} else {
		log.Printf("[Workflow] Reported failure to workflow for bead %s (condition: %s)",
			candidate.ID, failCondition)
	}
}

// advanceWorkflowOnSuccess reports a successful task completion to the
// workflow engine and handles escalation bead creation if needed.
func (d *Dispatcher) advanceWorkflowOnSuccess(candidate *models.Bead, agentID string, result *worker.TaskResult) {
	if d.workflowEngine == nil {
		return
	}
	execution, err := d.workflowEngine.GetDatabase().GetWorkflowExecutionByBeadID(candidate.ID)
	if err != nil || execution == nil {
		return
	}

	advanceCondition := workflow.EdgeConditionSuccess
	if currentNode, nodeErr := d.workflowEngine.GetCurrentNode(execution.ID); nodeErr == nil && currentNode != nil {
		switch currentNode.NodeType {
		case workflow.NodeTypeApproval, workflow.NodeTypeVerify:
			advanceCondition = workflow.EdgeConditionApproved
			log.Printf("[Workflow] %s node %s completed by agent %s — advancing with 'approved'",
				currentNode.NodeType, currentNode.NodeKey, agentID)
		}
	}

	resultData := map[string]string{
		"agent_id":    agentID,
		"output":      result.Response,
		"tokens_used": fmt.Sprintf("%d", result.TokensUsed),
	}
	if err := d.workflowEngine.AdvanceWorkflow(execution.ID, advanceCondition, agentID, resultData); err != nil {
		log.Printf("[Workflow] Failed to advance workflow for bead %s: %v", candidate.ID, err)
		return
	}

	updatedExec, _ := d.workflowEngine.GetDatabase().GetWorkflowExecution(execution.ID)
	if updatedExec == nil {
		return
	}
	log.Printf("[Workflow] Advanced workflow for bead %s: status=%s, node=%s, cycle=%d",
		candidate.ID, updatedExec.Status, updatedExec.CurrentNodeKey, updatedExec.CycleCount)

	if updatedExec.Status == workflow.ExecutionStatusEscalated && (candidate.Context == nil || candidate.Context["escalation_bead_created"] != "true") {
		d.createEscalationBead(candidate, updatedExec)
	}
}

// createEscalationBead creates a CEO escalation bead when a workflow is escalated.
func (d *Dispatcher) createEscalationBead(candidate *models.Bead, execution *workflow.WorkflowExecution) {
	log.Printf("[Workflow] Creating CEO escalation bead for workflow %s (bead %s)", execution.ID, candidate.ID)

	title, description, err := d.workflowEngine.GetEscalationInfo(execution)
	if err != nil {
		log.Printf("[Workflow] Failed to get escalation info for workflow %s: %v", execution.ID, err)
		return
	}

	createdBead, err := d.beads.CreateBead(title, description, models.BeadPriorityP0, "decision", candidate.ProjectID)
	if err != nil {
		log.Printf("[Workflow] Failed to create CEO escalation bead: %v", err)
		return
	}
	log.Printf("[Workflow] Created CEO escalation bead %s for workflow %s", createdBead.ID, execution.ID)

	escalationReason := ""
	if candidate.Context != nil {
		escalationReason = candidate.Context["escalation_reason"]
	}

	escalationUpdates := map[string]interface{}{
		"tags": []string{"workflow-escalation", "ceo-review", "urgent"},
		"context": map[string]string{
			"original_bead_id":      candidate.ID,
			"workflow_execution_id": execution.ID,
			"escalation_reason":     escalationReason,
			"escalated_at":          time.Now().UTC().Format(time.RFC3339),
		},
	}
	if err := d.beads.UpdateBead(createdBead.ID, escalationUpdates); err != nil {
		log.Printf("[Workflow] Failed to update escalation bead with tags and context: %v", err)
	}

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

// checkProjectReadiness evaluates a single project's readiness and returns
// early with a non-dispatched result if it fails in block mode.
func (d *Dispatcher) checkProjectReadiness(ctx context.Context, projectID string) (blocked bool, result *DispatchResult) {
	d.mu.RLock()
	readinessCheck := d.readinessCheck
	readinessMode := d.readinessMode
	d.mu.RUnlock()

	if readinessCheck == nil || projectID == "" {
		return false, nil
	}

	readyOK, issues := readinessCheck(ctx, projectID)
	if !readyOK && readinessMode == ReadinessBlock {
		reason := "project readiness failed"
		if len(issues) > 0 {
			reason = fmt.Sprintf("project readiness failed: %s", strings.Join(issues, "; "))
		}
		d.setStatus(StatusParked, reason)
		return true, &DispatchResult{Dispatched: false, ProjectID: projectID, Error: reason}
	}
	return false, nil
}
