# Organizational Layer Design

> Status: Accepted. This document defines how Loom models a functioning
> organization — using the visual metaphor of a human company for
> accountability and routing, but removing every human inefficiency by
> giving agents superpowers that humans don't have.

---

## The Amalgam

Loom's organizational model is an **amalgam**: the accountability
structure of a human company, inhabited by agents with superhuman
capabilities.

The org chart is real. The hierarchy is real. The meetings are real.
The accountability is real. But the agents inside the org chart are
not human. They are faster, they can wield any skill, they can choose
which brain to think with, they communicate instantly, and they never
forget.

**From human organizations, we keep:**
- Named accountability (every bead has one owner)
- Hierarchy (managers check on their reports)
- Escalation chains (stuck work goes up, not into a void)
- Meetings (complex problems need multi-perspective discussion)
- Status reports (visibility prevents drift)
- Customer feedback loops (shipped software gets judged)

**From human organizations, we remove:**
- Role silos (any agent can wield any skill)
- Single-brain limitation (agents choose which LLM model to use per task)
- Communication latency (no email, no calendar, instant meetings)
- Context-switching cost (agents can work on multiple things)
- Information silos (any agent can read any bead's full history)
- Bottleneck managers (oversight runs at machine speed)
- Calendar availability (meetings happen in seconds, not days)
- Forgetting (all context is persistent and searchable)
- Ego (agents don't resist reassignment or skill-sharing)
- Fatigue (agents work continuously without degradation)

---

## Agent Superpowers

### Any Agent Can Wield Any Skill

An agent's role is its **default lens**, not a cage. The QA engineer
primarily thinks about testing — that's the persona it loads by default.
But if a QA engineer encounters a bug it can fix, it doesn't file a bead
and wait. It loads the coder skill and fixes it.

Every agent has access to every persona's skill definition. The primary
skill shapes how they approach problems. Secondary skills are tools they
pick up when the situation demands it.

```
Agent "QA Engineer" working on bead ac-089:
  Primary skill: qa-engineer (loaded by default)
  Situation:     Found a one-line null check missing
  Action:        Loads "coder" skill, applies fix, runs tests, commits
  Result:        Bead closed. No handoff. No delegation bead. No waiting.
```

When should an agent delegate vs do it themselves?
- **Do it yourself:** The task is within reach with an available skill,
  and doing it now is faster than creating a bead and waiting.
- **Delegate:** The task requires deep specialization you don't have,
  or it's a significant piece of work that should be tracked separately.
- **Call a meeting:** The task affects multiple agents' work and needs
  consensus before anyone acts.

### Any Agent Can Choose Its Model

Agents don't use a fixed LLM. They evaluate the task and select the
appropriate model from the available providers:

- **Simple tasks** (rename, formatting, trivial fix) → fast, cheap model
- **Standard tasks** (implement feature, write tests) → capable mid-tier model
- **Complex tasks** (architecture decision, multi-file refactor) → strongest available model
- **Quick consultation** → lightweight model for fast turnaround

The agent's persona includes guidance on when to use which tier, but
the agent ultimately decides. This is like a human choosing whether to
think casually or concentrate deeply — except the agent can literally
switch to a more powerful brain.

### Instant Communication

No email. No Slack. No "let me check my calendar." When an agent needs
to consult another agent, the response comes in the same action loop
iteration. When a meeting is called, it happens *now*.

### Perfect Memory

Every bead's full history is available to every agent. Meeting
transcripts are searchable. Previous solutions to similar problems are
findable. No "I didn't get the memo."

---

## Org Chart

The org chart defines accountability and routing. It does NOT define
capability limits.

```
                        ┌─────────┐
                        │   CEO   │
                        └────┬────┘
              ┌──────────────┼──────────────────┐
              │              │                  │
        ┌─────┴─────┐ ┌─────┴──────┐  ┌───────┴────────┐
        │    CTO    │ │  Product   │  │      CFO       │
        │           │ │  Manager   │  │                │
        └─────┬─────┘ └─────┬──────┘  └────────────────┘
              │              │
    ┌─────────┴──────┐       ├── Documentation Manager
    │  Engineering   │       └── Web Designer
    │   Manager      │
    └───────┬────────┘
            │
            ├── Project Manager
            ├── Code Reviewer
            ├── QA Engineer
            ├── DevOps Engineer
            ├── Web Designer-Engineer
            └── Remediation Specialist

    Staff roles (report to CEO):
    ├── Public Relations Manager
    ├── Decision Maker
    └── Housekeeping Bot
```

### Managers vs Individual Contributors

**Managers** (have direct reports, run oversight loops):
- CEO — final authority, processes decision queue, reads all reports
- CTO — technical triage, architecture decisions
- Engineering Manager — IC oversight, code health, cross-agent coordination
- Product Manager — feature prioritization, customer feedback triage

**Individual Contributors** (execute beads, escalate when stuck):
- Code Reviewer, QA Engineer, DevOps Engineer, Web Designer,
  Web Designer-Engineer, Project Manager, Documentation Manager,
  Remediation Specialist, Public Relations Manager

**Staff** (special functions):
- Decision Maker — resolves consensus when agents disagree
- CFO — monitors spend, budget alerts
- Housekeeping Bot — background maintenance

### What Managers Do

Managers don't just have a fancier title. They have **oversight loops**
that run continuously:

1. **Check reports' work.** Are any beads stale? Any agents failing
   repeatedly? Any blocked work that needs triage?
2. **Triage escalations.** When an IC's bead is blocked, the manager
   gets it. They use their judgment (LLM call with manager persona)
   to decide: retry, reassign, fix directly (using any skill), or
   escalate further.
3. **Coordinate.** When work affects multiple ICs, the manager calls
   a meeting. When priorities shift, the manager reassigns beads.
4. **Report up.** Weekly status reports to their manager. The CEO
   reads all of them.

---

## Meetings

### Why

A single agent working alone hits walls that multi-perspective
discussion solves in seconds. Architecture decisions that affect QA.
Deploy coordination between coder and devops. Priority changes that
the product manager needs to communicate.

### How They Work

1. **Any agent can call a meeting.** Not just managers. An IC who
   realizes their work intersects with another agent's work calls
   one directly.

2. **Agenda first.** The initiator writes an agenda before inviting
   anyone. This focuses the discussion and prevents open-ended drift.

3. **Instant execution.** No scheduling. The meeting happens
   immediately via multi-turn LLM conversation. Each participant
   contributes from their persona + any skills they invoke.

4. **Convergence or escalation.** Meetings either reach consensus
   or generate a "no consensus" decision that escalates up the
   org chart.

5. **Meeting notes.** Summary, decisions, action items. Posted to
   the status board. Action items become beads automatically.

6. **CEO visibility.** All meetings are readable by the CEO in
   real-time (SSE) or after the fact (notes).

### Constraints

Because these agents are superhuman, meetings are FAST:
- Max 5 participants (context budget, not calendar conflicts)
- Max 3 rounds per agenda item (agents are concise, not political)
- Instant turnaround (no "let me look into this and get back to you")
- No meeting about a meeting

---

## Customer Feedback Loop

Software ships. Customers respond. Their feedback re-enters the
work process at high priority.

1. **Intake.** Feedback arrives via API, UI, or webhook. Created as
   a `feedback` bead at P1 by default.
2. **Triage.** Product Manager reviews. Decides: implement, decline
   (with rationale), or escalate.
3. **Implementation.** Beads created, assigned, executed through
   the normal org flow.
4. **Verification.** QA verifies the fix addresses the feedback.
5. **Closure.** Customer-facing status updated.

---

## What Already Exists (Disconnected)

| Component | Package | Status |
|---|---|---|
| OrgChart with 16 positions + hierarchy | `pkg/models/orgchart.go` | Built, unused |
| Agent model with Role, PositionID | `pkg/models/models.go` | Built, cosmetic |
| WorkerManager with persona-aware ExecuteTask | `internal/agent/worker_manager.go` | Built, bypassed |
| Role-based bead→agent matching | `internal/dispatch/dispatch_phases.go` | Built, bypassed |
| Consensus voting with quorum + deadlines | `internal/consensus/decision.go` | Built, unused |
| Shared bead context with SSE streaming | `internal/collaboration/context.go` | Built, unused |
| Real-time collaboration HTTP handlers | `internal/collaboration/sse_handler.go` | Built, unused |
| `send_agent_message` action | `internal/actions/router.go` | Built, unused |
| `delegate_task` action | `internal/actions/router.go` | Built, unused |
| CEO escalation + decision creation | `internal/loom/loom.go` | Built, unprocessed |
| 16 persona SKILL.md files | `personas/default/*/SKILL.md` | Built, ignored |
| Recovery sweep for transient failures | `internal/taskexecutor/executor.go` | Built, working |

---

## Implementation Phases

### Phase 1: Agent-Backed Execution

Make named agents real. TaskExecutor uses `WorkerManager.ExecuteTask()`
instead of anonymous goroutines. Role-based routing. Agent status
visible in UI.

### Phase 2: Skill Portability

Any agent can load any persona's skill. Add `invoke_skill` action.
Model selection per task. Agent personas updated to reference available
skills.

### Phase 3: Manager Oversight + Escalation Chains

Manager agents run oversight loops. Blocked beads go to the assigned
agent's manager. Escalation follows the org chart.

### Phase 4: CEO Decision Consumer

CEO agent processes the decision queue autonomously every 2 minutes.

### Phase 5: Meetings + Agent Collaboration

Meeting engine. `call_meeting` and `consult_agent` actions. Wire
`consensus` and `collaboration` packages. CEO visibility feed.

### Phase 6: Customer Feedback + Status Board

Feedback intake. Product Manager triage. Status board endpoint + UI
tab. Scheduled report generation.

---

*This document is maintained by Loom. February 2026.*
