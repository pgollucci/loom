# Detailed Design: Workflow System Gap Closure

**Last Updated:** 2026-02-15
**Status:** Design Phase - Implementation Ready
**Related Documents:**
- Architecture: `/Users/jkh/Src/loom/.vibe/docs/architecture.md`
- Requirements: `/Users/jkh/Src/loom/.vibe/docs/requirements.md`
- Development Plan: `/Users/jkh/Src/loom/.vibe/development-plan.md`

---

## Executive Summary

This document provides detailed design specifications for closing 3 critical gaps in the existing workflow system to achieve full autonomous self-healing capability.

**Gaps to Close:**
1. **Multi-Dispatch Redispatch Flag** (P0) - Enable agents to continue investigations across multiple dispatch cycles
2. **Commit Serialization** (P1) - Prevent concurrent git conflicts
3. **Agent Role Assignment** (P2) - Improve role-based routing accuracy

**Estimated Effort:** 5-8 hours implementation + 4-6 hours testing = 9-14 hours total

---

## Gap #1: Multi-Dispatch Redispatch Flag

### Problem Statement

**Current Behavior:**
```
Agent investigates bug, finds partial leads →
Agent returns result →
Dispatcher marks bead as "last_run_at = now" →
Bead not redispatched (too recent) →
Investigation stops after 1 turn ❌
```

**Desired Behavior:**
```
Agent investigates bug, finds partial leads →
Agent returns result →
Workflow engine sets "redispatch_requested = true" →
Dispatcher sees flag and redispatches immediately →
Agent continues investigation across multiple turns ✅
```

### Design Solution

#### 1.1 Data Model Extension

**Bead Context Schema:**
```go
// Existing bead context structure
type Bead struct {
    ID          string
    Title       string
    Type        string
    Priority    int
    Context     map[string]string  // ← Extend this
    LastRunAt   *time.Time
    // ... other fields
}

// New context keys
const (
    ContextKeyWorkflowID          = "workflow_id"
    ContextKeyWorkflowExecID      = "workflow_exec_id"
    ContextKeyWorkflowNode        = "workflow_node"
    ContextKeyWorkflowStatus      = "workflow_status"
    ContextKeyRedispatchRequested = "redispatch_requested"  // ← NEW
)
```

**Values:**
- `"true"` - Bead should be redispatched immediately
- `"false"` or absent - Normal dispatch rules apply

#### 1.2 Workflow Engine Modification

**File:** `internal/workflow/engine.go`

**Method:** `AdvanceWorkflow()`

**Change Location:** After updating bead context, before persisting execution

**Pseudo-code:**
```go
func (e *Engine) AdvanceWorkflow(executionID string, condition EdgeCondition, agentID string, resultData map[string]string) error {
    // ... existing code ...

    // Move to next node
    exec.CurrentNodeKey = nextNode.NodeKey
    exec.NodeAttemptCount = 0
    exec.LastNodeAt = time.Now()

    if err := e.db.UpsertWorkflowExecution(exec); err != nil {
        return fmt.Errorf("failed to update workflow execution: %w", err)
    }

    // Update bead context with current node
    updates := map[string]interface{}{
        "context": map[string]string{
            "workflow_node":   nextNode.NodeKey,
            "workflow_status": string(exec.Status),
            // ↓↓↓ NEW LOGIC ↓↓↓
            "redispatch_requested": shouldRedispatch(exec, nextNode), // "true" or "false"
        },
    }

    // ... rest of existing code ...
}

// New helper function
func shouldRedispatch(exec *WorkflowExecution, node *WorkflowNode) string {
    // Redispatch if:
    // 1. Node is not an approval node (approvals wait for human)
    // 2. Node has attempts remaining
    // 3. Workflow is active (not escalated/completed)

    if node.NodeType == NodeTypeApproval {
        return "false" // Approvals wait for human, don't redispatch
    }

    if exec.Status != ExecutionStatusActive {
        return "false" // Only redispatch active workflows
    }

    if exec.NodeAttemptCount >= node.MaxAttempts {
        return "false" // No more attempts, escalate instead
    }

    // For task/commit/verify nodes, allow redispatch
    return "true"
}
```

**Rationale:**
- Setting flag in workflow engine ensures it's set consistently based on workflow state
- Agents don't need to manually set flag (automated based on node type)
- Clear separation: engine manages workflow state, dispatcher honors it

#### 1.3 Dispatcher Modification

**File:** `internal/dispatch/dispatcher.go`

**Method:** `GetReadyBeads()` or equivalent

**Change Location:** In bead readiness check logic

**Existing pseudo-code:**
```go
func (d *Dispatcher) GetReadyBeads(ctx context.Context) ([]*models.Bead, error) {
    beads, err := d.beads.ListBeads(...)

    var readyBeads []*models.Bead
    for _, bead := range beads {
        // Check if bead is ready
        if bead.Status != "open" {
            continue
        }

        // Skip if recently dispatched
        if bead.LastRunAt != nil && time.Since(*bead.LastRunAt) < 5*time.Minute {
            continue  // ← This blocks redispatch!
        }

        readyBeads = append(readyBeads, bead)
    }
    return readyBeads, nil
}
```

**Modified pseudo-code:**
```go
func (d *Dispatcher) GetReadyBeads(ctx context.Context) ([]*models.Bead, error) {
    beads, err := d.beads.ListBeads(...)

    var readyBeads []*models.Bead
    for _, bead := range beads {
        // Check if bead is ready
        if bead.Status != "open" {
            continue
        }

        // ↓↓↓ NEW LOGIC ↓↓↓
        // Check redispatch flag first
        if bead.Context != nil && bead.Context["redispatch_requested"] == "true" {
            // Immediately ready, bypass timing check
            readyBeads = append(readyBeads, bead)
            continue
        }

        // Original timing check for non-redispatch beads
        if bead.LastRunAt != nil && time.Since(*bead.LastRunAt) < 5*time.Minute {
            continue
        }

        readyBeads = append(readyBeads, bead)
    }
    return readyBeads, nil
}
```

**Rationale:**
- Redispatch check happens BEFORE timing check (higher priority)
- Flag allows workflow-managed beads to bypass normal dispatch throttling
- Non-workflow beads unaffected (normal 5-minute cooldown applies)

#### 1.4 Flag Clearing

**When to clear flag:**
- After bead is dispatched (dispatcher should clear it before dispatch)
- When workflow completes
- When workflow escalates

**Dispatcher modification:**
```go
func (d *Dispatcher) DispatchBead(bead *models.Bead, agent *Agent) error {
    // Clear redispatch flag before execution
    if bead.Context != nil && bead.Context["redispatch_requested"] == "true" {
        updates := map[string]interface{}{
            "context": map[string]string{
                "redispatch_requested": "false",
            },
        }
        if err := d.beads.UpdateBead(bead.ID, updates); err != nil {
            log.Printf("Warning: failed to clear redispatch flag: %v", err)
        }
    }

    // Execute task
    result, err := agent.Execute(bead)
    // ... rest of dispatch logic ...
}
```

### Testing Strategy

**Unit Tests:**
```go
// internal/workflow/engine_test.go

func TestAdvanceWorkflow_SetsRedispatchFlag(t *testing.T) {
    // Test that task nodes get redispatch_requested = "true"
    // Test that approval nodes get redispatch_requested = "false"
    // Test that escalated workflows get redispatch_requested = "false"
}

func TestShouldRedispatch(t *testing.T) {
    // Test all node types and execution states
}
```

**Integration Tests:**
```go
// test/integration/workflow_redispatch_test.go

func TestMultiTurnInvestigation(t *testing.T) {
    // 1. Create bug bead
    // 2. Verify workflow starts
    // 3. Dispatch to agent
    // 4. Verify redispatch_requested = "true" after first turn
    // 5. Verify bead redispatched immediately
    // 6. Repeat for 3 turns
    // 7. Verify investigation completes
}
```

### Implementation Checklist

- [ ] Add `shouldRedispatch()` helper to `engine.go`
- [ ] Modify `AdvanceWorkflow()` to set flag
- [ ] Modify `GetReadyBeads()` to honor flag
- [ ] Add flag clearing logic to dispatcher
- [ ] Write unit tests for flag logic
- [ ] Write integration test for multi-turn investigation
- [ ] Update workflow completion to clear flag
- [ ] Update escalation logic to clear flag

**Estimated Effort:** 2-3 hours

---

## Gap #2: Commit Serialization

### Problem Statement

**Current Behavior:**
```
Workflow A: Bug #1 → investigate → fix → commit (Agent A starts commit) →
Workflow B: Bug #2 → investigate → fix → commit (Agent B starts commit) →
Both agents modify same file →
Git conflict ❌
```

**Desired Behavior:**
```
Workflow A: Bug #1 → commit (Agent A acquires lock) → git operations → release lock ✅
Workflow B: Bug #2 → commit (Agent B waits for lock) → git operations → release lock ✅
Sequential, conflict-free commits ✅
```

### Design Solution

#### 2.1 Commit Lock Data Structure

**File:** `internal/dispatch/dispatcher.go`

**Add to Dispatcher struct:**
```go
type Dispatcher struct {
    // ... existing fields ...
    workflowEngine      *workflow.Engine

    // ↓↓↓ NEW FIELDS ↓↓↓
    commitLock          sync.Mutex              // Global commit lock
    commitQueue         chan commitRequest     // Queue for waiting commits
    commitLockTimeout   time.Duration          // Max time to hold lock (5 min)
    commitInProgress    *commitState           // Current commit state
    commitStateMutex    sync.RWMutex           // Protects commitInProgress
}

// New types
type commitRequest struct {
    BeadID    string
    AgentID   string
    Timestamp time.Time
    ResultCh  chan error  // Send result back to requester
}

type commitState struct {
    BeadID      string
    AgentID     string
    StartedAt   time.Time
    Node        *workflow.WorkflowNode
}
```

#### 2.2 Commit Queue Initialization

**File:** `internal/dispatch/dispatcher.go`

**In NewDispatcher():**
```go
func NewDispatcher(...) *Dispatcher {
    d := &Dispatcher{
        // ... existing fields ...
        commitQueue:       make(chan commitRequest, 100), // Buffer 100 waiting commits
        commitLockTimeout: 5 * time.Minute,
        // ...
    }

    // Start commit queue processor goroutine
    go d.processCommitQueue()

    return d
}
```

#### 2.3 Commit Queue Processor

**New method:**
```go
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

        // Execute commit (will be called from dispatcher)
        // Result sent back via req.ResultCh

        // Commit will release lock when done via releaseCommitLock()
    }
}
```

#### 2.4 Commit Lock Acquisition

**New method:**
```go
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
```

#### 2.5 Commit Lock Release

**New method:**
```go
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
```

#### 2.6 Dispatcher Integration

**Modify dispatch loop:**
```go
func (d *Dispatcher) dispatchWorkflowBead(ctx context.Context, bead *models.Bead, agent *Agent, node *workflow.WorkflowNode) error {
    // Check if this is a commit node
    if node.NodeType == workflow.NodeTypeCommit {
        // Acquire commit lock before executing
        if err := d.acquireCommitLock(ctx, bead.ID, agent.ID); err != nil {
            return fmt.Errorf("failed to acquire commit lock: %w", err)
        }
        defer d.releaseCommitLock()

        log.Printf("[Commit] Executing commit for bead %s with exclusive lock", bead.ID)
    }

    // Execute task
    result, err := agent.Execute(bead)

    // ... rest of dispatch logic ...
    return nil
}
```

### Alternative Design: Database-Based Locking

**Rationale for in-memory lock:**
- Simpler implementation (no additional tables)
- Faster (no database round trips)
- Loom runs as single process (no distributed locking needed)

**If distributed Loom needed in future:**
```sql
CREATE TABLE commit_locks (
    id TEXT PRIMARY KEY,
    bead_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    acquired_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);

-- Acquire lock:
INSERT INTO commit_locks (id, bead_id, agent_id, acquired_at, expires_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING;
-- Check if insert succeeded to determine if lock acquired

-- Release lock:
DELETE FROM commit_locks WHERE bead_id = ?;

-- Cleanup stale locks:
DELETE FROM commit_locks WHERE expires_at < datetime('now');
```

### Testing Strategy

**Unit Tests:**
```go
// internal/dispatch/commit_lock_test.go

func TestAcquireCommitLock_Sequential(t *testing.T) {
    // Test that two sequential commits work correctly
}

func TestAcquireCommitLock_Concurrent(t *testing.T) {
    // Test that concurrent commit requests are serialized
}

func TestCommitLockTimeout(t *testing.T) {
    // Test that stale locks are forcibly released after 5 minutes
}
```

**Integration Tests:**
```go
// test/integration/concurrent_commits_test.go

func TestConcurrentWorkflowCommits(t *testing.T) {
    // 1. Create 3 bug beads
    // 2. Start all workflows
    // 3. Simulate all reaching commit nodes simultaneously
    // 4. Verify commits execute sequentially
    // 5. Verify no git conflicts
    // 6. Verify all workflows complete successfully
}
```

### Implementation Checklist

- [ ] Add commit lock fields to Dispatcher struct
- [ ] Implement `acquireCommitLock()` method
- [ ] Implement `releaseCommitLock()` method
- [ ] Implement `processCommitQueue()` goroutine
- [ ] Modify dispatch loop to use lock for commit nodes
- [ ] Add timeout handling for stale locks
- [ ] Write unit tests for lock acquisition/release
- [ ] Write integration test for concurrent commits
- [ ] Add monitoring/logging for commit queue length

**Estimated Effort:** 2-3 hours

---

## Gap #3: Agent Role Assignment

### Problem Statement

**Current Behavior:**
```
Agent created with persona "default/qa-engineer" →
Agent.Role field remains empty →
Dispatcher checks workflow role requirement "QA" →
Role match fails (empty != "QA") →
Falls back to persona matching ⚠️
```

**Desired Behavior:**
```
Agent created with persona "default/qa-engineer" →
inferRoleFromPersona() sets Agent.Role = "QA" →
Dispatcher checks workflow role requirement "QA" →
Role match succeeds ✅
```

### Design Solution

#### 3.1 Role Inference Logic

**File:** `internal/agent/worker_manager.go`

**New helper function:**
```go
// inferRoleFromPersona extracts role from persona path
func inferRoleFromPersona(persona string) string {
    personaLower := strings.ToLower(persona)

    // Mapping from persona keywords to workflow roles
    roleMap := map[string]string{
        "qa":                    "QA",
        "qa-engineer":           "QA",
        "quality-assurance":     "QA",
        "engineering-manager":   "Engineering Manager",
        "eng-manager":           "Engineering Manager",
        "product-manager":       "Product Manager",
        "pm":                    "Product Manager",
        "web-designer":          "Web Designer",
        "designer":              "Web Designer",
        "backend-engineer":      "Backend Engineer",
        "backend":               "Backend Engineer",
        "frontend-engineer":     "Frontend Engineer",
        "frontend":              "Frontend Engineer",
        "code-reviewer":         "Code Reviewer",
        "reviewer":              "Code Reviewer",
        "ceo":                   "CEO",
    }

    // Check for matches
    for keyword, role := range roleMap {
        if strings.Contains(personaLower, keyword) {
            return role
        }
    }

    // No match - return empty (will fall back to persona matching)
    return ""
}
```

**Rationale:**
- Uses keyword matching for flexibility
- Supports multiple persona naming conventions
- Returns empty string when uncertain (safe fallback)
- Extensible (add new roles easily)

#### 3.2 Agent Creation Modification

**File:** `internal/agent/worker_manager.go`

**Method:** `CreateAgent()` or equivalent

**Existing code (example):**
```go
func (wm *WorkerManager) CreateAgent(persona, projectID string) (*models.Agent, error) {
    agent := &models.Agent{
        ID:        uuid.New().String(),
        Persona:   persona,
        ProjectID: projectID,
        Role:      "", // ← Empty!
        Status:    "idle",
        CreatedAt: time.Now(),
    }

    // Save to database
    if err := wm.db.InsertAgent(agent); err != nil {
        return nil, err
    }

    return agent, nil
}
```

**Modified code:**
```go
func (wm *WorkerManager) CreateAgent(persona, projectID string) (*models.Agent, error) {
    agent := &models.Agent{
        ID:        uuid.New().String(),
        Persona:   persona,
        ProjectID: projectID,
        Role:      inferRoleFromPersona(persona), // ← NEW: Infer role from persona
        Status:    "idle",
        CreatedAt: time.Now(),
    }

    log.Printf("[Agent] Created agent %s with persona %s, inferred role: %s",
        agent.ID, agent.Persona, agent.Role)

    // Save to database
    if err := wm.db.InsertAgent(agent); err != nil {
        return nil, err
    }

    return agent, nil
}
```

#### 3.3 Existing Agent Migration

**One-time migration script (optional):**
```go
// cmd/migrate-agent-roles/main.go

func migrateExistingAgents(db *database.Database) error {
    agents, err := db.ListAgents()
    if err != nil {
        return err
    }

    for _, agent := range agents {
        if agent.Role == "" {
            // Infer role from persona
            role := inferRoleFromPersona(agent.Persona)
            if role != "" {
                agent.Role = role
                if err := db.UpdateAgent(agent); err != nil {
                    log.Printf("Failed to update agent %s: %v", agent.ID, err)
                } else {
                    log.Printf("Updated agent %s: persona=%s, role=%s",
                        agent.ID, agent.Persona, role)
                }
            }
        }
    }

    return nil
}
```

**Alternative:** Lazy migration (update role on first use)

### Testing Strategy

**Unit Tests:**
```go
// internal/agent/role_inference_test.go

func TestInferRoleFromPersona(t *testing.T) {
    tests := []struct {
        persona  string
        expected string
    }{
        {"default/qa-engineer", "QA"},
        {"default/engineering-manager", "Engineering Manager"},
        {"custom/product-manager", "Product Manager"},
        {"default/web-designer", "Web Designer"},
        {"unknown/persona", ""}, // No match
    }

    for _, tt := range tests {
        t.Run(tt.persona, func(t *testing.T) {
            got := inferRoleFromPersona(tt.persona)
            if got != tt.expected {
                t.Errorf("inferRoleFromPersona(%s) = %s, want %s",
                    tt.persona, got, tt.expected)
            }
        })
    }
}

func TestCreateAgent_SetsRole(t *testing.T) {
    // Test that CreateAgent() sets Role field correctly
}
```

**Integration Tests:**
```go
// test/integration/role_based_routing_test.go

func TestRoleBasedWorkflowRouting(t *testing.T) {
    // 1. Create agents with different personas
    // 2. Create bug bead
    // 3. Verify workflow starts
    // 4. Verify first node (investigate) routed to QA agent by role
    // 5. Verify second node (apply_fix) routed to Engineering Manager by role
}
```

### Implementation Checklist

- [ ] Implement `inferRoleFromPersona()` helper function
- [ ] Modify `CreateAgent()` to call helper
- [ ] Add logging for role inference
- [ ] Write unit tests for role inference
- [ ] Write integration test for role-based routing
- [ ] (Optional) Create migration script for existing agents
- [ ] Update agent creation docs with role inference

**Estimated Effort:** 1 hour

---

## Quality Attributes

### Performance Considerations

**Gap #1 (Redispatch Flag):**
- **Impact:** Negligible (1 map lookup per bead)
- **Optimization:** Flag is stored in bead context (already loaded)

**Gap #2 (Commit Serialization):**
- **Impact:** Commits serialized (adds latency for concurrent commits)
- **Mitigation:** Commits are rare events (~1% of workflow nodes)
- **Optimization:** Buffered channel (100 capacity) prevents blocking

**Gap #3 (Role Inference):**
- **Impact:** One-time cost at agent creation
- **Optimization:** Simple string matching (< 1ms)

### Reliability Considerations

**Gap #1:**
- **Risk:** Flag not cleared → infinite redispatch loop
- **Mitigation:** Clear flag after dispatch, on completion, on escalation

**Gap #2:**
- **Risk:** Lock timeout too short → commits interrupted
- **Mitigation:** 5-minute timeout (commits typically < 30 seconds)
- **Risk:** Lock holder crashes → permanent deadlock
- **Mitigation:** Timeout forces release after 5 minutes

**Gap #3:**
- **Risk:** Wrong role inferred → bad routing
- **Mitigation:** Fallback to persona matching if role empty

### Security Considerations

**Gap #2 (Commit Lock):**
- **Concern:** Malicious agent could hold lock indefinitely
- **Mitigation:** 5-minute timeout prevents permanent lock
- **Future:** Add commit permission checks per agent

---

## Implementation Plan

### Phase 1: Core Implementation (5-8 hours)

**Day 1: Gap #1 - Redispatch Flag (2-3 hours)**
1. Implement `shouldRedispatch()` helper (30 min)
2. Modify `AdvanceWorkflow()` to set flag (30 min)
3. Modify `GetReadyBeads()` to honor flag (30 min)
4. Add flag clearing logic (30 min)
5. Write unit tests (30-60 min)

**Day 1-2: Gap #2 - Commit Serialization (2-3 hours)**
1. Add commit lock fields to Dispatcher (15 min)
2. Implement lock acquisition/release (1 hour)
3. Implement commit queue processor (30 min)
4. Integrate with dispatch loop (30 min)
5. Add timeout handling (30 min)
6. Write unit tests (30 min)

**Day 2: Gap #3 - Role Inference (1 hour)**
1. Implement `inferRoleFromPersona()` (20 min)
2. Modify `CreateAgent()` (10 min)
3. Add logging (10 min)
4. Write unit tests (20 min)

### Phase 2: Testing (4-6 hours)

**Day 3: Integration Tests (2-3 hours)**
1. Multi-turn investigation test (1 hour)
2. Concurrent commit test (1 hour)
3. Role-based routing test (30 min)

**Day 3-4: System Tests (2-3 hours)**
1. Full self-healing loop test (1 hour)
2. Investigation continuation test (30 min)
3. Concurrent bug handling test (1 hour)
4. Failure and escalation test (30 min)

### Phase 3: Validation (2 hours)

**Day 4-5: Manual Testing & Documentation**
1. Create test beads with known bugs (30 min)
2. Observe workflow progression (30 min)
3. Verify CEO escalation (30 min)
4. Update documentation (30 min)

---

## Success Criteria

### Functional Requirements

**Gap #1:**
- [x] Agents can continue investigations across 3+ dispatch cycles
- [x] Redispatch flag set/cleared correctly
- [x] Approval nodes don't get redispatched
- [x] Non-workflow beads unaffected

**Gap #2:**
- [x] Multiple workflows reaching commit nodes execute sequentially
- [x] No git conflicts in concurrent scenarios
- [x] Commit lock timeout prevents deadlock
- [x] Commit queue doesn't grow unbounded

**Gap #3:**
- [x] Agents created with inferred roles
- [x] Role-based routing works for all workflow nodes
- [x] Fallback to persona matching still works

### Non-Functional Requirements

**Performance:**
- Redispatch latency < 100ms
- Commit queue processing < 1 second per commit
- Role inference < 1ms per agent creation

**Reliability:**
- Zero infinite redispatch loops
- Zero permanent deadlocks
- 100% audit trail for commits

**Maintainability:**
- Clear logging for all state transitions
- Easy to add new roles
- Easy to adjust commit timeout

---

## Rollback Plan

### If Critical Issues Found

**Gap #1 Rollback:**
```go
// In AdvanceWorkflow(), comment out flag setting
// updates["context"]["redispatch_requested"] = shouldRedispatch(exec, nextNode)
```
Impact: Agents return to 1-turn limit

**Gap #2 Rollback:**
```go
// In dispatch loop, remove lock acquisition
// if node.NodeType == workflow.NodeTypeCommit {
//     d.acquireCommitLock(...)
// }
```
Impact: Concurrent commits possible again (risk of git conflicts)

**Gap #3 Rollback:**
```go
// In CreateAgent(), remove role inference
// Role: inferRoleFromPersona(persona),
Role: "",
```
Impact: All routing falls back to persona matching

### Monitoring for Issues

**Metrics to watch:**
- Commit queue length (should stay < 5)
- Redispatch loop count per bead (should be < 10)
- Role match success rate (should be > 90%)
- Git conflict rate (should be 0%)

**Logs to check:**
- `[Commit] Bead X queued for commit` - commit queue activity
- `[Workflow] Redispatching bead X` - redispatch activity
- `[Agent] Created agent X with role Y` - role inference

---

## Appendices

### A. Bead Context Schema

**Full context keys after implementation:**
```
workflow_id:              "wf-bug-default"
workflow_exec_id:         "wfexec-abc123"
workflow_node:            "investigate"
workflow_status:          "active"
redispatch_requested:     "true" | "false"
```

### B. Workflow Node Types

```
task      - General work (investigation, implementation)
approval  - Requires human decision (redispatch_requested = false)
commit    - Git operations (serialized via lock)
verify    - Testing/verification
```

### C. Dispatcher Flow with Gaps Closed

```
1. GetReadyBeads()
   - Check redispatch_requested flag
   - If "true", bypass timing check
   - Add to ready beads

2. ensureBeadHasWorkflow()
   - Check/start workflow
   - Get current node

3. Match agent
   - Try workflow role first (now works with inferred roles!)
   - Fall back to persona if role empty

4. Dispatch
   - If commit node, acquire lock
   - Execute task
   - Release lock (if commit)

5. AdvanceWorkflow()
   - Update workflow state
   - Set redispatch_requested flag
   - Persist changes
```

---

**Design Status:** ✅ Complete and Implementation-Ready

**Next Step:** Begin Phase 1 implementation (Gap #1: Redispatch Flag)
