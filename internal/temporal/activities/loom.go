package activities

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/internal/agent"
	"github.com/jordanhubbard/loom/internal/beads"
	"github.com/jordanhubbard/loom/internal/database"
	"github.com/jordanhubbard/loom/internal/dispatch"
	"github.com/jordanhubbard/loom/pkg/models"
)

const maxDispatchesPerBeat = 50

// LoomActivities supplies activities for the Ralph Loop heartbeat.
type LoomActivities struct {
	database   *database.Database
	dispatcher *dispatch.Dispatcher
	beadsMgr   *beads.Manager
	agentMgr   *agent.WorkerManager
}

func NewLoomActivities(db *database.Database, d *dispatch.Dispatcher, b *beads.Manager, a *agent.WorkerManager) *LoomActivities {
	return &LoomActivities{
		database:   db,
		dispatcher: d,
		beadsMgr:   b,
		agentMgr:   a,
	}
}

// LoomHeartbeatActivity is the Ralph Loop — the relentless work-draining engine.
// Each beat: resets stuck agents, resolves stuck beads, then drains all
// dispatchable work by calling DispatchOnce in a tight loop.
func (a *LoomActivities) LoomHeartbeatActivity(ctx context.Context, beatCount int) error {
	start := time.Now()
	log.Printf("[Ralph] Beat %d: starting (dispatcher=%v agentMgr=%v beadsMgr=%v)", beatCount, a.dispatcher != nil, a.agentMgr != nil, a.beadsMgr != nil)

	// Phase 1: Reset agents stuck in "working" state for too long
	agentsReset := 0
	if a.agentMgr != nil {
		agentsReset = a.agentMgr.ResetStuckAgents(5 * time.Minute)
	}
	log.Printf("[Ralph] Beat %d: phase1 done (agentsReset=%d, elapsed=%v)", beatCount, agentsReset, time.Since(start).Round(time.Millisecond))

	// Phase 2: Auto-block beads stuck in dispatch loops
	stuckResolved := a.resolveStuckBeads()
	log.Printf("[Ralph] Beat %d: phase2 done (stuckResolved=%d, elapsed=%v)", beatCount, stuckResolved, time.Since(start).Round(time.Millisecond))

	// Phase 3: Drain all dispatchable work.
	// NOTE: TaskExecutor (internal/taskexecutor) handles all bead execution
	// directly via workerLoop → claimNextBead → ExecuteTaskWithLoop. The old
	// dispatcher goroutines were tied to the Temporal activity context, causing
	// mass "context canceled" errors every ~10s when the activity completed.
	// Phase 3 is intentionally a no-op to avoid double-dispatch.
	dispatched := 0

	// Phase 4: Auto-recover beads blocked due to transient provider failures.
	// Provider errors (context canceled, 5xx, NATS disconnect) cause dispatch
	// failures that accumulate until max_hops is reached and the bead is blocked.
	// Once the underlying cause is fixed, these beads are permanently stuck
	// unless explicitly recovered. We scan every ~10 beats (every ~100s) to
	// reset eligible beads back to open so they can be retried.
	recovered := 0
	if beatCount%10 == 0 {
		recovered = a.autoRecoverProviderBlockedBeads()
		if recovered > 0 {
			log.Printf("[Ralph] Beat %d: auto-recovered %d provider-blocked bead(s)", beatCount, recovered)
		}
	}

	elapsed := time.Since(start)
	log.Printf("[Ralph] Beat %d: dispatched=%d stuck_resolved=%d agents_reset=%d recovered=%d elapsed=%v",
		beatCount, dispatched, stuckResolved, agentsReset, recovered, elapsed.Round(time.Millisecond))

	return nil
}

// autoRecoverProviderBlockedBeads scans blocked beads and resets those that
// were blocked due to transient provider failures (context canceled, repeated
// provider errors, NATS-related errors). These beads are safe to retry once
// the underlying infrastructure issue is resolved.
//
// Beads blocked for auth failures, budget exhaustion, or hard dispatch limits
// are intentionally left alone — those require human intervention.
func (a *LoomActivities) autoRecoverProviderBlockedBeads() int {
	if a.beadsMgr == nil {
		return 0
	}

	blockedBeads, err := a.beadsMgr.ListBeads(map[string]interface{}{
		"status": models.BeadStatusBlocked,
	})
	if err != nil {
		log.Printf("[Ralph] autoRecover: could not list blocked beads: %v", err)
		return 0
	}

	recovered := 0
	for _, b := range blockedBeads {
		if b == nil || b.Context == nil {
			continue
		}

		reason := b.Context["ralph_blocked_reason"]
		if reason == "" {
			continue
		}

		// Categorize the block reason.
		isProviderTransient := strings.Contains(reason, "provider errors") ||
			strings.Contains(reason, "context canceled") ||
			strings.Contains(reason, "provider unavailable") ||
			strings.Contains(reason, "Identical error repeated") ||
			strings.Contains(reason, "provider error") ||
			strings.Contains(reason, "rate limit")
		isAuthBlocked := strings.Contains(reason, "auth") || strings.Contains(reason, "Authentication")
		isBudgetBlocked := strings.Contains(reason, "budget") || strings.Contains(reason, "hard_dispatch_limit")

		// Budget exhaustion always requires human intervention — skip.
		if isBudgetBlocked {
			continue
		}

		blockedAt, _ := time.Parse(time.RFC3339, b.Context["ralph_blocked_at"])

		if isProviderTransient {
			// Recover provider-blocked beads after 30 minutes.
			if !blockedAt.IsZero() && time.Since(blockedAt) < 30*time.Minute {
				continue
			}
		} else if isAuthBlocked {
			// Auth errors may be transient (key rotation, temporary outage, stale
			// error history from a fixed provider). Retry after 2 hours with a
			// clean error slate so the fresh run can succeed.
			if !blockedAt.IsZero() && time.Since(blockedAt) < 2*time.Hour {
				continue
			}
		} else {
			// Unknown block reason — leave alone, require human intervention.
			continue
		}

		// Reset to open with a fresh dispatch count so the bead gets a clean
		// retry slate. Preserve all other context fields for debugging.
		ctxReset := map[string]string{
			"ralph_blocked_reason": "",
			"ralph_blocked_at":     "",
			"dispatch_count":       "0",
			"loop_detected":        "",
			"loop_detected_reason": "",
			"error_history":        "",
			"redispatch_requested": "true",
		}
		updates := map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
			"context":     ctxReset,
		}
		if err := a.beadsMgr.UpdateBead(b.ID, updates); err != nil {
			log.Printf("[Ralph] autoRecover: failed to reset bead %s: %v", b.ID, err)
			continue
		}
		log.Printf("[Ralph] Auto-recovered blocked bead %s (was blocked: %s)",
			b.ID, reason[:min(len(reason), 80)])
		recovered++
	}
	return recovered
}

// min returns the smaller of two ints (until Go 1.21 built-in min is available).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resolveStuckBeads finds beads with loop_detected=true that haven't been
// resolved by Ralph yet, and auto-blocks them.
func (a *LoomActivities) resolveStuckBeads() int {
	if a.beadsMgr == nil {
		return 0
	}

	// Only query open/in-progress beads — closed beads can't be stuck.
	openBeads, err := a.beadsMgr.ListBeads(map[string]interface{}{"status": models.BeadStatusOpen})
	if err != nil {
		return 0
	}
	inProgressBeads, err := a.beadsMgr.ListBeads(map[string]interface{}{"status": models.BeadStatusInProgress})
	if err != nil {
		return 0
	}
	candidates := append(openBeads, inProgressBeads...)

	resolved := 0
	for _, b := range candidates {
		if b == nil || b.Context == nil {
			continue
		}
		if b.Context["loop_detected"] != "true" {
			continue
		}
		// Skip if already resolved by Ralph or escalated to CEO
		if b.Context["ralph_blocked_at"] != "" || b.Context["escalated_to_ceo_decision_id"] != "" {
			continue
		}

		reason := b.Context["loop_detected_reason"]
		if reason == "" {
			reason = "loop detected"
		}

		triageAgent := a.findDefaultTriageAgent(b.ProjectID)
		updates := map[string]interface{}{
			"status":      models.BeadStatusBlocked,
			"assigned_to": triageAgent,
			"context": map[string]string{
				"ralph_blocked_at":     time.Now().UTC().Format(time.RFC3339),
				"ralph_blocked_reason": fmt.Sprintf("auto-blocked by Ralph: %s", reason),
				"redispatch_requested": "false",
			},
		}
		if err := a.beadsMgr.UpdateBead(b.ID, updates); err != nil {
			log.Printf("[Ralph] Failed to auto-block stuck bead %s: %v", b.ID, err)
			continue
		}
		log.Printf("[Ralph] Auto-blocked stuck bead %s: %s (reassigned to %s)", b.ID, reason, triageAgent)
		resolved++
	}
	return resolved
}

func (a *LoomActivities) findDefaultTriageAgent(projectID string) string {
	if a.agentMgr == nil {
		return ""
	}
	agents := a.agentMgr.ListAgentsByProject(projectID)
	if len(agents) == 0 {
		agents = a.agentMgr.ListAgents()
	}
	var fallback string
	for _, ag := range agents {
		role := strings.TrimSpace(strings.ToLower(ag.Role))
		role = strings.ReplaceAll(role, "_", "-")
		role = strings.ReplaceAll(role, " ", "-")
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
