package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jordanhubbard/loom/pkg/messages"
)

type mockPDABus struct {
	mu              sync.Mutex
	publishedTasks  []*messages.TaskMessage
	publishedPlans  []*messages.PlanMessage
	publishedEvents []*messages.EventMessage
	taskRoles       []string
	resultHandler   func(*messages.ResultMessage)
}

func (m *mockPDABus) PublishTask(_ context.Context, _ string, task *messages.TaskMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishedTasks = append(m.publishedTasks, task)
	m.taskRoles = append(m.taskRoles, "")
	return nil
}

func (m *mockPDABus) PublishTaskForRole(_ context.Context, _ string, role string, task *messages.TaskMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishedTasks = append(m.publishedTasks, task)
	m.taskRoles = append(m.taskRoles, role)
	return nil
}

func (m *mockPDABus) PublishPlan(_ context.Context, _ string, plan *messages.PlanMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishedPlans = append(m.publishedPlans, plan)
	return nil
}

func (m *mockPDABus) PublishEvent(_ context.Context, _ string, event *messages.EventMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

func (m *mockPDABus) SubscribeResults(handler func(*messages.ResultMessage)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resultHandler = handler
	return nil
}

type mockBeadCreator struct {
	mu    sync.Mutex
	beads map[string]string
	count int
}

func (m *mockBeadCreator) CreateBead(projectID, title, description, beadType string, priority int, tags []string, parentID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count++
	id := fmt.Sprintf("mock-bead-%d", m.count)
	if m.beads == nil {
		m.beads = make(map[string]string)
	}
	m.beads[title] = id
	return id, nil
}

type mockBeadUpdater struct {
	mu      sync.Mutex
	updates map[string][]map[string]interface{}
}

func (m *mockBeadUpdater) UpdateBead(id string, updates map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updates == nil {
		m.updates = make(map[string][]map[string]interface{})
	}
	m.updates[id] = append(m.updates[id], updates)
	return nil
}

func TestPDAOrchestrator_IsPlanComplete(t *testing.T) {
	o := &PDAOrchestrator{activePlans: make(map[string]*ActivePlan)}

	tests := []struct {
		name     string
		statuses map[string]string
		want     bool
	}{
		{"all completed", map[string]string{"s1": "completed", "s2": "completed"}, true},
		{"all failed", map[string]string{"s1": "failed", "s2": "failed"}, true},
		{"mixed complete", map[string]string{"s1": "completed", "s2": "failed"}, true},
		{"one pending", map[string]string{"s1": "completed", "s2": "pending"}, false},
		{"one in_progress", map[string]string{"s1": "completed", "s2": "in_progress"}, false},
		{"empty plan", map[string]string{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := &ActivePlan{StepStatus: tc.statuses}
			got := o.isPlanComplete(plan)
			if got != tc.want {
				t.Errorf("isPlanComplete(%v) = %v, want %v", tc.statuses, got, tc.want)
			}
		})
	}
}

func TestPDAOrchestrator_ActivePlanCount(t *testing.T) {
	o := &PDAOrchestrator{activePlans: make(map[string]*ActivePlan)}
	if o.ActivePlanCount() != 0 {
		t.Errorf("expected 0 active plans, got %d", o.ActivePlanCount())
	}

	o.activePlans["p1"] = &ActivePlan{}
	o.activePlans["p2"] = &ActivePlan{}
	if o.ActivePlanCount() != 2 {
		t.Errorf("expected 2 active plans, got %d", o.ActivePlanCount())
	}
}

func TestPDAOrchestrator_ExecutePDA_FullCycle(t *testing.T) {
	bus := &mockPDABus{}
	creator := &mockBeadCreator{}
	updater := &mockBeadUpdater{}
	planner := &StaticPlanner{}

	o := NewPDAOrchestrator(bus, planner, creator, updater)

	req := PlanRequest{
		ProjectID:   "proj-1",
		BeadID:      "bead-root",
		Title:       "Test task",
		Description: "A simple test",
	}

	err := o.ExecutePDA(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecutePDA failed: %v", err)
	}

	// Check that beads were created (StaticPlanner produces 3 steps)
	creator.mu.Lock()
	beadCount := creator.count
	creator.mu.Unlock()
	if beadCount != 3 {
		t.Errorf("expected 3 beads created, got %d", beadCount)
	}

	// Check source bead was updated with plan context
	updater.mu.Lock()
	rootUpdates := updater.updates["bead-root"]
	updater.mu.Unlock()
	if len(rootUpdates) == 0 {
		t.Error("expected source bead to be updated with plan context")
	}

	// Check that a plan was published
	bus.mu.Lock()
	planCount := len(bus.publishedPlans)
	bus.mu.Unlock()
	if planCount != 1 {
		t.Errorf("expected 1 plan published, got %d", planCount)
	}

	// Check tasks were dispatched (first step has no deps, should be dispatched)
	bus.mu.Lock()
	taskCount := len(bus.publishedTasks)
	bus.mu.Unlock()
	if taskCount < 1 {
		t.Errorf("expected at least 1 task dispatched, got %d", taskCount)
	}

	if o.ActivePlanCount() != 1 {
		t.Errorf("expected 1 active plan, got %d", o.ActivePlanCount())
	}
}

func TestPDAOrchestrator_Start_SubscribesResults(t *testing.T) {
	bus := &mockPDABus{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, &mockBeadCreator{}, &mockBeadUpdater{})

	err := o.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer o.Close()

	bus.mu.Lock()
	hasHandler := bus.resultHandler != nil
	bus.mu.Unlock()
	if !hasHandler {
		t.Error("expected result handler to be registered")
	}
}

func TestPDAOrchestrator_HandleResult_CompletesStep(t *testing.T) {
	bus := &mockPDABus{}
	creator := &mockBeadCreator{}
	updater := &mockBeadUpdater{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, creator, updater)

	// Set up a plan manually with 2 sequential steps
	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "root-bead",
		CorrelationID: "corr-1",
		Plan: messages.PlanData{
			Priority: 1,
			Steps: []messages.PlanStep{
				{StepID: "s1", Role: "coder", Action: "implement"},
				{StepID: "s2", Role: "reviewer", Action: "review", DependsOn: []string{"s1"}},
			},
		},
		StepBeads:  map[string]string{"s1": "bead-s1", "s2": "bead-s2"},
		StepStatus: map[string]string{"s1": "in_progress", "s2": "pending"},
	}
	o.mu.Lock()
	o.activePlans["plan-1"] = plan
	o.mu.Unlock()

	// Simulate s1 completing successfully
	result := &messages.ResultMessage{
		BeadID: "bead-s1",
		Result: messages.ResultData{Status: "success"},
	}
	o.handleResult(context.Background(), result)

	o.mu.RLock()
	s1Status := plan.StepStatus["s1"]
	s2Status := plan.StepStatus["s2"]
	o.mu.RUnlock()

	if s1Status != "completed" {
		t.Errorf("expected s1 completed, got %q", s1Status)
	}
	if s2Status != "in_progress" {
		t.Errorf("expected s2 in_progress after s1 completed, got %q", s2Status)
	}

	// s2 should be dispatched to reviewer
	bus.mu.Lock()
	taskCount := len(bus.publishedTasks)
	roles := bus.taskRoles
	bus.mu.Unlock()
	if taskCount < 1 {
		t.Errorf("expected at least 1 dispatched task, got %d", taskCount)
	}
	found := false
	for _, r := range roles {
		if r == "reviewer" {
			found = true
		}
	}
	if !found {
		t.Error("expected a task dispatched to reviewer role")
	}
}

func TestPDAOrchestrator_HandleResult_FailedStep(t *testing.T) {
	bus := &mockPDABus{}
	updater := &mockBeadUpdater{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, &mockBeadCreator{}, updater)

	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "root",
		CorrelationID: "corr-1",
		Plan: messages.PlanData{
			Steps: []messages.PlanStep{
				{StepID: "s1", Role: "coder", Action: "implement"},
			},
		},
		StepBeads:  map[string]string{"s1": "bead-s1"},
		StepStatus: map[string]string{"s1": "in_progress"},
	}
	o.mu.Lock()
	o.activePlans["plan-1"] = plan
	o.mu.Unlock()

	result := &messages.ResultMessage{
		BeadID: "bead-s1",
		Result: messages.ResultData{Status: "failure", Error: "compile error"},
	}
	o.handleResult(context.Background(), result)

	o.mu.RLock()
	s1Status := plan.StepStatus["s1"]
	o.mu.RUnlock()

	if s1Status != "failed" {
		t.Errorf("expected failed, got %q", s1Status)
	}

	// Plan is complete (all steps terminal), should be cleaned up
	if o.ActivePlanCount() != 0 {
		t.Errorf("expected plan to be cleaned up, got %d active", o.ActivePlanCount())
	}
}

func TestPDAOrchestrator_CompletePlan_AllSuccess(t *testing.T) {
	bus := &mockPDABus{}
	updater := &mockBeadUpdater{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, &mockBeadCreator{}, updater)

	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "root",
		CorrelationID: "corr-1",
		StepStatus:    map[string]string{"s1": "completed", "s2": "completed"},
	}
	o.mu.Lock()
	o.activePlans["plan-1"] = plan
	o.mu.Unlock()

	o.completePlan(context.Background(), plan)

	// Source bead should be updated
	updater.mu.Lock()
	updates := updater.updates["root"]
	updater.mu.Unlock()
	if len(updates) == 0 {
		t.Error("expected source bead to be updated on plan completion")
	}

	// Event should be published
	bus.mu.Lock()
	eventCount := len(bus.publishedEvents)
	bus.mu.Unlock()
	if eventCount != 1 {
		t.Errorf("expected 1 completion event, got %d", eventCount)
	}

	// Plan should be deleted
	if o.ActivePlanCount() != 0 {
		t.Errorf("expected 0 active plans after completion, got %d", o.ActivePlanCount())
	}
}

func TestPDAOrchestrator_CompletePlan_WithFailures(t *testing.T) {
	bus := &mockPDABus{}
	updater := &mockBeadUpdater{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, &mockBeadCreator{}, updater)

	plan := &ActivePlan{
		PlanID:        "plan-2",
		ProjectID:     "proj-1",
		SourceBeadID:  "root-2",
		CorrelationID: "corr-2",
		StepStatus:    map[string]string{"s1": "completed", "s2": "failed"},
	}
	o.mu.Lock()
	o.activePlans["plan-2"] = plan
	o.mu.Unlock()

	o.completePlan(context.Background(), plan)

	updater.mu.Lock()
	updates := updater.updates["root-2"]
	updater.mu.Unlock()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	ctx, ok := updates[0]["context"].(map[string]string)
	if !ok {
		t.Fatal("expected context map")
	}
	if ctx["pda_status"] != "completed_with_failures" {
		t.Errorf("expected completed_with_failures, got %q", ctx["pda_status"])
	}
}

func TestPDAOrchestrator_CreateReviewGate(t *testing.T) {
	bus := &mockPDABus{}
	creator := &mockBeadCreator{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, creator, &mockBeadUpdater{})

	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "root",
		CorrelationID: "corr-1",
		Plan: messages.PlanData{
			Priority: 2,
			Steps:    []messages.PlanStep{{StepID: "s1", Role: "coder", Action: "implement"}},
		},
		StepBeads:  map[string]string{"s1": "bead-s1"},
		StepStatus: map[string]string{"s1": "completed"},
	}

	codeResult := &messages.ResultMessage{
		BeadID: "bead-s1",
		Result: messages.ResultData{
			Status:    "success",
			Commits:   []string{"abc123"},
			Artifacts: []string{"main.go"},
		},
	}

	o.createReviewGate(context.Background(), plan, "s1", codeResult)

	// Should have created a review bead and QA bead
	creator.mu.Lock()
	beadCount := creator.count
	creator.mu.Unlock()
	if beadCount < 2 {
		t.Errorf("expected at least 2 beads (review + QA), got %d", beadCount)
	}

	// Review step should be in_progress
	if plan.StepStatus["review-after-s1"] != "in_progress" {
		t.Errorf("expected review step in_progress, got %q", plan.StepStatus["review-after-s1"])
	}

	// QA step should be pending (depends on review)
	if plan.StepStatus["qa-after-review-after-s1"] != "pending" {
		t.Errorf("expected QA step pending, got %q", plan.StepStatus["qa-after-review-after-s1"])
	}

	// Review task dispatched
	bus.mu.Lock()
	taskCount := len(bus.publishedTasks)
	bus.mu.Unlock()
	if taskCount < 1 {
		t.Errorf("expected review task dispatched, got %d tasks", taskCount)
	}
}

func TestPDAOrchestrator_CreateReviewGate_NoDouble(t *testing.T) {
	bus := &mockPDABus{}
	creator := &mockBeadCreator{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, creator, &mockBeadUpdater{})

	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "root",
		CorrelationID: "corr-1",
		Plan:          messages.PlanData{Priority: 1, Steps: []messages.PlanStep{{StepID: "s1"}}},
		StepBeads:     map[string]string{"s1": "bead-s1"},
		StepStatus:    map[string]string{"s1": "completed", "review-after-s1": "in_progress"},
	}

	codeResult := &messages.ResultMessage{
		BeadID: "bead-s1",
		Result: messages.ResultData{Status: "success", Commits: []string{"x"}},
	}

	o.createReviewGate(context.Background(), plan, "s1", codeResult)

	creator.mu.Lock()
	ct := creator.count
	creator.mu.Unlock()
	if ct != 0 {
		t.Errorf("expected no new beads (review gate already exists), got %d", ct)
	}
}

func TestPDAOrchestrator_HandleResult_ImplementTriggersReview(t *testing.T) {
	bus := &mockPDABus{}
	creator := &mockBeadCreator{}
	updater := &mockBeadUpdater{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, creator, updater)

	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "root",
		CorrelationID: "corr-1",
		Plan: messages.PlanData{
			Priority: 1,
			Steps: []messages.PlanStep{
				{StepID: "s1", Role: "coder", Action: "implement"},
				{StepID: "s2", Role: "qa", Action: "test", DependsOn: []string{"s1"}},
			},
		},
		StepBeads:  map[string]string{"s1": "bead-s1", "s2": "bead-s2"},
		StepStatus: map[string]string{"s1": "in_progress", "s2": "pending"},
	}
	o.mu.Lock()
	o.activePlans["plan-1"] = plan
	o.mu.Unlock()

	result := &messages.ResultMessage{
		BeadID:  "bead-s1",
		AgentID: "coder-1",
		Result: messages.ResultData{
			Status:    "success",
			Commits:   []string{"abc"},
			Artifacts: []string{"main.go"},
		},
	}
	o.handleResult(context.Background(), result)

	// Should have auto-created review and QA gate beads
	creator.mu.Lock()
	ct := creator.count
	creator.mu.Unlock()
	if ct < 2 {
		t.Errorf("expected at least 2 gate beads created, got %d", ct)
	}
}

func TestPDAOrchestrator_DispatchReadySteps_AllRoles(t *testing.T) {
	bus := &mockPDABus{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, &mockBeadCreator{}, &mockBeadUpdater{})

	plan := &ActivePlan{
		ProjectID:     "proj-1",
		CorrelationID: "corr-1",
		Plan: messages.PlanData{
			Priority: 1,
			Steps: []messages.PlanStep{
				{StepID: "s1", Role: "coder", Action: "implement"},
				{StepID: "s2", Role: "", Action: "misc"},
			},
		},
		StepBeads:  map[string]string{"s1": "b1", "s2": "b2"},
		StepStatus: map[string]string{"s1": "pending", "s2": "pending"},
	}

	o.dispatchReadySteps(context.Background(), plan)

	bus.mu.Lock()
	taskCount := len(bus.publishedTasks)
	roles := bus.taskRoles
	bus.mu.Unlock()

	if taskCount != 2 {
		t.Errorf("expected 2 tasks dispatched, got %d", taskCount)
	}
	if roles[0] != "coder" {
		t.Errorf("expected role coder, got %q", roles[0])
	}
	if roles[1] != "" {
		t.Errorf("expected empty role for generic dispatch, got %q", roles[1])
	}
}

func TestPDAOrchestrator_Close(t *testing.T) {
	called := false
	cancelFn := func() { called = true }

	o := &PDAOrchestrator{
		activePlans: make(map[string]*ActivePlan),
		cancel:      cancelFn,
	}
	o.Close()
	if !called {
		t.Error("expected cancel to be called")
	}
}

func TestPDAOrchestrator_CloseNilCancel(t *testing.T) {
	o := &PDAOrchestrator{activePlans: make(map[string]*ActivePlan)}
	o.Close()
}

func TestPDAOrchestrator_NewPDAOrchestrator(t *testing.T) {
	bus := &mockPDABus{}
	creator := &mockBeadCreator{}
	updater := &mockBeadUpdater{}
	planner := &StaticPlanner{}

	o := NewPDAOrchestrator(bus, planner, creator, updater)
	if o == nil {
		t.Fatal("expected non-nil orchestrator")
	}
	if o.planner == nil {
		t.Error("planner should be set")
	}
	if o.beadCreator == nil {
		t.Error("bead creator should be set")
	}
	if o.beadUpdater == nil {
		t.Error("bead updater should be set")
	}
	if o.bus == nil {
		t.Error("bus should be set")
	}
	if o.ActivePlanCount() != 0 {
		t.Errorf("expected 0 active plans, got %d", o.ActivePlanCount())
	}
}

func TestActivePlan_Fields(t *testing.T) {
	plan := &ActivePlan{
		PlanID:        "plan-1",
		ProjectID:     "proj-1",
		SourceBeadID:  "bead-root",
		CorrelationID: "corr-1",
		CreatedAt:     time.Now(),
		StepBeads:     map[string]string{"s1": "b1"},
		StepStatus:    map[string]string{"s1": "pending"},
	}
	if plan.PlanID != "plan-1" {
		t.Errorf("got plan ID %q", plan.PlanID)
	}
	if plan.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestPlanRequest_Fields(t *testing.T) {
	req := PlanRequest{
		ProjectID:   "proj-1",
		BeadID:      "bead-1",
		Title:       "Fix bug",
		Description: "Needs fixing",
		Context:     map[string]interface{}{"key": "val"},
	}
	if req.ProjectID != "proj-1" {
		t.Errorf("got project %q", req.ProjectID)
	}
	if req.Context["key"] != "val" {
		t.Error("context mismatch")
	}
}

func TestPDAOrchestrator_ExecutePDA_PlannerError(t *testing.T) {
	bus := &mockPDABus{}
	o := NewPDAOrchestrator(bus, &failingPlanner{}, &mockBeadCreator{}, &mockBeadUpdater{})

	err := o.ExecutePDA(context.Background(), PlanRequest{
		ProjectID: "proj-1", BeadID: "b1", Title: "test",
	})
	if err == nil {
		t.Error("expected error from failing planner")
	}
}

type failingPlanner struct{}

func (f *failingPlanner) GeneratePlan(_ context.Context, _ PlanRequest) (*messages.PlanData, error) {
	return nil, fmt.Errorf("planner error")
}

func TestPDAOrchestrator_HandleResult_UnmatchedResult(t *testing.T) {
	bus := &mockPDABus{}
	o := NewPDAOrchestrator(bus, &StaticPlanner{}, &mockBeadCreator{}, &mockBeadUpdater{})

	plan := &ActivePlan{
		PlanID:     "plan-1",
		StepBeads:  map[string]string{"s1": "bead-s1"},
		StepStatus: map[string]string{"s1": "in_progress"},
	}
	o.mu.Lock()
	o.activePlans["plan-1"] = plan
	o.mu.Unlock()

	result := &messages.ResultMessage{
		BeadID: "no-such-bead",
		Result: messages.ResultData{Status: "success"},
	}
	o.handleResult(context.Background(), result)

	o.mu.RLock()
	if plan.StepStatus["s1"] != "in_progress" {
		t.Error("unmatched result should not change step status")
	}
	o.mu.RUnlock()
}
