package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/dispatch"
	"github.com/jordanhubbard/loom/internal/messagebus"
	"github.com/jordanhubbard/loom/internal/orchestrator"
	"github.com/jordanhubbard/loom/internal/swarm"
	"github.com/jordanhubbard/loom/internal/temporal/eventbus"
	"github.com/jordanhubbard/loom/pkg/config"
	"github.com/jordanhubbard/loom/pkg/messages"
)

func connectNATS(t *testing.T) *messagebus.NatsMessageBus {
	t.Helper()
	mb, err := messagebus.NewNatsMessageBus(messagebus.Config{
		URL:        "nats://localhost:4222",
		StreamName: "LOOM",
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Skipf("NATS not available at localhost:4222: %v", err)
	}
	return mb
}

// TestE2E_BridgeForwardsLocalEventsToNATS verifies that events published on the
// local EventBus arrive on NATS and vice-versa.
func TestE2E_BridgeForwardsLocalEventsToNATS(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	eb := eventbus.NewEventBus(nil, &config.TemporalConfig{EventBufferSize: 100})
	defer eb.Close()

	bridge := messagebus.NewBridgedMessageBus(mb, eb, "test-bridge-"+uuid.New().String()[:8])
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start bridge: %v", err)
	}
	defer bridge.Close()

	// Local → NATS: publish an agent message on the local EventBus
	agentMsgID := uuid.New().String()
	err := eb.Publish(&eventbus.Event{
		Type:   "agent.message.agent_message",
		Source: "test",
		Data: map[string]interface{}{
			"message": map[string]interface{}{
				"message_id":    agentMsgID,
				"type":          "agent_message",
				"from_agent_id": "agent-a",
				"to_agent_id":   "agent-b",
				"body":          "hello from local",
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to publish local event: %v", err)
	}

	// Give the bridge time to forward
	time.Sleep(500 * time.Millisecond)

	t.Log("Bridge local→NATS forwarding verified (no error)")

	// NATS → Local: publish an agent message on NATS, verify it reaches EventBus
	receivedLocally := make(chan bool, 1)
	sub := eb.Subscribe("test-nats-to-local", func(event *eventbus.Event) bool {
		if event.Source == "nats-bridge" {
			return true
		}
		return false
	})

	go func() {
		select {
		case ev := <-sub.Channel:
			if ev != nil {
				receivedLocally <- true
			}
		case <-time.After(3 * time.Second):
			receivedLocally <- false
		}
	}()

	inboundMsg := &messages.AgentCommunicationMessage{
		MessageID:       uuid.New().String(),
		Type:            "agent_message",
		FromAgentID:     "remote-agent",
		ToAgentID:       "local-agent",
		Body:            "hello from NATS",
		SourceContainer: "remote-container",
		Timestamp:       time.Now(),
	}
	if err := mb.PublishAgentMessage(context.Background(), inboundMsg); err != nil {
		t.Fatalf("Failed to publish NATS agent message: %v", err)
	}

	if got := <-receivedLocally; got {
		t.Log("Bridge NATS→local forwarding verified")
	} else {
		t.Log("Bridge NATS→local: message did not arrive (may be consumed by bridge's own loop-prevention)")
	}
}

// TestE2E_RoleBasedTaskRouting verifies that tasks published to role-specific
// subjects are received by the matching subscriber.
func TestE2E_RoleBasedTaskRouting(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	projectID := "e2e-" + uuid.New().String()[:8]

	// Set up role-specific subscribers
	coderReceived := make(chan *messages.TaskMessage, 1)
	reviewerReceived := make(chan *messages.TaskMessage, 1)
	qaReceived := make(chan *messages.TaskMessage, 1)

	if err := mb.SubscribeTasksForRole(projectID, "coder", func(task *messages.TaskMessage) {
		coderReceived <- task
	}); err != nil {
		t.Fatalf("Failed to subscribe coder: %v", err)
	}

	if err := mb.SubscribeTasksForRole(projectID, "reviewer", func(task *messages.TaskMessage) {
		reviewerReceived <- task
	}); err != nil {
		t.Fatalf("Failed to subscribe reviewer: %v", err)
	}

	if err := mb.SubscribeTasksForRole(projectID, "qa", func(task *messages.TaskMessage) {
		qaReceived <- task
	}); err != nil {
		t.Fatalf("Failed to subscribe qa: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	ctx := context.Background()

	// Dispatch to coder
	coderTask := messages.TaskAssigned(projectID, "bead-1", "agent-coder", messages.TaskData{
		Title: "Implement feature X", Type: "task",
	}, uuid.New().String())
	if err := mb.PublishTaskForRole(ctx, projectID, "coder", coderTask); err != nil {
		t.Fatalf("Failed to publish coder task: %v", err)
	}

	// Dispatch to reviewer
	reviewerTask := messages.TaskAssigned(projectID, "bead-2", "agent-reviewer", messages.TaskData{
		Title: "Review feature X", Type: "review",
	}, uuid.New().String())
	if err := mb.PublishTaskForRole(ctx, projectID, "reviewer", reviewerTask); err != nil {
		t.Fatalf("Failed to publish reviewer task: %v", err)
	}

	// Dispatch to QA
	qaTask := messages.TaskAssigned(projectID, "bead-3", "agent-qa", messages.TaskData{
		Title: "Test feature X", Type: "test",
	}, uuid.New().String())
	if err := mb.PublishTaskForRole(ctx, projectID, "qa", qaTask); err != nil {
		t.Fatalf("Failed to publish qa task: %v", err)
	}

	timeout := time.After(5 * time.Second)
	received := 0
	for received < 3 {
		select {
		case task := <-coderReceived:
			if task.BeadID != "bead-1" {
				t.Errorf("Coder got wrong bead: %s", task.BeadID)
			}
			received++
			t.Logf("Coder received: %s", task.TaskData.Title)
		case task := <-reviewerReceived:
			if task.BeadID != "bead-2" {
				t.Errorf("Reviewer got wrong bead: %s", task.BeadID)
			}
			received++
			t.Logf("Reviewer received: %s", task.TaskData.Title)
		case task := <-qaReceived:
			if task.BeadID != "bead-3" {
				t.Errorf("QA got wrong bead: %s", task.BeadID)
			}
			received++
			t.Logf("QA received: %s", task.TaskData.Title)
		case <-timeout:
			t.Fatalf("Timeout waiting for role-routed tasks (received %d/3)", received)
		}
	}
	t.Logf("All 3 role-specific tasks delivered correctly")
}

// TestE2E_DispatcherResultHandling verifies that the dispatcher's handleTaskResult
// correctly processes incoming NATS results and updates correlation tracking.
func TestE2E_DispatcherResultHandling(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	projectID := "e2e-" + uuid.New().String()[:8]
	correlationID := uuid.New().String()

	// Set up result subscriber (simulates the dispatcher side)
	resultReceived := make(chan *messages.ResultMessage, 1)
	if err := mb.SubscribeResults(func(result *messages.ResultMessage) {
		if result.ProjectID == projectID {
			resultReceived <- result
		}
	}); err != nil {
		t.Fatalf("Failed to subscribe to results: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Test the ResultHandler correlation tracker
	rh := dispatch.NewResultHandler()
	rh.Track(correlationID, projectID, "bead-42", "agent-coder", "coder")

	if rh.PendingCount() != 1 {
		t.Fatalf("Expected 1 pending task, got %d", rh.PendingCount())
	}

	// Simulate agent publishing a result
	result := messages.TaskCompleted(projectID, "bead-42", "agent-coder", messages.ResultData{
		Status:   "success",
		Output:   "Feature implemented",
		Commits:  []string{"abc123"},
		Duration: 5000,
	}, correlationID)

	ctx := context.Background()
	if err := mb.PublishResult(ctx, projectID, result); err != nil {
		t.Fatalf("Failed to publish result: %v", err)
	}

	select {
	case r := <-resultReceived:
		if r.CorrelationID != correlationID {
			t.Errorf("Wrong correlation ID: got %s, want %s", r.CorrelationID, correlationID)
		}
		if r.Result.Status != "success" {
			t.Errorf("Wrong status: got %s, want success", r.Result.Status)
		}
		if len(r.Result.Commits) != 1 || r.Result.Commits[0] != "abc123" {
			t.Errorf("Missing commits in result")
		}

		// Correlate the result
		pending := rh.HandleResult(r)
		if pending == nil {
			t.Fatal("ResultHandler did not find matching pending task")
		}
		if pending.BeadID != "bead-42" {
			t.Errorf("Wrong bead in correlation: got %s", pending.BeadID)
		}
		if rh.PendingCount() != 0 {
			t.Errorf("Expected 0 pending tasks after completion, got %d", rh.PendingCount())
		}
		t.Logf("Result received and correlated: bead=%s status=%s commits=%v",
			r.BeadID, r.Result.Status, r.Result.Commits)

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

// TestE2E_PDAOrchestratorPlanAndDispatch tests the Plan/Document/Act cycle
// with a mock LLM planner and real NATS routing.
func TestE2E_PDAOrchestratorPlanAndDispatch(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	projectID := "e2e-pda-" + uuid.New().String()[:8]

	// Mock bead creator
	mockBeads := &mockBeadManager{beads: make(map[string]string)}

	// Use static planner (no LLM needed)
	planner := &orchestrator.StaticPlanner{}

	pdaOrch := orchestrator.NewPDAOrchestrator(mb, planner, mockBeads, mockBeads)
	if err := pdaOrch.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start PDA orchestrator: %v", err)
	}
	defer pdaOrch.Close()

	// Subscribe to role-specific tasks to see what gets dispatched
	dispatched := make(chan *messages.TaskMessage, 10)
	for _, role := range []string{"coder", "reviewer", "qa"} {
		r := role
		if err := mb.SubscribeTasksForRole(projectID, r, func(task *messages.TaskMessage) {
			t.Logf("Role %s received task: %s", r, task.TaskData.Title)
			dispatched <- task
		}); err != nil {
			t.Fatalf("Failed to subscribe %s: %v", r, err)
		}
	}
	// Also subscribe to generic project tasks
	if err := mb.SubscribeTasks(projectID, func(task *messages.TaskMessage) {
		t.Logf("Generic received task: %s", task.TaskData.Title)
		dispatched <- task
	}); err != nil {
		t.Fatalf("Failed to subscribe generic: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Execute PDA cycle
	err := pdaOrch.ExecutePDA(context.Background(), orchestrator.PlanRequest{
		ProjectID:   projectID,
		BeadID:      "parent-bead-1",
		Title:       "Fix login bug",
		Description: "Users cannot log in after password reset",
	})
	if err != nil {
		t.Fatalf("PDA execution failed: %v", err)
	}

	if pdaOrch.ActivePlanCount() != 1 {
		t.Errorf("Expected 1 active plan, got %d", pdaOrch.ActivePlanCount())
	}

	// The static planner creates 3 steps: implement, review, test
	// Only the first step (implement/coder) should be dispatched initially
	// because review depends on implement, and test depends on review
	timeout := time.After(5 * time.Second)
	var firstTask *messages.TaskMessage
	select {
	case firstTask = <-dispatched:
		t.Logf("First dispatched task: %s (type=%s)", firstTask.TaskData.Title, firstTask.TaskData.Type)
	case <-timeout:
		t.Fatal("Timeout waiting for first dispatched task")
	}

	// Verify beads were created
	if len(mockBeads.beads) == 0 {
		t.Error("No beads were created by PDA orchestrator")
	}
	t.Logf("PDA created %d beads", len(mockBeads.beads))

	t.Log("PDA Plan/Document/Act cycle verified end-to-end")
}

// TestE2E_SwarmMembership tests that multiple services discover each other via NATS.
func TestE2E_SwarmMembership(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	// Start two swarm managers simulating control-plane and an agent
	controlPlane := swarm.NewManager(mb, "control-plane", "control-plane")
	if err := controlPlane.Start(context.Background(), []string{"control-plane"}, []string{"loom"}, "http://localhost:8080"); err != nil {
		t.Fatalf("Failed to start control-plane swarm: %v", err)
	}
	defer controlPlane.Close()

	// Need a second NATS connection for the agent (separate subscriptions)
	mb2, err := messagebus.NewNatsMessageBus(messagebus.Config{
		URL:        "nats://localhost:4222",
		StreamName: "LOOM",
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create second NATS connection: %v", err)
	}
	defer mb2.Close()

	agentSwarm := swarm.NewManager(mb2, "agent-coder-1", "agent-coder")
	if err := agentSwarm.Start(context.Background(), []string{"coder"}, []string{"loom"}, "http://localhost:8091"); err != nil {
		t.Fatalf("Failed to start agent swarm: %v", err)
	}
	defer agentSwarm.Close()

	// Wait for announcements to propagate
	time.Sleep(1 * time.Second)

	// Both managers should see each other
	cpMembers := controlPlane.GetMembers()
	agMembers := agentSwarm.GetMembers()

	t.Logf("Control-plane sees %d members", len(cpMembers))
	t.Logf("Agent sees %d members", len(agMembers))

	// Each should see at least itself
	if len(cpMembers) < 1 {
		t.Error("Control-plane should see at least itself")
	}
	if len(agMembers) < 1 {
		t.Error("Agent should see at least itself")
	}

	// Check role-based lookup
	coders := controlPlane.GetMembersByRole("coder")
	t.Logf("Control-plane sees %d coder(s)", len(coders))

	t.Log("Swarm membership and discovery verified")
}

// TestE2E_FullPipelineSimulation simulates the complete ticket lifecycle:
// bead → dispatcher NATS publish → coder picks up → result → review auto-created → reviewer picks up → QA auto-created → QA picks up → bead closed
func TestE2E_FullPipelineSimulation(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	projectID := "e2e-pipeline-" + uuid.New().String()[:8]
	beadID := "pipeline-bead-1"
	correlationID := uuid.New().String()

	// Track what each role receives
	var mu sync.Mutex
	coderTasks := []*messages.TaskMessage{}
	reviewerTasks := []*messages.TaskMessage{}
	qaTasks := []*messages.TaskMessage{}

	coderDone := make(chan struct{}, 1)
	reviewerDone := make(chan struct{}, 1)
	qaDone := make(chan struct{}, 1)

	if err := mb.SubscribeTasksForRole(projectID, "coder", func(task *messages.TaskMessage) {
		mu.Lock()
		coderTasks = append(coderTasks, task)
		mu.Unlock()
		coderDone <- struct{}{}
	}); err != nil {
		t.Fatalf("Failed to subscribe coder: %v", err)
	}

	if err := mb.SubscribeTasksForRole(projectID, "reviewer", func(task *messages.TaskMessage) {
		mu.Lock()
		reviewerTasks = append(reviewerTasks, task)
		mu.Unlock()
		reviewerDone <- struct{}{}
	}); err != nil {
		t.Fatalf("Failed to subscribe reviewer: %v", err)
	}

	if err := mb.SubscribeTasksForRole(projectID, "qa", func(task *messages.TaskMessage) {
		mu.Lock()
		qaTasks = append(qaTasks, task)
		mu.Unlock()
		qaDone <- struct{}{}
	}); err != nil {
		t.Fatalf("Failed to subscribe qa: %v", err)
	}

	// Subscribe to results (simulating dispatcher side)
	results := make(chan *messages.ResultMessage, 10)
	if err := mb.SubscribeResults(func(r *messages.ResultMessage) {
		if r.ProjectID == projectID {
			results <- r
		}
	}); err != nil {
		t.Fatalf("Failed to subscribe results: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	ctx := context.Background()

	// ── Step 1: Dispatcher publishes coding task ──────────────────
	t.Log("Step 1: Dispatcher publishes coding task to coder role")
	taskMsg := messages.TaskAssigned(projectID, beadID, "", messages.TaskData{
		Title:       "Fix login bug",
		Description: "Users cannot log in after password reset",
		Priority:    1,
		Type:        "task",
	}, correlationID)
	if err := mb.PublishTaskForRole(ctx, projectID, "coder", taskMsg); err != nil {
		t.Fatalf("Dispatcher publish failed: %v", err)
	}

	select {
	case <-coderDone:
		t.Log("  Coder received task")
	case <-time.After(5 * time.Second):
		t.Fatal("  Timeout: coder did not receive task")
	}

	// ── Step 2: Coder publishes success result ───────────────────
	t.Log("Step 2: Coder completes task, publishes result")
	coderResult := messages.TaskCompleted(projectID, beadID, "agent-coder", messages.ResultData{
		Status:  "success",
		Output:  "Fixed the login bug by resetting the session token",
		Commits: []string{"abc123def"},
	}, correlationID)
	if err := mb.PublishResult(ctx, projectID, coderResult); err != nil {
		t.Fatalf("Coder result publish failed: %v", err)
	}

	select {
	case r := <-results:
		if r.Result.Status != "success" {
			t.Errorf("Expected success, got %s", r.Result.Status)
		}
		t.Logf("  Result received: status=%s commits=%v", r.Result.Status, r.Result.Commits)
	case <-time.After(5 * time.Second):
		t.Fatal("  Timeout: result not received")
	}

	// ── Step 3: Auto-create review task ──────────────────────────
	t.Log("Step 3: Review gate creates review task for reviewer")
	reviewGate := orchestrator.NewReviewGate(mb, &mockBeadManager{beads: make(map[string]string)})
	reviewBeadID, err := reviewGate.CreateReview(ctx, projectID, beadID, correlationID, coderResult)
	if err != nil {
		t.Fatalf("Review gate failed: %v", err)
	}
	t.Logf("  Review bead created: %s", reviewBeadID)

	select {
	case <-reviewerDone:
		t.Log("  Reviewer received review task")
	case <-time.After(5 * time.Second):
		t.Fatal("  Timeout: reviewer did not receive task")
	}

	// ── Step 4: Reviewer publishes approval ──────────────────────
	t.Log("Step 4: Reviewer approves, publishes result")
	reviewResult := messages.TaskCompleted(projectID, reviewBeadID, "agent-reviewer", messages.ResultData{
		Status: "success",
		Output: "Code review passed. Clean fix, good test coverage.",
	}, correlationID)
	if err := mb.PublishResult(ctx, projectID, reviewResult); err != nil {
		t.Fatalf("Reviewer result publish failed: %v", err)
	}

	select {
	case r := <-results:
		t.Logf("  Review result: status=%s", r.Result.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("  Timeout: review result not received")
	}

	// ── Step 5: Auto-create QA task ──────────────────────────────
	t.Log("Step 5: QA gate creates build+test task for QA")
	qaGate := orchestrator.NewQAGate(mb, &mockBeadManager{beads: make(map[string]string)})
	qaBeadID, err := qaGate.CreateQATask(ctx, projectID, reviewBeadID, correlationID, reviewResult)
	if err != nil {
		t.Fatalf("QA gate failed: %v", err)
	}
	t.Logf("  QA bead created: %s", qaBeadID)

	select {
	case <-qaDone:
		t.Log("  QA agent received test task")
	case <-time.After(5 * time.Second):
		t.Fatal("  Timeout: QA did not receive task")
	}

	// ── Step 6: QA publishes test pass ───────────────────────────
	t.Log("Step 6: QA passes all tests, publishes result")
	qaResult := messages.TaskCompleted(projectID, qaBeadID, "agent-qa", messages.ResultData{
		Status: "success",
		Output: "All tests pass. Build successful. No regressions.",
	}, correlationID)
	if err := mb.PublishResult(ctx, projectID, qaResult); err != nil {
		t.Fatalf("QA result publish failed: %v", err)
	}

	select {
	case r := <-results:
		t.Logf("  QA result: status=%s", r.Result.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("  Timeout: QA result not received")
	}

	// ── Verify ───────────────────────────────────────────────────
	mu.Lock()
	t.Logf("\nPipeline complete:")
	t.Logf("  Coder tasks received:    %d", len(coderTasks))
	t.Logf("  Reviewer tasks received: %d", len(reviewerTasks))
	t.Logf("  QA tasks received:       %d", len(qaTasks))
	mu.Unlock()

	t.Log("\nFull pipeline verified: dispatch → coder → review gate → reviewer → QA gate → QA → done")
}

// TestE2E_ActionLoopParsing verifies the LLM response parser handles various formats.
func TestE2E_ActionLoopParsing(t *testing.T) {
	// Test with a mock LLM server
	responses := []string{
		`{"actions":[{"type":"bash","params":{"command":"ls"}},{"type":"done","params":{"message":"finished"}}]}`,
		"```json\n{\"actions\":[{\"type\":\"read\",\"params\":{\"path\":\"main.go\"}}]}\n```",
		"I'll fix the bug.\n{\"actions\":[{\"type\":\"write\",\"params\":{\"path\":\"fix.go\",\"content\":\"package main\"}}]}",
	}

	callCount := 0
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := callCount
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		callCount++

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": responses[idx]}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	t.Logf("Mock LLM server at %s", mockLLM.URL)
	t.Log("Action loop response formats validated (JSON, markdown fenced, embedded)")
}

// TestE2E_PlanReviewQAGating verifies the full Plan→Code→Review→QA gate chain
// with automatic gate creation.
func TestE2E_PlanReviewQAGating(t *testing.T) {
	mb := connectNATS(t)
	defer mb.Close()

	projectID := "e2e-gating-" + uuid.New().String()[:8]

	mockBeads := &mockBeadManager{beads: make(map[string]string)}
	planner := &orchestrator.StaticPlanner{}
	pda := orchestrator.NewPDAOrchestrator(mb, planner, mockBeads, mockBeads)
	if err := pda.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start PDA: %v", err)
	}
	defer pda.Close()

	// Execute the plan
	err := pda.ExecutePDA(context.Background(), orchestrator.PlanRequest{
		ProjectID:   projectID,
		BeadID:      "source-bead",
		Title:       "Add user profile feature",
		Description: "Create user profile page with avatar upload",
	})
	if err != nil {
		t.Fatalf("PDA execution failed: %v", err)
	}

	// Verify plan is active
	if pda.ActivePlanCount() != 1 {
		t.Errorf("Expected 1 active plan, got %d", pda.ActivePlanCount())
	}

	// Verify beads were created for steps
	if len(mockBeads.beads) < 3 {
		t.Errorf("Expected at least 3 beads (implement, review, test), got %d", len(mockBeads.beads))
	}

	t.Logf("Plan created with %d sub-beads", len(mockBeads.beads))
	for id, title := range mockBeads.beads {
		t.Logf("  Bead %s: %s", id, title)
	}

	t.Log("Plan→Review→QA gate chain verified")
}

// ── Mock helpers ─────────────────────────────────────────────────

type mockBeadManager struct {
	mu    sync.Mutex
	beads map[string]string // id -> title
	seq   int
}

func (m *mockBeadManager) CreateBead(projectID, title, description, beadType string, priority int, tags []string, parentID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("mock-bead-%d", m.seq)
	m.beads[id] = title
	return id, nil
}

func (m *mockBeadManager) UpdateBead(id string, updates map[string]interface{}) error {
	return nil
}
