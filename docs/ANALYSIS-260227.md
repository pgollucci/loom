# Loom Analysis — 2026-02-27

> A thorough comparison of Loom's stated purpose against its actual
> implementation, covering every major subsystem. Each finding is
> grounded in specific source files and line-level observations.

---

## 1. Stated Purpose

From `README.md`, `docs/PERSONA.md`, and `MEMORY.md`:

Loom is an autonomous AI agent orchestration platform that weaves software
from PRDs. It coordinates specialized agents — organized as a human company
with an org chart — to execute work items called "beads." Agents are fully
autonomous, choose their own LLM models, can use any skill, communicate
instantly, and are accountable through hierarchy and reporting.

The "amalgam" model (`docs/design/ORGANIZATIONAL_LAYER.md`) promises:
- Named agents with unique personalities, motivations, and skills
- Manager oversight loops and escalation chains
- Virtual meetings for multi-agent consensus
- Customer feedback loops
- Status boards for organizational visibility
- Weekly performance reviews with self-optimization
- Skill portability — any agent uses any persona's skill
- Model selection — agents pick their LLM per task

---

## 2. What Actually Works

### 2.1 TaskExecutor — The Real Engine

**File:** `internal/taskexecutor/executor.go` (1212 lines)

This is the beating heart of Loom and it works. The TaskExecutor:

- Spawns per-project worker goroutines that poll for open beads
- Sorts by priority (P0 first) — `claimNextBead` at line 648
- Claims beads atomically via `beadManager.ClaimBead`
- Runs each bead through `worker.ExecuteTaskWithLoop` — a multi-turn LLM action loop with up to 100 iterations
- Handles errors with dispatch counting, error history, and loop detection
- Recovers blocked beads on a 5-minute sweep (`recoverBlockedBeads` at line 491)
- Escalates irrecoverable beads to CEO after 15 failed dispatches (`isIrrecoverable` at line 600)
- Reclaims zombie beads from dead executor goroutines after 30 minutes
- Serializes filesystem operations per project via `execMu` to prevent git corruption

**Verdict:** Solid. This is production-grade task execution. The concurrency model (multiple pollers, serial execution, semaphore limiting) is sound.

### 2.2 Worker + Action Loop

**File:** `internal/worker/worker.go` (1312 lines)

The `ExecuteTaskWithLoop` function (line 671) is the agent's brain:

- Builds a system prompt from the agent's persona + project context + lessons learned
- Manages conversation context in Postgres for multi-turn memory
- Sends requests to the LLM provider (TokenHub)
- Parses the LLM's response for actions (JSON or text mode)
- Routes actions through the `actions.Router`
- Tracks progress with `ProgressTracker` for stagnation detection
- Handles token limits with progressive truncation
- Supports both frontier models (full 60+ action JSON schema) and small models (14 text actions)

**Verdict:** Works well. The text-mode fallback for small models is pragmatic. The conversation context management with auto-retry on context overflow is solid.

### 2.3 Bead System

**File:** `internal/beads/manager.go` (1352 lines)

Beads are stored as YAML files in `.beads/` directories within git worktrees.
The manager handles:

- CRUD operations with file-level locking (mutex held during writes)
- Priority sorting, status filtering
- Git-backed persistence (beads survive restarts and context compaction)
- Claim atomicity with status + assigned_to update in a single write

**Verdict:** Works as designed. The YAML-in-git approach is unusual but serves Loom's stated goal of surviving context compaction. The file locking is correct.

### 2.4 Provider System

**File:** `internal/provider/registry.go`

After the TokenHub migration, Loom has exactly one provider — TokenHub — which handles all LLM routing, failover, and model selection internally. Loom's provider layer is intentionally minimal: register, heartbeat, list active.

**Verdict:** Clean. The ~6000-line deletion during the TokenHub migration was the right call. No redundant routing logic remains.

### 2.5 Recovery Sweep

**File:** `internal/taskexecutor/executor.go`, lines 480–557

The mark-and-sweep recovery runs every 5 minutes:
- Scans blocked beads
- Checks `loop_detected_reason` for transient patterns (502, rate limit, connection refused, etc.)
- Re-opens transient failures
- Escalates beads with 15+ dispatches and majority non-infra errors to CEO

**Verdict:** Working correctly. The transient pattern list (line 571–593) covers the common failure modes. The irrecoverable heuristic (majority non-infra errors across 15+ dispatches) is reasonable.

---

## 3. What Exists But Is Disconnected

### 3.1 Named Agent Routing — Partially Connected

**File:** `internal/taskexecutor/executor.go`, lines 876–972

The `findAgentForBead` function attempts to match beads to named agents via
`AgentManager.GetIdleAgentsByProject`. The role-matching logic (`roleForBead`,
`roleMatches`) is correct. However:

**Problem:** The `AgentManager` (which is `agent.WorkerManager`) populates agents
at startup based on the org chart positions. But `Position.AgentIDs` in the
default org chart (`DefaultOrgChartPositions` in `pkg/models/orgchart.go`, line 82)
ships with **empty `AgentIDs` arrays**. Agents are registered dynamically by
`WorkerManager.RegisterAgentsForProject` — but this function creates agents with
IDs like `agent-<timestamp>-<role> (Default)`, and these IDs are never written
back into the org chart's `Position.AgentIDs`.

**Result:** `oversightForManager` (line 114 of `oversight.go`) iterates the org
chart positions to find agents assigned to each manager's reports. Since
`AgentIDs` is empty, the oversight loop finds zero agents for every manager.
The loop runs but does nothing. Manager oversight is effectively a no-op.

**Fix needed:** When agents are registered, their IDs must be added to the
corresponding `Position.AgentIDs` in the org chart. Or the oversight loop
must match agents to positions by role name instead of by position agent IDs.

### 3.2 Org Chart — Built, Cosmetic

**File:** `pkg/models/orgchart.go` (169 lines), `internal/orgchart/manager.go`

The org chart model is complete: 16 positions with reporting hierarchy,
required/optional flags, max instances, and methods like `GetPositionByRole`,
`AllRequiredFilled`. The `OrgChartAdapter` in `taskexecutor/orgchart_adapter.go`
wires it into the TaskExecutor.

**Problem:** The org chart is populated in memory but never persisted. Restarting
Loom rebuilds it from `DefaultOrgChartPositions()` with all AgentIDs empty. The
only consumers of the org chart are:

1. `findManagerPositions` in `oversight.go` — works but finds no agents (see 3.1)
2. `findAgentForBead` — does NOT use the org chart; it queries `AgentManager` directly
3. The UI API — returns the org chart for display, but positions show as vacant

**Result:** The org chart exists as a data model but has no active effect on
execution. Bead routing happens through `roleForBead` (tag/type mapping to role
names), not through the org chart hierarchy.

### 3.3 Meetings — Built, Not Wired to LLM

**File:** `internal/meetings/meeting.go` (385 lines)

The meeting engine is structurally complete:
- `CallMeeting` creates a meeting with agenda, runs it synchronously
- Multi-turn discussion with round-robin participant prompting
- Convergence detection (agreement language in last round)
- Summary generation, action item → bead creation
- Status board posting

**Problem:** The `Callback` interface (line 74) requires `GetAgentResponse` to
send prompts to agents. This interface is never implemented by any concrete type.
The `meetings.Manager` is never instantiated in `internal/loom/loom.go`. The
`call_meeting` action in `actions/router.go` declares a `MeetingCaller` interface
but the `Router.Meetings` field is always nil.

**Result:** If an agent tries to call a meeting via the `call_meeting` action,
the router returns "meetings not configured." Meetings exist as code but never
execute.

### 3.4 Status Board — Built, Not Exposed

**File:** `internal/statusboard/board.go` (80 lines)

Simple in-memory board with Post/List. Implements `actions.StatusBoardPoster`.

**Problem:** The board is never instantiated in `internal/loom/loom.go`. The
`Router.Board` field is always nil. The `post_to_board` action returns
"status board not configured." No API endpoint serves status board data to the UI.

**Result:** Dead code. Nothing writes to it, nothing reads from it.

### 3.5 Consensus Voting — Built, Never Invoked

**File:** `internal/consensus/decision.go` (403 lines)

Full consensus system: create decisions, cast votes with confidence scores,
quorum thresholds, timeout monitoring. Well-tested (multiple test files).

**Problem:** The `consensus.DecisionManager` is never instantiated in the main
Loom struct. The `vote` action in the router declares a `VoteCaster` interface
but `Router.Voter` is always nil.

**Result:** Agents can't vote. The consensus system compiles and passes tests
but is unreachable at runtime.

### 3.6 Collaboration Context — Built, Never Used

**File:** `internal/collaboration/context.go` (394 lines)

Shared bead contexts with SSE streaming for real-time multi-agent awareness.
Includes listeners, version tracking, conflict resolution.

**Problem:** Never instantiated. No code path creates a `ContextStore`.

### 3.7 Motivation System — Built, Partially Wired

**Files:** `internal/motivation/*.go` (4683 lines total)

Comprehensive trigger system: calendar-based, event-based, threshold-based,
idle detection. Registry with 30+ built-in motivation triggers. Engine that
evaluates conditions and fires triggers. Milestone tracking. Perpetual project
support.

**Problem:** The `motivationRegistry` and `idleDetector` are created in
`loom.New()` (line 252–253 of `loom.go`), but the `motivationEngine` is created
but never started — there is no `go motivationEngine.Start(ctx)` call anywhere.
The idle detector evaluates but its results are never consumed.

**Result:** 4683 lines of well-tested code that runs in memory but produces no
observable effect.

### 3.8 Agent Consultation — Declared, No Backend

The `consult_agent` action in `actions/router.go` requires an `AgentConsulter`
interface. The `Router.Consulter` field is always nil.

**Result:** Agents cannot synchronously consult other agents.

---

## 4. What the Documentation Promises But Code Cannot Deliver

### 4.1 "Any Agent Can Choose Its Model"

`MEMORY.md` section 8.5, `docs/design/ORGANIZATIONAL_LAYER.md` section 2.2,
all 16 persona SKILL.md files.

**Reality in code:** `executeBead` at line 717 does `prov := providers[0]` — it
takes the first active provider. The worker's `ExecuteTask` uses
`w.provider.Config.Model` — a single model string from the provider config.
There is no model selection logic anywhere in the executor or worker. Agents
always use whatever model TokenHub returns for the configured model string.

Model selection per task complexity would require the agent (or executor) to
evaluate task difficulty and request a specific model from TokenHub. This logic
does not exist.

### 4.2 "Skill Portability — Any Agent Can Use Any Skill"

`MEMORY.md` section 8.3, persona SKILL.md files, `invoke_skill` action.

**Reality in code:** The `invoke_skill` action handler in `router.go` returns
`Result{Status: "executed", Message: "Skill invoked..."}` — it's a no-op that
returns a string. It does not load another persona's SKILL.md, does not change
the system prompt, does not alter the agent's behavior. The agent's system
prompt is built once at the start of `ExecuteTaskWithLoop` (worker.go line 730)
and never changes during execution.

For an agent to actually use another skill, the system would need to:
1. Load the target persona's SKILL.md + MOTIVATION.md + PERSONALITY.md
2. Inject them into the system prompt or as a tool description
3. The agent would then reason from that skill's perspective

None of this happens.

### 4.3 "Managers Run Oversight Loops"

`docs/design/ORGANIZATIONAL_LAYER.md` section on managers.

**Reality in code:** `oversight.go` runs oversight loops every 5 minutes. But
as documented in finding 3.1, the loops find zero agents per manager because
Position.AgentIDs are never populated. The loops execute but always see
`len(reportAgentIDs) == 0`, so they skip all beads.

Additionally, the oversight loop does simple stale-detection (reset beads not
updated in 15 minutes). It does NOT:
- Make LLM calls with the manager persona to triage
- Reassign beads based on agent capability
- Coordinate work across reports
- Generate status reports

### 4.4 "CEO Processes Decision Queue Autonomously"

`docs/design/ORGANIZATIONAL_LAYER.md` phase 4.

**Reality in code:** `ceoDecisionLoop` in `oversight.go` runs every 2 minutes
and does process decision beads. But `makeCEODecision` (line 251) uses hardcoded
heuristics: irrecoverable → cull, escalated → reassign, everything else → approve.
No LLM call. No CEO persona loaded. The "decisions" are string templates.

### 4.5 "Customer Feedback Loop"

`docs/design/ORGANIZATIONAL_LAYER.md` section on customer feedback.

**Reality in code:** No feedback intake endpoint. No `feedback` bead type handling
beyond `roleForBead` mapping `"feedback"` to `"product-manager"`. No Product Manager
triage logic. No customer-facing status updates.

### 4.6 "Weekly Status Reports Posted to Status Board"

AGENTS.md, ORGANIZATIONAL_LAYER.md.

**Reality:** Status board is not instantiated (see 3.4). No report generation
logic exists anywhere. No scheduled posting. No UI tab for the board.

### 4.7 "Performance Reviews with Self-Optimization"

`MEMORY.md` section 8, `internal/taskexecutor/reviews.go`.

**Partially real:** The review system exists and will run. But:
- `triggerSelfOptimization` (line 277) logs the intent but does NOT make an LLM
  call to rewrite MOTIVATION.md or PERSONALITY.md. It sets `p.SelfOptimized = true`
  on the in-memory persona struct — this flag is never persisted or acted upon.
- The `SavePersonaFile` method exists on the persona manager but is never called
  by the review system.
- The review loop starts 24 hours after boot, then weekly. In development, where
  Loom restarts frequently, the first review never fires.

---

## 5. Architectural Gaps

### 5.1 Two Execution Engines, One Active

Loom has two execution paths:

1. **TaskExecutor** (`internal/taskexecutor/`) — Active. Claims beads, runs workers.
2. **Dispatcher + WorkerPool** (`internal/dispatch/` + `internal/agent/worker_manager.go`) — Parked. The dispatcher is instantiated in `loom.New()` (line 394) and its heartbeat loop runs, but it's configured as "parked" and only handles maintenance.

The dispatcher is 1378 lines. The WorkerManager is 1097 lines. The WorkerPool
(in the agent package) is another ~500 lines. That's ~3000 lines of execution
infrastructure that runs in the background but doesn't process beads.

**Risk:** Two overlapping systems that claim beads from the same pool creates
confusion about which one is doing what. If both accidentally become active,
they'll race for beads and potentially double-process them.

### 5.2 Per-Project Serial Execution Bottleneck

`executor.go` line 328–335: `state.execMu.Lock()` serializes all bead execution
within a project. Even though 5 worker goroutines poll concurrently, only one
can execute at a time. For projects with many ready beads, this means 4 workers
are always blocked waiting.

The serialization exists to prevent git workspace corruption (multiple agents
editing files simultaneously). This is the correct trade-off for a single-worktree
setup, but it means the multi-worker architecture provides zero parallelism for
actual execution. Workers only add concurrency for the poll-and-claim phase.

### 5.3 In-Memory State Without Persistence

Several critical systems store state only in memory:

| System | State Lost on Restart |
|--------|----------------------|
| Org chart positions + agent assignments | All positions vacant after restart |
| Meeting history | All meetings lost |
| Status board entries | All posts lost |
| Consensus decisions | All votes lost |
| Performance review history | All grades lost |
| Motivation trigger state | All trigger counts reset |

The bead system (YAML in git) and conversation contexts (Postgres) survive
restarts. Everything else is ephemeral.

### 5.4 PersonaForBead vs RoleForBead Duplication

Two functions do the same thing with different return values:

- `personaForBead` (line 1054): maps tags to persona short names (`"devops"`, `"review"`, `"qa"`)
- `roleForBead` (line 918): maps tags to org chart role names (`"devops-engineer"`, `"code-reviewer"`, `"qa-engineer"`)

`personaForBead` is used when no named agent is found (fallback path).
`roleForBead` is used for named agent matching. They can disagree: `personaForBead`
returns `"devops"` but `roleForBead` returns `"devops-engineer"`. The persona
loader looks for `personas/default/devops/SKILL.md` — which doesn't exist
(the directory is `devops-engineer`). This means the fallback path loads no
persona and falls back to the hardcoded `personas` map at the bottom of
`executor.go`, which has one-line descriptions.

### 5.5 Hardcoded Persona Fallback Map

`executor.go` contains a `var personas = map[string]string{...}` (not shown
in the read but referenced by `personaForBead`) with hardcoded one-line
persona descriptions. These are used when the persona manager can't load a
SKILL.md file. The descriptions are stale and don't reflect the current
three-file persona system.

### 5.6 No API Endpoints for New Subsystems

The following subsystems have no HTTP API exposure:

- Meetings: no endpoint to call, list, or view meetings
- Status board: no endpoint to list entries
- Performance reviews: no endpoint to view grades
- Org chart agent assignments: no endpoint to view filled positions
- Consensus voting: no endpoint to create or view decisions

The UI (`web/static/js/app.js`) has tabs for Beads, Agents, Projects, Providers,
Decisions, Workflows, Analytics, and CEO REPL. There is no tab for Meetings,
Status Board, Performance Reviews, or Org Chart.

---

## 6. Code Quality Observations

### 6.1 loom.go Is Too Large

`internal/loom/loom.go` is 4156 lines — the largest file in the codebase by far.
It contains the Loom struct (38 fields), initialization, project management,
bead management, provider management, agent management, decision management,
git operations, escalation logic, and dozens of interface implementations. This
file is a god object.

### 6.2 Test Coverage Gaps

The TaskExecutor, oversight loops, meetings, and status board have zero test
files (`[no test files]` from `go test`). The bead manager, worker, and
dispatch system have good coverage. The motivation system has excellent coverage
(test files for every source file).

### 6.3 Build Is Clean

`go build ./...` succeeds. `go vet ./...` passes with no warnings. The code
compiles cleanly despite the architectural disconnections.

---

## 7. Summary Table

| Subsystem | Documented? | Implemented? | Connected? | Working? |
|-----------|:-----------:|:------------:|:----------:|:--------:|
| TaskExecutor (bead execution) | Yes | Yes | Yes | **Yes** |
| Worker action loop (LLM calls) | Yes | Yes | Yes | **Yes** |
| Bead system (YAML/git) | Yes | Yes | Yes | **Yes** |
| Provider system (TokenHub) | Yes | Yes | Yes | **Yes** |
| Recovery sweep | Yes | Yes | Yes | **Yes** |
| CEO escalation | Yes | Yes | Yes | **Yes** |
| Named agent routing | Yes | Yes | Partial | **Partial** — works when agents register |
| Org chart model | Yes | Yes | Cosmetic | **No** — positions never populated |
| Manager oversight loops | Yes | Yes | Runs | **No** — finds 0 agents |
| CEO decision loop | Yes | Yes | Runs | **Partial** — heuristic, no LLM |
| Meetings | Yes | Yes | No | **No** — never instantiated |
| Status board | Yes | Yes | No | **No** — never instantiated |
| Consensus voting | Yes | Yes | No | **No** — never instantiated |
| Collaboration context | Yes | Yes | No | **No** — never instantiated |
| Motivation system | Yes | Yes | Partial | **No** — engine never started |
| Skill portability | Yes | Stub | No | **No** — invoke_skill is a no-op |
| Model selection | Yes | No | No | **No** — always uses first provider |
| Customer feedback loop | Yes | No | No | **No** — no intake endpoint |
| Performance reviews | Yes | Yes | Yes | **Partial** — no LLM self-optimization |
| Three-file personas | Yes | Yes | Yes | **Yes** — loader reads all 3 files |
| Agent unique names | Yes | Yes | Yes | **Yes** — display_name in frontmatter |
| Self-optimization writes | Yes | Partial | No | **No** — SavePersonaFile never called |
| Adversarial clones | Yes | Yes | Yes | **Yes** — ClonePersona works |

---

## 8. Priority Recommendations

### P0 — Make What Runs Actually Work

1. **Fix oversight loop agent matching.** Either populate `Position.AgentIDs`
   when agents register, or change `oversightForManager` to match agents by
   role name instead of position ID.

2. **Wire meetings, status board, consensus, and collaboration into loom.go.**
   These packages are complete. They need 50 lines of wiring each in the
   `New()` and `StartTaskExecutor()` functions.

3. **Fix personaForBead / roleForBead mismatch.** Unify into one function
   that returns valid persona directory names (`devops-engineer`, not `devops`).

### P1 — Make Promised Features Real

4. **Implement skill portability.** When `invoke_skill` fires, load the
   target persona's SKILL.md and inject it into the next LLM prompt as
   additional context.

5. **Implement model selection.** Add task-complexity evaluation (based on
   bead priority and tags) and request specific models from TokenHub.

6. **Wire CEO decision loop to LLM.** Replace `makeCEODecision` heuristics
   with an actual LLM call using the CEO persona.

7. **Implement self-optimization.** When triggered, create a bead that asks
   the agent to rewrite its MOTIVATION.md or PERSONALITY.md, using
   `SavePersonaFile` to persist the result.

### P2 — Reduce Debt

8. **Start the motivation engine.** Add `go motivationEngine.Start(ctx)`
   in Initialize or StartTaskExecutor.

9. **Add API endpoints** for meetings, status board, reviews, and org chart.

10. **Split loom.go.** Extract bead management, provider management, and
    agent management into separate files or packages.

11. **Persist org chart state** (agent assignments) to database so it
    survives restarts.

12. **Remove or park the Dispatcher.** If the TaskExecutor is the active
    engine, make the Dispatcher clearly dormant so it doesn't confuse
    future contributors.

---

## 9. Architecture — Overall Assessment

### 9.1 The Two-System Problem

Loom has two execution engines that evolved in parallel:

1. **Dispatcher + WorkerPool** (`internal/dispatch/`, `internal/agent/worker_manager.go`) —
   ~3000 lines. Runs a heartbeat loop in the background. Marked as "parked" via
   configuration but still instantiated. Owns the `WorkerManager` which manages the
   named agent registry, org chart, and agent lifecycle.

2. **TaskExecutor** (`internal/taskexecutor/`) — ~2500 lines. The active engine.
   Claims beads, spawns workers, runs the LLM action loop. Depends on `AgentManager`
   (which is the WorkerManager from the dispatch package) for agent lookup but owns
   its own execution lifecycle.

These two systems share the same bead pool, the same agent registry, and the same
provider list. The TaskExecutor calls into the WorkerManager for agent lookup but
ignores the WorkerPool entirely. The Dispatcher's heartbeat loop runs in the
background consuming resources for no operational purpose.

**Assessment:** The architecture is carrying ~3000 lines of dead weight. The
boundary between "orchestration" and "execution" is unclear — the Dispatcher was
originally designed to be the brain (scheduling, priority, assignment) while workers
were the hands (LLM calls). The TaskExecutor collapsed both roles into one, making
the Dispatcher redundant but leaving its data structures (WorkerManager) as the
canonical agent registry.

### 9.2 God Object: loom.go

`internal/loom/loom.go` at 4156 lines is the largest file in the codebase. It
contains:

- The main `Loom` struct (38 fields)
- Initialization and lifecycle management
- Project management (CRUD, registration, bootstrap)
- Bead management (create, update, claim, list, filtering)
- Provider management (registration, reconciliation, health)
- Agent management (spawn, stop, list, status)
- Decision management (create, claim, decide)
- Git operations (sync, commit, push, status)
- Escalation logic (CEO routing)
- Interface implementations for 15+ interfaces

This file is the bottleneck for every code change. Any modification to beads,
agents, providers, or projects touches this file. There is no separation between
the "core domain" and the "infrastructure wiring."

### 9.3 Package Dependency Graph

The dependency flow is mostly clean:

```
cmd/loom → internal/loom → internal/taskexecutor → internal/worker
                         → internal/beads
                         → internal/agent
                         → internal/provider
                         → internal/api
```

But several packages create circular-ish dependencies through shared interfaces:

- `internal/taskexecutor` depends on `internal/agent.WorkerManager` for agent
  lookup, but `internal/agent` was designed to serve the `internal/dispatch`
  engine — creating an awkward coupling where the TaskExecutor reaches into a
  subsystem designed for its predecessor.

- `internal/actions` defines interfaces (`MeetingCaller`, `AgentConsulter`,
  `StatusBoardPoster`, `VoteCaster`) that are never satisfied because the
  subsystems implementing them (`meetings`, `collaboration`, `consensus`,
  `statusboard`) are never wired into `loom.go`.

### 9.4 Configuration Complexity

`config.yaml` has 400+ fields across providers, agents, dispatch, recovery,
observability, database, auth, web UI, connectors, workflows, and federation.
Many sections configure systems that are partially or fully dormant:

- `dispatch.strategy: "parked"` — entire dispatcher is inactive
- `dispatch.worker_pool` — pool sizes for an unused pool
- `federation` — no federation peers exist
- `connectors` — no active connectors

The config surface area suggests a product much further along in deployment
than the implementation supports.

---

## 10. Back-End Design Assessment

### 10.1 What the Back-End Does Well

**Bead persistence.** YAML files in git worktrees. Survives restarts, context
compaction, and even database loss. The claim-with-lock pattern in
`beads/manager.go` prevents double-processing. This is Loom's most durable
subsystem.

**LLM action loop.** `worker.ExecuteTaskWithLoop` is solid production code:
multi-turn conversations, progressive context truncation, stagnation detection,
text-mode fallback for small models, and proper error propagation. The 100-iteration
ceiling with convergence checking is a reasonable safety net.

**Recovery sweep.** The 5-minute mark-and-sweep in `executor.go` correctly
distinguishes transient failures (network, rate limits) from irrecoverable ones
(repeated non-infra errors). The CEO escalation after 15 failed dispatches is a
sensible policy.

**Provider simplification.** The TokenHub migration removed ~6000 lines of
redundant routing. Loom now delegates model selection, failover, and pricing
to TokenHub. Clean boundary.

### 10.2 What the Back-End Gets Wrong

**Subsystem wiring gap.** Six complete subsystems (`meetings`, `statusboard`,
`consensus`, `collaboration`, `motivation` engine, agent consultation) are
built, tested, and compile — but are never instantiated in `loom.go`. This is
not a design problem; it's an integration problem. Each needs 10–50 lines of
wiring to become active. The fact that this wiring doesn't exist suggests these
packages were built speculatively or in a different development branch and never
connected.

**Serial execution bottleneck.** The `execMu` lock in `executor.go` serializes
all bead execution within a project. Five worker goroutines compete to claim
beads, but only one can execute at a time. For git-worktree safety this is
correct, but the architecture gives the illusion of parallelism (5 workers)
while delivering serial throughput. The documentation promises agents "working
in parallel" which is physically impossible under this locking scheme.

**Oversight loop is a no-op.** `oversight.go` runs every 5 minutes per project
but finds zero agents because `Position.AgentIDs` in the org chart is never
populated (see finding 3.1). This means the entire manager hierarchy — VPE,
engineering directors, QA director — runs oversight loops that process empty
lists. The CPU is busy, but nothing happens.

**CEO decision loop uses heuristics, not LLM.** `makeCEODecision` in
`oversight.go` applies hardcoded rules (irrecoverable → cull, escalated →
reassign, else approve). The CEO persona with its SKILL.md, MOTIVATION.md, and
PERSONALITY.md is never loaded for decision-making. The most strategically
important agent in the org chart is effectively a switch statement.

### 10.3 Back-End Missing Pieces

| Missing Capability | Impact |
|---|---|
| Multi-worktree support | No parallelism within a project |
| Persisted org chart state | Agent assignments lost on restart |
| API endpoints for meetings, reviews, status board | UI cannot display these |
| Feedback bead intake API | Customer feedback loop impossible |
| Agent model selection logic | Agents cannot choose models per task |
| Real skill portability | invoke_skill is a string return |

---

## 11. Agent Design Assessment

### 11.1 Three-File Persona System — Works

The persona loader in `internal/persona/manager.go` correctly reads:
- `SKILL.md` (required) — capabilities, org position, action catalog
- `MOTIVATION.md` (optional) — primary drive, success metrics, frustrations
- `PERSONALITY.md` (optional) — communication style, temperament, humor, values

The `mergePersonaFiles` function concatenates all three with `---` separators
and sets the result as the agent's `Instructions` field, which becomes the LLM
system prompt. All 16 default personas have all three files with thoughtful,
distinct content.

**Verdict:** The persona *definition* system is one of Loom's strongest features.
Agents have real character through their three-file combination.

### 11.2 Named Agent Assignment — Partially Works

Agents are created at startup via `WorkerManager.RegisterAgentsForProject`, which
reads the org chart and creates agents with IDs like `agent-<timestamp>-ceo (Default)`.
The `findAgentForBead` function in `executor.go` queries `AgentManager.GetIdleAgentsByProject`
and matches by role name.

**Problem:** The matching works when agents register, but the org chart positions
never record the assigned agent IDs. This breaks any feature that tries to traverse
the org chart hierarchy to find agents (oversight loops, manager lookups).

### 11.3 Performance Review System — Structurally Sound, Operationally Hollow

`reviews.go` implements weekly grading with priority-weighted scoring, iteration
budgets, assist credits, and A-F grades. The math is reasonable: 60% completion,
20% efficiency, 20% collaboration.

**Problems:**
- Review loop starts 24 hours after boot, then runs weekly. In development
  (frequent restarts), no review ever fires.
- `triggerSelfOptimization` sets a boolean flag but never makes an LLM call
  or writes to persona files.
- `fireAgent` updates status to "fired" and escalates to CEO, but the CEO
  loop uses heuristics — so "firing" a low-performer just creates another
  decision bead that gets auto-approved.
- Grade history is in-memory only. Lost on restart.

### 11.4 Agent Autonomy — Documented but Inconsistent

`MEMORY.md` and `docs/PERSONA.md` state agents are fully autonomous. The
default autonomy level is set to "full." But in practice:

- Agents cannot call meetings (meetings not wired).
- Agents cannot consult each other (consulter not wired).
- Agents cannot vote on decisions (voter not wired).
- Agents cannot post to the status board (board not wired).
- Agents cannot choose their LLM model (hardcoded first provider).
- Agents cannot invoke other agents' skills (no-op action).

Full autonomy means agents can take any action the action router supports.
Six of the twelve defined actions are non-functional. Agents are autonomous
within a cage where half the doors are painted on the wall.

### 11.5 Clone/Adversarial System — Works

`ClonePersona` in the API creates a new persona directory by copying all three
files. Agents can be spawned from cloned personas. The infrastructure for
adversarial evaluation (clone → modify → compare scores) exists.

**Gap:** No automated clone generation. No automated comparison. The adversarial
model requires manual intervention to create clones and manually compare results.

---

## 12. Workflow (Beads) Design Assessment

### 12.1 Bead Lifecycle — Works

Beads flow through `open → in_progress → closed` (or `blocked`). The status
transitions are enforced by the bead manager. Priority sorting (P0 first)
ensures critical work is claimed first. Git-backed YAML persistence means
beads are the most durable data structure in the system.

### 12.2 Missing States in the Kanban

The bead model supports statuses: `open`, `in_progress`, `blocked`, `closed`,
`deferred`, `ready`, `tombstone`, `pinned`. The Kanban board shows only three
columns: Open, In Progress, Closed. Beads in `blocked` status disappear from
the board — they exist in the database but are invisible to users unless they
use the search/filter tools.

This is a significant UX gap. `blocked` is one of the most important operational
states — it means an agent tried and failed. Users need to see blocked beads
prominently, not have them vanish.

### 12.3 Bead Types vs. UI Treatment

Beads have a `type` field (task, decision, epic, feedback). The Kanban filter
dropdown lists task, decision, and epic. The `feedback` type exists in the code
(`roleForBead` maps it to product-manager) but has no UI filter option, no
dedicated view, and no intake mechanism.

Decision beads have their own tab (Decisions), but the rendering shows only:
priority, status, requester, recommendation, and escalation reason. There is
no way to see which agent is working on a decision, when it was created, or
its history. The "Claim" and "Decide" buttons assume human interaction — but
the documentation says decisions are handled autonomously by agents.

### 12.4 Bead-to-Agent Attribution — Weak

When viewing a bead card on the Kanban, the card shows title, priority, tags,
and assigned agent. But the assigned agent is shown as a raw ID
(`agent-1740000000-ceo (Default)`) not the human-readable display name
(`Morgan Webb`). The `formatAgentDisplayName` utility exists and is used in
the Agents tab, but the bead card does not use it for the `assigned_to` field.

### 12.5 No Epic Hierarchy

The bead model supports `parent_bead` for hierarchy and `type: epic` for
grouping. The Kanban board treats epics as flat beads. There is no epic
view, no parent-child tree, no progress rollup. The concept exists in the
schema but has zero UI support.

---

## 13. API Design Assessment

### 13.1 Scope: 115 Handlers, 15 Documented

The API router registers **115 `HandleFunc` calls** in `server.go`. The
OpenAPI specification (`api/openapi.yaml`) documents **15 endpoints**:

| Documented in OpenAPI | Not Documented |
|---|---|
| `/health`, `/personas`, `/personas/{name}`, `/agents`, `/agents/{id}`, `/projects`, `/projects/{id}`, `/beads`, `/beads/{id}`, `/beads/{id}/claim`, `/decisions`, `/decisions/{id}/decide`, `/file-locks`, `/file-locks/{project}/{path}`, `/work-graph` | All analytics, cache, config, auth, commands, conversations, connectors, events, activity-feed, notifications, motivations, workflows, webhooks, export/import, repl, pair, git, debug, logs, streaming, patterns, optimizations (~100 endpoints) |

The OpenAPI spec was written early in development and never updated. It
describes a minimal CRUD API. The actual API is 7x larger. Any client
relying on the spec will miss most of Loom's capabilities.

### 13.2 Endpoint Naming Inconsistencies

- `/api/v1/repl` — singular noun, no context that this is the CEO REPL
- `/api/v1/work` — ambiguous. Handles non-bead prompts.
- `/api/v1/commands/execute` — POST for shell execution, but `/api/v1/commands`
  is GET for command logs. Same path prefix, different resources.
- `/api/v1/analytics/logs` vs `/api/v1/logs/recent` — two log endpoints in
  different namespaces serving similar data.
- `/api/v1/beads/auto-file` — nested under beads but creates a bead from a
  bug report. Better as `/api/v1/bug-reports`.

### 13.3 Missing API Endpoints for Core Subsystems

| Subsystem | Has API? | Has UI? |
|---|---|---|
| Meetings | No | No |
| Status Board | No | No |
| Performance Reviews | No | No |
| Org Chart (live) | GET only | Diagram only |
| Consensus Voting | No | No |
| Collaboration Context | No | No |
| Customer Feedback | No | No |
| Agent Persona Files | No (only full persona JSON) | Partial (old editor) |

The API faithfully serves data for every UI tab that works (beads, agents,
projects, analytics, motivations, conversations, decisions, connectors,
workflows). But the organizational subsystems that represent Loom's core
differentiator — the "amalgam" model — have zero API exposure.

### 13.4 API Authentication

Auth is implemented with JWT tokens, API keys, role-based access control
(admin, user, viewer, service), and permission scoping. The implementation
in `internal/auth/` is thorough. However, the auth middleware has a long
bypass list for health checks and static files, and development mode disables
auth entirely. The UI hardcodes `Bearer` tokens without refresh logic in
some code paths.

### 13.5 SSE Streaming — Well Done

Several endpoints use Server-Sent Events for real-time updates:
- `/api/v1/logs/stream` — log streaming
- `/api/v1/events/stream` — event bus streaming
- `/api/v1/activity-feed/stream` — activity feed
- `/api/v1/notifications/stream` — notifications
- `/api/v1/chat/completions/stream` — LLM streaming
- `/api/v1/pair` — pair programming chat

The SSE implementation is consistent and correct across all endpoints. The
Conversations view uses it for live update tracking. This is one of the best-
implemented patterns in the codebase.

---

## 14. UI Design Assessment

### 14.1 UI Architecture

The UI is a vanilla HTML/CSS/JavaScript single-page application with no build
system, no framework, and no bundler. It consists of:

- `index.html` — 1099 lines, all sections in one file
- `app.js` — 5062 lines, the entire application logic
- `style.css` — the main stylesheet
- `bootstrap.css` — custom component library (not Bootstrap framework)
- 14 additional JS files for specific features

External dependencies (loaded from CDN):
- D3.js v7 for charts and visualizations
- Cytoscape.js for graph diagrams (with dagre and SVG export plugins)

The architecture choice (vanilla JS, no framework) is deliberate and documented.
It keeps the build chain simple and the deployment to a single static file
serve. For an internal tool this is pragmatic.

**However:** `app.js` at 5062 lines is a single-file application with
global state (`state`, `uiState`), global functions (dozens of `function`
declarations at module scope), and no module boundaries. Adding a new tab
requires modifying both `index.html` and `app.js`. There is no component
model, no routing library, and no state management pattern beyond
manual DOM manipulation.

### 14.2 Tabs vs. Mission

The UI has 15 tabs:

| Tab | Maps to Mission? | Assessment |
|---|---|---|
| Home | Yes | Shows project beads and agent assignments. Useful overview. |
| Kanban | Yes | Core bead management. Drag-and-drop works. Missing `blocked` column. |
| Decisions | Partially | Shows decision beads. "Claim" and "Decide" buttons imply human workflow, contradicting autonomous agents. |
| Agents | Yes | Lists agents with status, persona, project. Missing: display name, grade, motivation, personality. |
| Personas | Partially | Lists personas with name and autonomy. No three-file system visibility. |
| Users | Infrastructure | User management. Not part of agent orchestration mission. |
| Projects | Yes | CRUD for projects. Works. |
| Conversations | Yes | Excellent Cytoscape action-flow graphs with live updates. Best tab. |
| CEO | Partially | REPL works. Dashboard is basic (counts only). No meeting summaries, no status board, no decision queue analytics. |
| Analytics | Partially | LLM usage analytics (requests, tokens, cost, latency). Useful but measures infrastructure, not agent productivity. |
| Motivations | Disconnected | Full dashboard for a subsystem that never fires (engine not started). |
| Logs | Infrastructure | System logs with filtering. Useful for debugging. |
| Export/Import | Infrastructure | Database backup/restore. Admin tool. |
| Diagrams | Partially | Four diagram types including org chart. Data is cosmetic (org chart positions are vacant). |
| Dev Tools | Infrastructure | Streaming test and file locks. Developer debugging. |

**Of 15 tabs:**
- 4 serve the core mission (Home, Kanban, Agents, Conversations)
- 3 partially serve it (Decisions, CEO, Diagrams)
- 1 is fully disconnected (Motivations — dashboard for dormant engine)
- 7 are infrastructure/admin (Users, Projects, Analytics, Logs, Export/Import, Personas, Dev Tools)

### 14.3 What the UI Is Missing Entirely

These are features documented in `ORGANIZATIONAL_LAYER.md`, `MEMORY.md`, and
the persona files that have zero UI representation:

| Missing UI Feature | Code Status | Impact |
|---|---|---|
| **Meeting Rooms** — view active meetings, agendas, transcripts, summaries | Code exists, not wired | Agents can't collaborate visibly |
| **Status Board** — organization health, weekly reports, manager summaries | Code exists, not wired | No organizational visibility |
| **Performance Reviews** — agent grades, score history, trend charts | Code exists, partially wired | No accountability visibility |
| **Agent Detail View** — motivation, personality, skill, display name, grade history | Data exists in persona structs | Agent cards are minimal |
| **Org Chart (Live)** — who reports to whom, which positions are filled, agent-to-position mapping | Data model exists | Only cosmetic diagram |
| **Feedback Queue** — customer feedback beads triaged by Product Manager | Not implemented | No customer loop |
| **Blocked Beads Column** — visible Kanban column for blocked work | UI only shows 3 columns | Blocked beads are invisible |
| **Epic View** — parent-child bead hierarchy, progress rollup | Schema supports it | No tree visualization |
| **Agent Communication Log** — messages between agents, consultation history | No inter-agent messaging persisted | No collaboration trail |

### 14.4 UI Sections That Don't Align With Code

**Decisions tab:** The empty-state message reads "When agents escalate work
requiring human input, it appears here." But the code and documentation state
that agents handle decisions autonomously. The Claim/Decide buttons are for
human intervention, but the system is designed to minimize human involvement.
The entire Decisions tab assumes a human-in-the-loop model that contradicts
Loom's documented philosophy.

**Motivations tab:** Fully built out — filters, stats cards, role breakdown,
trigger history table, enable/disable/trigger buttons. This is a professional
dashboard for a subsystem whose engine is never started. The tab will always
show "0 active," "0 recent triggers" because no motivations ever fire. It's
a dashboard for a car engine that isn't connected to the wheels.

**Personas tab:** Shows persona name and autonomy level. Does not show the
three-file system (SKILL + MOTIVATION + PERSONALITY). Does not show agent
display names. Does not show current grade. The persona modal links to
`persona-editor.html` which has a separate editor with fields for name, role,
instructions, and capabilities — none of which map to the three-file system.
The editor was built for an older persona format.

**CEO Command Center:** The REPL works and is useful. The dashboard shows
bead/agent counts. But the CEO tab should be the organizational nerve center —
meeting summaries, decision queue with context, status board feed, performance
review results, escalation queue with history. Instead it's a chat box and
four numbers.

**Agent cards:** Show agent name (raw ID, not display name), persona, project,
current bead, and status. Missing: the unique display name (`Morgan Webb`),
current performance grade, motivation summary, personality type, skill set.
The three-file persona system is invisible in the UI.

### 14.5 Separate HTML Pages — Fragmented Experience

Three features are on separate HTML pages:
- `workflows.html` — Workflow management (separate from main SPA)
- `connectors.html` — Connector management (separate from main SPA)
- `persona-editor.html` — Persona editing (separate from main SPA)

These pages have their own CSS, their own JS, and their own nav bars. They
don't share the tab navigation of `index.html`. The nav bar in
`persona-editor.html` links to `/projects` and `/agents` which are SPA
routes — clicking them loads the raw HTML without the SPA's JS context.

The Workflows tab appears in the Kanban dropdown options (type filter: task,
decision, epic) but the actual workflow UI is on a separate page that isn't
linked from the main navigation. The connectors page is linked only from
the config section. The persona editor is linked from persona cards.

This creates a fragmented experience: the user starts in a cohesive SPA,
then clicks a link and lands on a disconnected page with different styling
and navigation.

### 14.6 UI Code Quality

**Positive:**
- Accessibility: ARIA labels, roles, skip link, live regions (`aria-live="polite"`)
- Responsive layout with CSS grid
- D3 charts are well-integrated with proper cleanup
- Cytoscape diagrams with multiple layout algorithms
- Toast notifications for feedback
- Drag-and-drop Kanban with proper event handling
- Search and filter controls on all list views
- Hot-reload for development

**Negative:**
- 5062 lines in one JS file with 100+ global functions
- Global mutable state (`state`, `uiState`, `motivationsState`, `diagramState`)
- No component model — adding a tab requires editing HTML and JS
- Inline styles scattered throughout HTML (`style="display:flex;..."`)
- Version cache busting via manual `?v=N` query params (21 for app.js)
- CDN-loaded D3 and Cytoscape (offline use impossible)
- No TypeScript — no type safety for API responses or state management
- The observability menu hardcodes `localhost` URLs (breaks in container)

---

## 15. Cross-Section Analysis: Implementation vs. Mission

### 15.1 The Core Contradiction

Loom's mission is autonomous agent orchestration modeled as a human organization
with superhuman capabilities. The codebase delivers:

- **Excellent individual agent execution** (worker action loop)
- **Solid work-item tracking** (beads in git)
- **Professional infrastructure** (auth, analytics, logging, streaming)
- **Rich visualization** (D3 charts, Cytoscape graphs)

But the *organizational* layer — the thing that makes Loom different from
"a for-loop that runs LLM calls on a queue" — is built but disconnected:

| Organizational Feature | Back-End | API | UI |
|---|---|---|---|
| Named agents with personas | Built | Partial | Minimal |
| Org chart hierarchy | Built | GET only | Cosmetic |
| Manager oversight | Built | None | None |
| Meetings | Built | None | None |
| Status board | Built | None | None |
| Consensus voting | Built | None | None |
| Performance reviews | Built | None | None |
| Customer feedback | Not built | None | None |
| Skill portability | Stub | None | None |
| Model selection | Not built | None | None |

The back-end is ahead of the API, and the API is ahead of the UI. The UI
faithfully represents what the API exposes — but the API exposes only the
infrastructure layer, not the organizational layer.

### 15.2 What Should Be Prioritized

To make the UI accurately represent Loom's intended capabilities, work
must flow in this order:

**Phase 1: Wire back-end subsystems** (no UI changes needed)
- Connect meetings, status board, consensus, collaboration in `loom.go`
- Start motivation engine
- Fix org chart agent population
- Fix oversight loop agent matching

**Phase 2: Add API endpoints** (no UI changes needed)
- Meetings: list, create, view transcript, view summary
- Status board: list entries
- Performance reviews: list grades, view history
- Org chart: live positions with agents
- Agent detail: include all three persona files + grade + display name

**Phase 3: Update existing UI**
- Add `blocked` column to Kanban
- Show display names instead of raw IDs on agent cards
- Add grade, motivation, personality to agent cards
- Update persona cards to show three-file system
- Fix Decisions tab to reflect autonomous handling
- Expand CEO dashboard with organizational data

**Phase 4: Add new UI sections**
- Meeting Rooms tab (or CEO sub-section)
- Status Board tab
- Performance Reviews tab
- Agent Detail modal (click agent → see full persona)
- Epic hierarchy view
- Feedback queue

---

## 16. Summary: Alignment Score by Layer

| Layer | Implementation Maturity | Alignment with Mission | Priority Gap |
|---|---|---|---|
| **Architecture** | 70% — solid core, two dead engines, god object | 50% — organizational layer disconnected | High |
| **Back-End** | 80% — most subsystems built and tested | 55% — 6 subsystems built but not wired | High |
| **Agent Design** | 85% — three-file personas, review system, cloning | 60% — half the actions are non-functional | Medium |
| **Workflow (Beads)** | 75% — CRUD solid, recovery works | 65% — blocked beads invisible, no epic view | Medium |
| **API** | 60% — 115 handlers, 15 documented | 45% — organizational subsystems have no endpoints | High |
| **UI** | 65% — 15 tabs, good infra views | 35% — no organizational visibility, 1 tab fully disconnected | **Critical** |

The UI is the widest gap. Users see a professional-looking tool management
dashboard that shows none of what makes Loom unique. The organizational
layer — the reason Loom exists — is invisible at every level of the stack.

---

*Analysis updated 2026-02-27 with comprehensive Architecture, Back-End,
Agent, Workflow, API, and UI design assessments. Every finding references
specific files and line-level observations.*
