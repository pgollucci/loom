package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/jordanhubbard/loom/internal/worker"
	"github.com/jordanhubbard/loom/pkg/models"
)

// --- sortReadyBeads ---

func TestSortReadyBeads_ByPriority(t *testing.T) {
	beads := []*models.Bead{
		{ID: "p2", Priority: 2, UpdatedAt: time.Now()},
		{ID: "p0", Priority: 0, UpdatedAt: time.Now()},
		{ID: "p1", Priority: 1, UpdatedAt: time.Now()},
	}
	sortReadyBeads(beads)
	if beads[0].ID != "p0" || beads[1].ID != "p1" || beads[2].ID != "p2" {
		t.Errorf("Expected [p0, p1, p2], got [%s, %s, %s]", beads[0].ID, beads[1].ID, beads[2].ID)
	}
}

func TestSortReadyBeads_SamePriorityByRecency(t *testing.T) {
	now := time.Now()
	beads := []*models.Bead{
		{ID: "old", Priority: 1, UpdatedAt: now.Add(-1 * time.Hour)},
		{ID: "new", Priority: 1, UpdatedAt: now},
	}
	sortReadyBeads(beads)
	if beads[0].ID != "new" {
		t.Errorf("Expected 'new' first (more recent), got %s", beads[0].ID)
	}
}

func TestSortReadyBeads_NilsToEnd(t *testing.T) {
	now := time.Now()
	beads := []*models.Bead{
		nil,
		{ID: "a", Priority: 1, UpdatedAt: now},
		nil,
		{ID: "b", Priority: 0, UpdatedAt: now},
	}
	sortReadyBeads(beads)
	if beads[0].ID != "b" {
		t.Errorf("Expected 'b' (priority 0) first, got %v", beads[0])
	}
	if beads[1].ID != "a" {
		t.Errorf("Expected 'a' second, got %v", beads[1])
	}
	if beads[2] != nil || beads[3] != nil {
		t.Error("Expected nils at end")
	}
}

func TestSortReadyBeads_Empty(t *testing.T) {
	sortReadyBeads(nil)
	sortReadyBeads([]*models.Bead{})
}

func TestSortReadyBeads_AllNil(t *testing.T) {
	beads := []*models.Bead{nil, nil, nil}
	sortReadyBeads(beads)
	for i, b := range beads {
		if b != nil {
			t.Errorf("Expected nil at index %d", i)
		}
	}
}

func TestSortReadyBeads_SingleElement(t *testing.T) {
	beads := []*models.Bead{{ID: "only", Priority: 1, UpdatedAt: time.Now()}}
	sortReadyBeads(beads)
	if beads[0].ID != "only" {
		t.Errorf("Expected 'only', got %s", beads[0].ID)
	}
}

// --- beadSkipCheck ---

func TestBeadSkipCheck_NilBead(t *testing.T) {
	skip, reason := beadSkipCheck(nil, 20)
	if !skip || reason != "nil_bead" {
		t.Errorf("Expected (true, nil_bead), got (%v, %s)", skip, reason)
	}
}

func TestBeadSkipCheck_DecisionType(t *testing.T) {
	b := &models.Bead{ID: "b1", Type: "decision"}
	skip, reason := beadSkipCheck(b, 20)
	if !skip || reason != "decision_type" {
		t.Errorf("Expected (true, decision_type), got (%v, %s)", skip, reason)
	}
}

func TestBeadSkipCheck_CooldownAfterFailure(t *testing.T) {
	b := &models.Bead{
		ID:   "b1",
		Type: "task",
		Context: map[string]string{
			"last_failed_at": time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339),
		},
	}
	skip, reason := beadSkipCheck(b, 20)
	if !skip || reason != "cooldown_after_failure" {
		t.Errorf("Expected (true, cooldown_after_failure), got (%v, %s)", skip, reason)
	}
}

func TestBeadSkipCheck_CooldownExpired(t *testing.T) {
	b := &models.Bead{
		ID:   "b1",
		Type: "task",
		Context: map[string]string{
			"last_failed_at": time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
		},
	}
	skip, _ := beadSkipCheck(b, 20)
	if skip {
		t.Error("Expected bead to pass skip check after cooldown expired")
	}
}

func TestBeadSkipCheck_AlreadyRun(t *testing.T) {
	// "open" beads that ran recently should be in retry_cooldown
	bRecent := &models.Bead{
		ID:     "b1",
		Type:   "task",
		Status: "open",
		Context: map[string]string{
			"redispatch_requested": "false",
			"last_run_at":          time.Now().UTC().Format(time.RFC3339),
		},
	}
	skip, reason := beadSkipCheck(bRecent, 20)
	if !skip || reason != "retry_cooldown" {
		t.Errorf("Expected (true, retry_cooldown) for recently-run open bead, got (%v, %s)", skip, reason)
	}

	// "open" beads that ran long ago should be eligible for retry
	bOld := &models.Bead{
		ID:     "b1b",
		Type:   "task",
		Status: "open",
		Context: map[string]string{
			"redispatch_requested": "false",
			"last_run_at":          time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339),
		},
	}
	skip, _ = beadSkipCheck(bOld, 20)
	if skip {
		t.Error("Expected open bead with old last_run_at to NOT be skipped (eligible for retry)")
	}

	// "done" beads should be skipped
	bDone := &models.Bead{
		ID:     "b2",
		Type:   "task",
		Status: "done",
		Context: map[string]string{
			"last_run_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
	skip, reason = beadSkipCheck(bDone, 20)
	if !skip || reason != "already_run" {
		t.Errorf("Expected (true, already_run) for done bead, got (%v, %s)", skip, reason)
	}
}

func TestBeadSkipCheck_RedispatchRequested(t *testing.T) {
	// A "done" bead would normally be skipped, but redispatch_requested overrides
	b := &models.Bead{
		ID:     "b1",
		Type:   "task",
		Status: "done",
		Context: map[string]string{
			"redispatch_requested": "true",
			"last_run_at":          time.Now().UTC().Format(time.RFC3339),
		},
	}
	skip, _ := beadSkipCheck(b, 20)
	if skip {
		t.Error("Expected done bead with redispatch_requested=true to not be skipped")
	}
}

func TestBeadSkipCheck_InProgress(t *testing.T) {
	b := &models.Bead{
		ID:     "b1",
		Type:   "task",
		Status: "in_progress",
		Context: map[string]string{
			"last_run_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
	skip, _ := beadSkipCheck(b, 20)
	if skip {
		t.Error("Expected in_progress bead to not be skipped")
	}
}

func TestBeadSkipCheck_NoContextPasses(t *testing.T) {
	b := &models.Bead{ID: "b1", Type: "task"}
	skip, _ := beadSkipCheck(b, 20)
	if skip {
		t.Error("Expected bead with no context to pass")
	}
}

func TestBeadSkipCheck_InvalidTimestamp(t *testing.T) {
	b := &models.Bead{
		ID:   "b1",
		Type: "task",
		Context: map[string]string{
			"last_failed_at": "not-a-timestamp",
		},
	}
	skip, _ := beadSkipCheck(b, 20)
	if skip {
		t.Error("Expected bead with invalid timestamp to pass (unparseable = no cooldown)")
	}
}

// --- dispatchCountForBead ---

func TestDispatchCountForBead_NilBead(t *testing.T) {
	if dispatchCountForBead(nil) != 0 {
		t.Error("Expected 0 for nil bead")
	}
}

func TestDispatchCountForBead_NilContext(t *testing.T) {
	b := &models.Bead{ID: "b1"}
	if dispatchCountForBead(b) != 0 {
		t.Error("Expected 0 for nil context")
	}
}

func TestDispatchCountForBead_MissingKey(t *testing.T) {
	b := &models.Bead{ID: "b1", Context: map[string]string{}}
	if dispatchCountForBead(b) != 0 {
		t.Error("Expected 0 for missing dispatch_count key")
	}
}

func TestDispatchCountForBead_ValidCount(t *testing.T) {
	b := &models.Bead{
		ID:      "b1",
		Context: map[string]string{"dispatch_count": "7"},
	}
	if got := dispatchCountForBead(b); got != 7 {
		t.Errorf("Expected 7, got %d", got)
	}
}

func TestDispatchCountForBead_InvalidCount(t *testing.T) {
	b := &models.Bead{
		ID:      "b1",
		Context: map[string]string{"dispatch_count": "abc"},
	}
	if got := dispatchCountForBead(b); got != 0 {
		t.Errorf("Expected 0 for invalid count, got %d", got)
	}
}

// --- matchAssignedAgent ---

func TestMatchAssignedAgent_NotAssigned(t *testing.T) {
	b := &models.Bead{ID: "b1", AssignedTo: ""}
	matched, skip, reason := matchAssignedAgent(b, nil, nil)
	if matched != nil || skip || reason != "" {
		t.Errorf("Expected (nil, false, ''), got (%v, %v, %s)", matched, skip, reason)
	}
}

func TestMatchAssignedAgent_DeadAgent(t *testing.T) {
	b := &models.Bead{ID: "b1", AssignedTo: "dead-agent"}
	allByID := map[string]*models.Agent{}
	matched, skip, reason := matchAssignedAgent(b, nil, allByID)
	if matched != nil || skip || reason != "dead_agent" {
		t.Errorf("Expected (nil, false, dead_agent), got (%v, %v, %s)", matched, skip, reason)
	}
}

func TestMatchAssignedAgent_BusyAgent(t *testing.T) {
	b := &models.Bead{ID: "b1", AssignedTo: "a1"}
	idleByID := map[string]*models.Agent{}
	allByID := map[string]*models.Agent{"a1": {ID: "a1"}}
	matched, skip, reason := matchAssignedAgent(b, idleByID, allByID)
	if matched != nil || !skip || reason != "assigned_agent_not_idle" {
		t.Errorf("Expected (nil, true, assigned_agent_not_idle), got (%v, %v, %s)", matched, skip, reason)
	}
}

func TestMatchAssignedAgent_IdleAgent(t *testing.T) {
	ag := &models.Agent{ID: "a1"}
	b := &models.Bead{ID: "b1", AssignedTo: "a1"}
	idleByID := map[string]*models.Agent{"a1": ag}
	allByID := map[string]*models.Agent{"a1": ag}
	matched, skip, reason := matchAssignedAgent(b, idleByID, allByID)
	if matched != ag || skip || reason != "" {
		t.Errorf("Expected (ag, false, ''), got (%v, %v, %s)", matched, skip, reason)
	}
}

// --- matchAgentForBead ---

func TestMatchAgentForBead_PrefersEngineeringManager(t *testing.T) {
	d := &Dispatcher{personaMatcher: NewPersonaMatcher()}
	b := &models.Bead{ID: "b1", ProjectID: "proj-1"}
	agents := []*models.Agent{
		{ID: "a1", Role: "Backend Engineer", ProjectID: "proj-1"},
		{ID: "a2", Role: "Engineering Manager", ProjectID: "proj-1"},
	}

	ag := d.matchAgentForBead(b, agents)
	if ag == nil || ag.ID != "a2" {
		t.Errorf("Expected engineering manager a2, got %v", ag)
	}
}

func TestMatchAgentForBead_FallbackToAny(t *testing.T) {
	d := &Dispatcher{personaMatcher: NewPersonaMatcher()}
	b := &models.Bead{ID: "b1", ProjectID: "proj-1"}
	agents := []*models.Agent{
		{ID: "a1", Role: "Backend Engineer", ProjectID: "proj-1"},
	}

	ag := d.matchAgentForBead(b, agents)
	if ag == nil || ag.ID != "a1" {
		t.Errorf("Expected fallback to a1, got %v", ag)
	}
}

func TestMatchAgentForBead_NoMatchingProject(t *testing.T) {
	d := &Dispatcher{personaMatcher: NewPersonaMatcher()}
	b := &models.Bead{ID: "b1", ProjectID: "proj-1"}
	agents := []*models.Agent{
		{ID: "a1", Role: "Backend Engineer", ProjectID: "proj-2"},
	}

	ag := d.matchAgentForBead(b, agents)
	if ag != nil {
		t.Errorf("Expected nil for non-matching project, got %v", ag)
	}
}

func TestMatchAgentForBead_AgentWithEmptyProject(t *testing.T) {
	d := &Dispatcher{personaMatcher: NewPersonaMatcher()}
	b := &models.Bead{ID: "b1", ProjectID: "proj-1"}
	agents := []*models.Agent{
		{ID: "a1", Role: "Backend Engineer", ProjectID: ""},
	}

	ag := d.matchAgentForBead(b, agents)
	if ag == nil || ag.ID != "a1" {
		t.Errorf("Expected agent with empty project to match, got %v", ag)
	}
}

func TestMatchAgentForBead_BeadWithEmptyProject(t *testing.T) {
	d := &Dispatcher{personaMatcher: NewPersonaMatcher()}
	b := &models.Bead{ID: "b1", ProjectID: ""}
	agents := []*models.Agent{
		{ID: "a1", Role: "Backend Engineer", ProjectID: "proj-1"},
	}

	ag := d.matchAgentForBead(b, agents)
	if ag == nil || ag.ID != "a1" {
		t.Errorf("Expected match with empty bead project, got %v", ag)
	}
}

func TestMatchAgentForBead_EmptyAgents(t *testing.T) {
	d := &Dispatcher{personaMatcher: NewPersonaMatcher()}
	b := &models.Bead{ID: "b1", ProjectID: "proj-1"}

	ag := d.matchAgentForBead(b, nil)
	if ag != nil {
		t.Errorf("Expected nil for empty agents, got %v", ag)
	}
}

// --- filterBeadsByReadiness ---

func TestFilterBeadsByReadiness_NilCheck(t *testing.T) {
	d := &Dispatcher{}
	beads := []*models.Bead{{ID: "b1"}, {ID: "b2"}}
	result := d.filterBeadsByReadiness(context.Background(), beads, nil, ReadinessBlock)
	if len(result) != 2 {
		t.Errorf("Expected all beads with nil check, got %d", len(result))
	}
}

func TestFilterBeadsByReadiness_BlockMode_AllReady(t *testing.T) {
	d := &Dispatcher{}
	beads := []*models.Bead{
		{ID: "b1", ProjectID: "p1"},
		{ID: "b2", ProjectID: "p1"},
	}
	check := func(ctx context.Context, pid string) (bool, []string) {
		return true, nil
	}
	result := d.filterBeadsByReadiness(context.Background(), beads, check, ReadinessBlock)
	if len(result) != 2 {
		t.Errorf("Expected 2 beads, got %d", len(result))
	}
}

func TestFilterBeadsByReadiness_BlockMode_NoneReady(t *testing.T) {
	d := &Dispatcher{}
	beads := []*models.Bead{
		{ID: "b1", ProjectID: "p1"},
	}
	check := func(ctx context.Context, pid string) (bool, []string) {
		return false, []string{"not ready"}
	}
	result := d.filterBeadsByReadiness(context.Background(), beads, check, ReadinessBlock)
	if len(result) != 0 {
		t.Errorf("Expected 0 beads, got %d", len(result))
	}
}

func TestFilterBeadsByReadiness_BlockMode_MixedProjects(t *testing.T) {
	d := &Dispatcher{}
	beads := []*models.Bead{
		{ID: "b1", ProjectID: "ready-proj"},
		{ID: "b2", ProjectID: "not-ready-proj"},
		{ID: "b3", ProjectID: "ready-proj"},
	}
	check := func(ctx context.Context, pid string) (bool, []string) {
		return pid == "ready-proj", nil
	}
	result := d.filterBeadsByReadiness(context.Background(), beads, check, ReadinessBlock)
	if len(result) != 2 {
		t.Errorf("Expected 2 beads from ready project, got %d", len(result))
	}
}

func TestFilterBeadsByReadiness_BlockMode_NilBeadPassesThrough(t *testing.T) {
	d := &Dispatcher{}
	beads := []*models.Bead{nil, {ID: "b1", ProjectID: "p1"}}
	check := func(ctx context.Context, pid string) (bool, []string) {
		return true, nil
	}
	result := d.filterBeadsByReadiness(context.Background(), beads, check, ReadinessBlock)
	if len(result) != 2 {
		t.Errorf("Expected 2 (nil + ready), got %d", len(result))
	}
}

func TestFilterBeadsByReadiness_WarnMode_AllPassThrough(t *testing.T) {
	d := &Dispatcher{}
	beads := []*models.Bead{
		{ID: "b1", ProjectID: "p1"},
		{ID: "b2", ProjectID: "p2"},
	}
	check := func(ctx context.Context, pid string) (bool, []string) {
		return false, []string{"warning"}
	}
	result := d.filterBeadsByReadiness(context.Background(), beads, check, ReadinessWarn)
	if len(result) != 2 {
		t.Errorf("Expected all beads in warn mode, got %d", len(result))
	}
}

func TestFilterBeadsByReadiness_BlockMode_CachesPerProject(t *testing.T) {
	d := &Dispatcher{}
	callCount := 0
	beads := []*models.Bead{
		{ID: "b1", ProjectID: "p1"},
		{ID: "b2", ProjectID: "p1"},
		{ID: "b3", ProjectID: "p1"},
	}
	check := func(ctx context.Context, pid string) (bool, []string) {
		callCount++
		return true, nil
	}
	d.filterBeadsByReadiness(context.Background(), beads, check, ReadinessBlock)
	if callCount != 1 {
		t.Errorf("Expected check called once for one project, got %d", callCount)
	}
}

// --- checkProjectReadiness ---

func TestCheckProjectReadiness_NoCheck(t *testing.T) {
	d := &Dispatcher{}
	blocked, result := d.checkProjectReadiness(context.Background(), "proj-1")
	if blocked || result != nil {
		t.Error("Expected no block with nil readiness check")
	}
}

func TestCheckProjectReadiness_EmptyProjectID(t *testing.T) {
	d := &Dispatcher{
		readinessCheck: func(ctx context.Context, pid string) (bool, []string) {
			return false, []string{"fail"}
		},
		readinessMode: ReadinessBlock,
	}
	blocked, result := d.checkProjectReadiness(context.Background(), "")
	if blocked || result != nil {
		t.Error("Expected no block with empty project ID")
	}
}

func TestCheckProjectReadiness_BlockWhenNotReady(t *testing.T) {
	d := &Dispatcher{
		readinessCheck: func(ctx context.Context, pid string) (bool, []string) {
			return false, []string{"missing config"}
		},
		readinessMode: ReadinessBlock,
	}
	blocked, result := d.checkProjectReadiness(context.Background(), "proj-1")
	if !blocked {
		t.Error("Expected block when readiness fails")
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Dispatched {
		t.Error("Expected Dispatched=false")
	}
	if result.Error == "" {
		t.Error("Expected error message in result")
	}
}

func TestCheckProjectReadiness_PassWhenReady(t *testing.T) {
	d := &Dispatcher{
		readinessCheck: func(ctx context.Context, pid string) (bool, []string) {
			return true, nil
		},
		readinessMode: ReadinessBlock,
	}
	blocked, result := d.checkProjectReadiness(context.Background(), "proj-1")
	if blocked || result != nil {
		t.Error("Expected no block when ready")
	}
}

func TestCheckProjectReadiness_WarnModeNeverBlocks(t *testing.T) {
	d := &Dispatcher{
		readinessCheck: func(ctx context.Context, pid string) (bool, []string) {
			return false, []string{"issue"}
		},
		readinessMode: ReadinessWarn,
	}
	blocked, result := d.checkProjectReadiness(context.Background(), "proj-1")
	if blocked || result != nil {
		t.Error("Expected no block in warn mode")
	}
}

func TestCheckProjectReadiness_BlockNoIssueList(t *testing.T) {
	d := &Dispatcher{
		readinessCheck: func(ctx context.Context, pid string) (bool, []string) {
			return false, nil
		},
		readinessMode: ReadinessBlock,
	}
	blocked, result := d.checkProjectReadiness(context.Background(), "proj-1")
	if !blocked {
		t.Error("Expected block")
	}
	if result.Error != "project readiness failed" {
		t.Errorf("Expected generic failure message, got %q", result.Error)
	}
}

// --- checkHopLimit ---

func TestCheckHopLimit_UnderLimit(t *testing.T) {
	d := &Dispatcher{loopDetector: NewLoopDetector()}
	b := &models.Bead{ID: "b1"}
	exceeded, stuck, reason := d.checkHopLimit(b, 5, 20)
	if exceeded || stuck || reason != "" {
		t.Errorf("Expected (false, false, ''), got (%v, %v, %s)", exceeded, stuck, reason)
	}
}

func TestCheckHopLimit_AlreadyEscalated(t *testing.T) {
	d := &Dispatcher{loopDetector: NewLoopDetector()}
	b := &models.Bead{
		ID: "b1",
		Context: map[string]string{
			"escalated_to_ceo_decision_id": "d-1",
		},
	}
	exceeded, stuck, reason := d.checkHopLimit(b, 20, 20)
	if !exceeded || !stuck || reason != "dispatch_limit_escalated" {
		t.Errorf("Expected (true, true, dispatch_limit_escalated), got (%v, %v, %s)", exceeded, stuck, reason)
	}
}

func TestCheckHopLimit_ProgressingBeyondLimit(t *testing.T) {
	d := &Dispatcher{loopDetector: NewLoopDetector()}
	b := &models.Bead{
		ID: "b1",
		Context: map[string]string{
			"agent_output": "investigating further, found new clue",
		},
	}
	exceeded, stuck, _ := d.checkHopLimit(b, 25, 20)
	if !exceeded {
		t.Error("Expected exceeded=true")
	}
	if stuck {
		t.Error("Expected stuck=false when making progress")
	}
}

func TestCheckHopLimit_ExactlyAtLimit(t *testing.T) {
	d := &Dispatcher{loopDetector: NewLoopDetector()}
	b := &models.Bead{ID: "b1"}
	exceeded, _, _ := d.checkHopLimit(b, 20, 20)
	if !exceeded {
		t.Error("Expected exceeded=true at exact limit")
	}
}

func TestCheckHopLimit_JustUnder(t *testing.T) {
	d := &Dispatcher{loopDetector: NewLoopDetector()}
	b := &models.Bead{ID: "b1"}
	exceeded, _, _ := d.checkHopLimit(b, 19, 20)
	if exceeded {
		t.Error("Expected exceeded=false just under limit")
	}
}

// --- candidateSelection type ---

func TestCandidateSelection_EmptyResult(t *testing.T) {
	sel := candidateSelection{}
	if sel.Bead != nil || sel.Agent != nil {
		t.Error("Expected nil bead and agent for zero-value selection")
	}
	if sel.SkippedReasons != nil {
		t.Error("Expected nil skipped reasons for zero-value selection")
	}
}

// --- selectCandidate ---

func TestSelectCandidate_EmptyBeads(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	sel := d.selectCandidate(context.Background(), nil, nil, nil, nil)
	if sel.Bead != nil {
		t.Error("Expected nil bead from empty beads")
	}
}

func TestSelectCandidate_AllNilBeads(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	beads := []*models.Bead{nil, nil}
	sel := d.selectCandidate(context.Background(), beads, nil, nil, nil)
	if sel.Bead != nil {
		t.Error("Expected nil bead from all-nil beads")
	}
	if sel.SkippedReasons["nil_bead"] != 2 {
		t.Errorf("Expected 2 nil_bead skips, got %d", sel.SkippedReasons["nil_bead"])
	}
}

func TestSelectCandidate_SkipsDecisions(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	beads := []*models.Bead{
		{ID: "b1", Type: "decision"},
	}
	sel := d.selectCandidate(context.Background(), beads, nil, nil, nil)
	if sel.Bead != nil {
		t.Error("Expected nil bead; decisions should be skipped")
	}
	if sel.SkippedReasons["decision_type"] != 1 {
		t.Error("Expected decision_type skip")
	}
}

func TestSelectCandidate_SkipsHumanConfig(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	beads := []*models.Bead{
		{ID: "b1", Type: "task", Tags: []string{"requires-human-config"}},
	}
	sel := d.selectCandidate(context.Background(), beads, nil, nil, nil)
	if sel.Bead != nil {
		t.Error("Expected nil bead; human-config beads should be skipped")
	}
	if sel.SkippedReasons["requires_human_config"] != 1 {
		t.Error("Expected requires_human_config skip")
	}
}

func TestSelectCandidate_MatchesAssignedIdleAgent(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	ag := &models.Agent{ID: "a1", ProjectID: "proj-1"}
	beads := []*models.Bead{
		{
			ID:         "b1",
			Type:       "task",
			ProjectID:  "proj-1",
			AssignedTo: "a1",
			Status:     "open",
			Context: map[string]string{
				"redispatch_requested": "true",
			},
		},
	}
	idleByID := map[string]*models.Agent{"a1": ag}
	allByID := map[string]*models.Agent{"a1": ag}

	sel := d.selectCandidate(context.Background(), beads, []*models.Agent{ag}, idleByID, allByID)
	if sel.Bead == nil || sel.Bead.ID != "b1" {
		t.Error("Expected bead b1 to be selected")
	}
	if sel.Agent == nil || sel.Agent.ID != "a1" {
		t.Error("Expected agent a1 to be matched")
	}
}

func TestSelectCandidate_PicksAgentForUnassigned(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	ag := &models.Agent{ID: "a1", Role: "Engineering Manager", ProjectID: "proj-1"}
	beads := []*models.Bead{
		{
			ID:        "b1",
			Type:      "task",
			ProjectID: "proj-1",
			Status:    "open",
		},
	}
	idleByID := map[string]*models.Agent{"a1": ag}
	allByID := map[string]*models.Agent{"a1": ag}

	sel := d.selectCandidate(context.Background(), beads, []*models.Agent{ag}, idleByID, allByID)
	if sel.Bead == nil || sel.Bead.ID != "b1" {
		t.Error("Expected bead b1 to be selected")
	}
	if sel.Agent == nil || sel.Agent.ID != "a1" {
		t.Error("Expected agent a1 to be matched")
	}
}

func TestSelectCandidate_NoIdleAgents(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	beads := []*models.Bead{
		{ID: "b1", Type: "task", ProjectID: "proj-1", Status: "open"},
	}

	sel := d.selectCandidate(context.Background(), beads, nil, nil, nil)
	if sel.Bead != nil {
		t.Error("Expected nil bead with no idle agents")
	}
	if sel.SkippedReasons["no_idle_agents_for_project"] != 1 {
		t.Errorf("Expected no_idle_agents_for_project skip, got %v", sel.SkippedReasons)
	}
}

func TestSelectCandidate_SkipsCooldownBead(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	beads := []*models.Bead{
		{
			ID:   "b1",
			Type: "task",
			Context: map[string]string{
				"last_failed_at": time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339),
			},
		},
	}

	sel := d.selectCandidate(context.Background(), beads, nil, nil, nil)
	if sel.Bead != nil {
		t.Error("Expected nil bead for cooldown bead")
	}
	if sel.SkippedReasons["cooldown_after_failure"] != 1 {
		t.Errorf("Expected cooldown_after_failure skip, got %v", sel.SkippedReasons)
	}
}

func TestSelectCandidate_AssignedBusyAgent(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	ag := &models.Agent{ID: "a1"}
	beads := []*models.Bead{
		{
			ID:         "b1",
			Type:       "task",
			AssignedTo: "a1",
			Status:     "open",
			Context: map[string]string{
				"redispatch_requested": "true",
			},
		},
	}
	// No idle agents at all — bead should be skipped (no one to dispatch to)
	idleByID := map[string]*models.Agent{}
	allByID := map[string]*models.Agent{"a1": ag}

	sel := d.selectCandidate(context.Background(), beads, nil, idleByID, allByID)
	if sel.Bead != nil {
		t.Error("Expected nil when no idle agents available")
	}

	// With an alternative idle agent available, the bead should be dispatched
	altAgent := &models.Agent{ID: "a2", Name: "Alt Agent", Role: "Engineering Manager", Status: "idle"}
	idleByID2 := map[string]*models.Agent{"a2": altAgent}
	allByID2 := map[string]*models.Agent{"a1": ag, "a2": altAgent}
	idleAgents := []*models.Agent{altAgent}

	sel2 := d.selectCandidate(context.Background(), beads, idleAgents, idleByID2, allByID2)
	if sel2.Bead == nil {
		t.Error("Expected bead to be dispatched to alternative idle agent when assigned agent is busy")
	}
}

func TestSelectCandidate_MixedBeads(t *testing.T) {
	d := &Dispatcher{
		personaMatcher: NewPersonaMatcher(),
		autoBugRouter:  NewAutoBugRouter(),
		loopDetector:   NewLoopDetector(),
	}
	ag := &models.Agent{ID: "a1", Role: "Engineering Manager", ProjectID: "proj-1"}
	beads := []*models.Bead{
		nil,
		{ID: "b1", Type: "decision"},
		{ID: "b2", Type: "task", Tags: []string{"requires-human-config"}},
		{ID: "b3", Type: "task", ProjectID: "proj-1", Status: "open"},
	}
	idleByID := map[string]*models.Agent{"a1": ag}
	allByID := map[string]*models.Agent{"a1": ag}

	sel := d.selectCandidate(context.Background(), beads, []*models.Agent{ag}, idleByID, allByID)
	if sel.Bead == nil || sel.Bead.ID != "b3" {
		t.Error("Expected bead b3 selected (first valid)")
	}
	if sel.SkippedReasons["nil_bead"] != 1 {
		t.Error("Expected 1 nil_bead skip")
	}
	if sel.SkippedReasons["decision_type"] != 1 {
		t.Error("Expected 1 decision_type skip")
	}
	if sel.SkippedReasons["requires_human_config"] != 1 {
		t.Error("Expected 1 requires_human_config skip")
	}
}

// --- applyLoopMetadata ---

func TestApplyLoopMetadata_NoIterations(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{"redispatch_requested": "true"}
	result := &worker.TaskResult{LoopIterations: 0}
	d.applyLoopMetadata(ctx, nil, nil, result)
	if ctx["redispatch_requested"] != "true" {
		t.Error("Expected no change when LoopIterations == 0")
	}
}

func TestApplyLoopMetadata_Completed(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{"redispatch_requested": "true"}
	result := &worker.TaskResult{LoopIterations: 5, LoopTerminalReason: "completed"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, nil, result)
	if ctx["redispatch_requested"] != "false" {
		t.Error("Expected redispatch_requested=false after completion")
	}
	if ctx["terminal_reason"] != "completed" {
		t.Error("Expected terminal_reason=completed")
	}
}

func TestApplyLoopMetadata_MaxIterationsFirstTime(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	bead := &models.Bead{ID: "b1", Context: map[string]string{}}
	result := &worker.TaskResult{LoopIterations: 10, LoopTerminalReason: "max_iterations"}
	d.applyLoopMetadata(ctx, bead, nil, result)
	if ctx["redispatch_requested"] != "true" {
		t.Error("Expected redispatch_requested=true for first max_iterations")
	}
	if ctx["max_iterations_retries"] != "1" {
		t.Error("Expected max_iterations_retries=1")
	}
}

func TestApplyLoopMetadata_MaxIterationsSecondTime(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	bead := &models.Bead{
		ID: "b1",
		Context: map[string]string{
			"max_iterations_retries": "1",
		},
	}
	result := &worker.TaskResult{LoopIterations: 10, LoopTerminalReason: "max_iterations"}
	d.applyLoopMetadata(ctx, bead, nil, result)
	if ctx["redispatch_requested"] != "false" {
		t.Error("Expected redispatch_requested=false after retry exhausted")
	}
	if ctx["max_iterations_retry_exhausted"] != "true" {
		t.Error("Expected max_iterations_retry_exhausted=true")
	}
}

func TestApplyLoopMetadata_ParseFailures(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	result := &worker.TaskResult{LoopIterations: 3, LoopTerminalReason: "parse_failures"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, nil, result)
	if ctx["last_failed_at"] == "" {
		t.Error("Expected last_failed_at to be set for parse_failures")
	}
}

func TestApplyLoopMetadata_ValidationFailures(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	result := &worker.TaskResult{LoopIterations: 1, LoopTerminalReason: "validation_failures"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, nil, result)
	if ctx["last_failed_at"] == "" {
		t.Error("Expected last_failed_at to be set for validation_failures")
	}
}

func TestApplyLoopMetadata_Error(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	result := &worker.TaskResult{LoopIterations: 1, LoopTerminalReason: "error"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, nil, result)
	if ctx["last_failed_at"] == "" {
		t.Error("Expected last_failed_at to be set for error")
	}
}

func TestApplyLoopMetadata_ProgressStagnant(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	result := &worker.TaskResult{LoopIterations: 5, LoopTerminalReason: "progress_stagnant"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, &models.Agent{ID: "a1"}, result)
	if ctx["last_failed_at"] == "" {
		t.Error("Expected last_failed_at to be set")
	}
	if ctx["remediation_needed"] != "true" {
		t.Error("Expected remediation_needed=true")
	}
}

func TestApplyLoopMetadata_InnerLoop(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	result := &worker.TaskResult{LoopIterations: 5, LoopTerminalReason: "inner_loop"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, &models.Agent{ID: "a1"}, result)
	if ctx["remediation_needed"] != "true" {
		t.Error("Expected remediation_needed=true for inner_loop")
	}
}

func TestApplyLoopMetadata_MaxIterNilContext(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	bead := &models.Bead{ID: "b1", Context: nil}
	result := &worker.TaskResult{LoopIterations: 10, LoopTerminalReason: "max_iterations"}
	d.applyLoopMetadata(ctx, bead, nil, result)
	if ctx["max_iterations_retries"] != "1" {
		t.Error("Expected first-time retry allowed with nil context")
	}
}

func TestApplyLoopMetadata_UnknownTerminalReason(t *testing.T) {
	d := &Dispatcher{}
	ctx := map[string]string{}
	result := &worker.TaskResult{LoopIterations: 1, LoopTerminalReason: "unknown_reason"}
	d.applyLoopMetadata(ctx, &models.Bead{ID: "b1"}, nil, result)
	if ctx["terminal_reason"] != "unknown_reason" {
		t.Error("Expected terminal_reason to be set even for unknown reason")
	}
	if _, ok := ctx["last_failed_at"]; ok {
		t.Error("Expected no cooldown for unknown terminal reason")
	}
}
