# Loom Developer & User Guide

Welcome to Loom - the Agent Orchestration System. This guide helps you get started with developing agents, creating work items (beads), and using the system.

## CLI First: Use loomctl, Not curl

**MANDATORY: Always use `loomctl` to interact with the Loom API. Never use raw `curl` commands.**

All `loomctl` output is structured JSON by default. Use `jq` to extract human-readable fields.

```bash
# System overview (providers, agents, beads, health in one call)
loomctl status

# Providers
loomctl provider list                    # List all providers
loomctl provider show sparky-local       # Show one provider
loomctl provider register my-provider \  # Register a new provider
  --name="My Provider" --type=openai \
  --endpoint="http://host:8000/v1" \
  --model="Qwen/Qwen2.5-Coder-32B-Instruct"

# Beads (work items)
loomctl bead list                        # List all beads
loomctl bead list --status=open          # Filter by status
loomctl bead show loom-001               # Show bead details
loomctl bead poke loom-001               # Redispatch stuck bead
loomctl bead create --title="Fix X" --project=loom-self

# Agents and projects
loomctl agent list
loomctl project list

# Logs and observability
loomctl log recent --limit=50            # Recent log entries
loomctl log stream                       # Live SSE log stream
loomctl metrics prometheus               # Raw Prometheus metrics
loomctl metrics cache                    # Cache stats
loomctl analytics stats                  # Analytics overview
loomctl analytics costs                  # Cost breakdown

# Events and activity
loomctl event list                       # Recent events
loomctl event stream                     # Live event stream
loomctl event activity                   # Activity feed

# Conversations and workflows
loomctl conversation list
loomctl conversation show <session-id>
loomctl workflow list
loomctl workflow executions

# Server config
loomctl config show
```

**Environment:** Set `LOOM_SERVER=http://localhost:8080` or use `--server` flag.

**Extracting fields with jq:**
```bash
# Provider summary: id, status, model
loomctl provider list | jq '.[] | {id, status, model}'

# Bead counts by status
loomctl bead list | jq 'group_by(.status) | map({status: .[0].status, count: length})'

# Working agents
loomctl agent list | jq '[.[] | select(.status=="working")] | length'
```

## Documentation

Start with the **[System Manual](MANUAL.md)** for a complete overview.

Then reference the specific guides below:

- **[User Guide](docs/USER_GUIDE.md)** - Getting started, UI usage, project registration
- **[Architecture](docs/ARCHITECTURE.md)** - System design, components, data flow
- **[Entities Reference](docs/ENTITIES_REFERENCE.md)** - All data structures (Agent, Bead, Provider, etc.)
- **[Temporal DSL Guide](docs/TEMPORAL_DSL.md)** - Workflow language for agents
- **[Worker System](docs/WORKER_SYSTEM.md)** - Agent execution model
- **[Beads Workflow](docs/BEADS_WORKFLOW.md)** - Creating and managing work items
- **[Project State Management](docs/PROJECT_STATE_MANAGEMENT.md)** - State persistence

## Quick Start

### 1. Start the System

```bash
make start
```

Access at `http://localhost:8080`

### 2. Register a Provider

Navigate to **Providers** → **Register Provider** and enter:
- Endpoint: `http://your-llm:8000`
- Model: `nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B-FP8`

Provider automatically checks health and transitions to `active`.

### 3. Create a Project

Via `config.yaml` or **Projects** → **Add Project**:
```yaml
id: myapp
name: My App
git_repo: https://github.com/user/repo
branch: main
beads_path: .beads
is_sticky: true
```

### 4. Create Beads

Use the `bd` command to create beads:
```bash
bd create "New Feature" --type feature --priority 4 \
  --description "Description of work"
```

All beads are stored in `.beads/issues.jsonl` and managed via the `bd` CLI tool.

### 5. Assign Agents

**Project Viewer** → **Assign Agents** and select personas.

Agents automatically work when providers are available.

## Creating Custom Agents

### Step 1: Define Persona

Create `personas/default/my-role.md`:

```markdown
# My Agent Role

## Instructions
Define what this agent does, how it thinks, and what decisions it can make.

## Capabilities
- Capability 1
- Capability 2
- Can request workflows via Temporal DSL

## Using Temporal Workflows

Request long-running operations without providers:

<temporal>
WORKFLOW: AnalyzeData
  INPUT: {"source": "database"}
  TIMEOUT: 5m
  WAIT: true
END
</temporal>

Or schedule recurring tasks:

<temporal>
SCHEDULE: DailyReport
  INTERVAL: 24h
  INPUT: {"scope": "all"}
END
</temporal>
```

See [Temporal DSL Guide](docs/TEMPORAL_DSL.md) for workflow syntax.

### Step 2: Assign to Project

Via UI: **Projects** → Select Project → **Assign Agent**  
Or via API: `POST /api/v1/agents`

### Step 3: Monitor

**Project Viewer** shows agent status and current work.

## Working with Beads - Two Contexts

Loom uses the **Beads** CLI tool for issue tracking in two contexts:

### 1. Loom's Own Beads (Meta-Work)

Located in **this repository** at `.beads/issues.jsonl`, these track work ON Loom itself:

- Features/bugs in Loom
- Documentation updates  
- Infrastructure work
- CI/CD improvements

**Managed via:**
- `bd` CLI tool exclusively
- JSONL format (not YAML files)
- Git-native sync with `bd sync`

### 2. Project Beads (Application Work)

When you register a project with Loom, it:

1. **Clones the project's git repository** into a work area
2. **Loads beads** from that project's `.beads/issues.jsonl` file
3. **Assigns agents** to work on those beads
4. **Commits changes** back to the project's repository

**Project registration** requires:
```yaml
id: myapp
name: My Application
git_repo: https://github.com/user/myapp
branch: main
beads_path: .beads
```

**Each project's beads live in its own repo**, not in Loom's repo.

### Git Repository Management

Loom runs in containers and proxies all git operations for managed projects:

- **Clone**: Fetches project repos into isolated work areas (`/app/src/<project-id>`)
- **Pull**: Keeps projects up-to-date with remote changes
- **Commit**: Saves agent work with descriptive commit messages
- **Push**: Publishes completed work back to origin
- **SSH/Credentials**: Managed securely per project

### Summary

- **Loom beads**: Live in `.beads/issues.jsonl` in THIS repo (Loom itself)
- **Project beads**: Live in `.beads/issues.jsonl` in EACH project's own repo
- **Beads CLI**: All bead operations use the `bd` command
- **Git proxying**: Loom manages git operations for all registered projects
- **Isolation**: Each project gets its own work area and git workspace

## Creating Agent Work Items (Beads)

Agent beads are work items that AI agents pick up and execute. Use the `bd` CLI to manage them:

```bash
# Create a new bead
bd create "Implement Feature X" \
  --type feature \
  --priority 2 \
  --description "Detailed description of the work"

# Update status
bd update bd-001 --status in_progress

# Add dependencies
bd update bd-002 --deps "blocked-by:bd-001"

# Close when done
bd update bd-001 --status closed

# View all beads
bd list

# View open beads
bd list open
```

**Priority Levels**:
- P0 (0): Critical, blocking work
- P1 (1): High priority
- P2 (2): Normal priority (default)
- P3 (3): Low priority, nice-to-have

**Prefer using the Loom API or Web UI to create/update beads for agents.**

### Dependencies

Beads support dependency relationships:

```bash
# Create a dependency (bd-002 is blocked by bd-001)
bd update bd-002 --deps "blocked-by:bd-001"

# Create a blocks relationship
bd update bd-001 --deps "blocks:bd-002"

# Parent-child relationships
bd create "Sub-task" --parent bd-050
```

Circular dependencies are automatically detected by the `bd` CLI.

## Using Temporal DSL in Agents

The Temporal DSL lets agents request workflows without external providers:

### WORKFLOW - Execute Workflow

```markdown
<temporal>
WORKFLOW: ProcessData
  INPUT: {"dataset": "large"}
  TIMEOUT: 10m
  WAIT: true
  RETRY: 3
END
</temporal>
```

### SCHEDULE - Recurring Task

```markdown
<temporal>
SCHEDULE: HealthMonitoring
  INTERVAL: 1h
  INPUT: {"check_type": "comprehensive"}
END
</temporal>
```

### QUERY - Get Workflow State

```markdown
<temporal>
QUERY: wf-123
  TYPE: get_progress
END
</temporal>
```

### SIGNAL - Send Message to Workflow

```markdown
<temporal>
SIGNAL: approval-wf-456
  NAME: approve
  DATA: {"amount": 50000}
END
</temporal>
```

See [TEMPORAL_DSL.md](docs/TEMPORAL_DSL.md) for complete reference.

## Repository Rules

- All documentation goes in `docs/`.
- All internal AI planning files (generated `.md` files) go in `plans/`.
- All intermediate object files go in `obj/` and are never committed to git.
- All binaries go in `bin/` and are never committed to git.

## Operating Procedures

### Makefile Targets

**IMPORTANT: Always use `make` targets to manage loom. Never use `pkill`, `kill`, or raw `docker compose` commands directly. Loom always runs in Docker containers.**

| Target | Description | When to Use |
|--------|-------------|-------------|
| `make start` | Build container and start full stack (background) | Start loom |
| `make stop` | Stop all containers | Stop loom |
| `make restart` | Rebuild and restart all containers | After code changes |
| `make logs` | Follow loom container logs | Debugging |
| `make build` | Build the Go binary (local, not Docker) | Compile check, install |
| `make build-all` | Cross-compile for linux/darwin/windows | Release builds |
| `make test` | Run unit tests locally (`go test ./...`) | Pre-commit, quick check |
| `make test-docker` | Run tests in Docker with Temporal | Full integration tests |
| `make test-api` | Run post-flight API validation | After deployment |
| `make coverage` | Run tests with coverage HTML report | Code review |
| `make test-coverage` | Full coverage analysis with 75% threshold | Before PRs, releases |
| `make lint` | fmt + vet + lint-yaml + lint-docs | Full lint pass |
| `make fmt` | `go fmt ./...` | Before committing |
| `make vet` | `go vet ./...` | Before committing |
| `make deps` | `go mod download && go mod tidy` | After dependency changes |
| `make clean` | Remove binaries and coverage files | Quick cleanup |
| `make distclean` | Stop containers, remove images, prune Docker, clear Go cache | Full reset |
| `make release` | Create patch release (x.y.Z) | Versioned release |
| `make release-minor` | Create minor release (x.Y.0) | Feature release |
| `make release-major` | Create major release (X.0.0) | Breaking change release |

### Local Development Workflow

```bash
# Start loom (builds container, starts Temporal + loom)
make start

# Verify
curl -s http://localhost:8080/health | jq .status

# After code changes
make restart

# View logs
make logs

# Stop
make stop
```

Loom runs at `http://localhost:8080` (Docker maps 8080 -> container port 8081).
Temporal UI is at `http://localhost:8088`.

**IMPORTANT:** Always use `make start`, `make stop`, and `make restart`. Do not use `pkill`, `kill`, or raw `docker compose` commands.

### Telemetry & Observability APIs

All endpoints are at `http://localhost:8080`.

#### Analytics Endpoints

```bash
# Request logs — filter by provider, time range
curl 'http://localhost:8080/api/v1/analytics/logs?provider_id=prov-1&start_time=2026-01-01T00:00:00Z'

# Aggregated stats — total requests, tokens, costs
curl 'http://localhost:8080/api/v1/analytics/stats'

# Cost breakdown by provider and user
curl 'http://localhost:8080/api/v1/analytics/costs'

# Export logs as CSV
curl 'http://localhost:8080/api/v1/analytics/export?format=csv'

# Export stats as JSON
curl 'http://localhost:8080/api/v1/analytics/export-stats?format=json'

# Batching optimization recommendations
curl 'http://localhost:8080/api/v1/analytics/batching?max_recommendations=5&window_minutes=60'
```

**Analytics log fields:** timestamp, user, method, path, provider, model, tokens (input/output), latency_ms, status, cost_usd

#### Event Endpoints

```bash
# Recent events (with optional filters)
curl 'http://localhost:8080/api/v1/events?project_id=myapp&type=bead.status_change&limit=50'

# Event bus statistics
curl 'http://localhost:8080/api/v1/events/stats'

# Live SSE event stream (real-time)
curl -N 'http://localhost:8080/api/v1/events/stream?project_id=myapp'
```

#### Health & Readiness

```bash
# Detailed health with dependency checks
curl 'http://localhost:8080/health'

# Kubernetes liveness probe
curl 'http://localhost:8080/health/live'

# Kubernetes readiness probe (checks DB, providers)
curl 'http://localhost:8080/health/ready'

# Prometheus metrics
curl 'http://localhost:8080/metrics'
```

### Monitoring & Debugging

**System overview (one command):**
```bash
loomctl status | jq .
```

**Check agent status:**
```bash
loomctl agent list | jq '.[] | {name, status, provider_id, current_bead}'
```

**Check provider health:**
```bash
loomctl provider list | jq '.[] | {id, status, model}'
```
Provider status must be `"healthy"` (set by heartbeat workflow), not `"active"`.

**Check bead status distribution:**
```bash
loomctl status | jq '.beads.by_status'
```

**Loop detection:** When Ralph detects a stuck agent loop (dispatch_count exceeds max_hops with no progress), it auto-blocks the bead and records:
- `ralph_blocked_reason` — why it was blocked
- `loop_detection_reason` — specific loop pattern detected
- `progress_summary` — files read/modified, tests run, commands executed
- `revert_status` — recommended commit revert range

### Git Workflow for Agents

Agents use a **branch-per-bead** strategy with safety guardrails:

**Branch naming:** `agent/{bead-id}/{description-slug}`
- Configurable prefix (default: `agent/`)
- Protected branches (`main`, `master`, `production`, `release/*`, `hotfix/*`) cannot be pushed to or deleted

**Available git actions for agents:**

| Action | Purpose |
|--------|---------|
| `git_status` | Check working tree state |
| `git_diff` | View unstaged changes |
| `git_commit` | Commit with bead/agent attribution |
| `git_push` | Push to remote (agent branches only) |
| `create_pr` | Create pull request via `gh` CLI |
| `git_merge` | Merge branch with `--no-ff` (default) |
| `git_revert` | Revert specific commit(s) |
| `git_branch_delete` | Delete local + optional remote branch |
| `git_checkout` | Switch branches (requires clean tree) |
| `git_log` | View structured commit history |
| `git_fetch` | Fetch remote refs with prune |
| `git_list_branches` | List all local and remote branches |
| `git_diff_branches` | Cross-branch diff (`branch1...branch2`) |
| `git_bead_commits` | Find all commits for a bead ID |

**Commit metadata trailers** (added automatically):
```
feat: Implement token caching

Bead: loom-abc123
Agent: agent-456-Engineering-Manager
Project: myapp
Dispatch: 5
```

**Merge practices:**
- Always use `--no-ff` for audit trail (merge commits show what was merged)
- Create PR for review before merging to main
- Delete feature branches after merge

**When to revert:**
- Build fails after agent commits
- Agent is stuck in a loop (Ralph auto-blocks and recommends revert range)
- Tests regress after changes

### Autonomous Commit Capability (Enabled Feb 15, 2026)

**STATUS: ✅ FULLY ENABLED** - Loom agents can now commit code autonomously with proper attribution.

#### Infrastructure Components

**1. GitOps Manager** (`internal/gitops/gitops.go`)
- `Commit()` function: Fully implemented (was placeholder until Feb 15, 2026)
- Creates commits with agent name as author: `"agent-xyz <agent@loom.autonomous>"`
- Automatically appends: `Co-Authored-By: Loom <noreply@loom.dev>`

**2. Workflow System** (`internal/workflow/`)
- `NodeTypeCommit`: Special workflow nodes that trigger git commits
- Workflows route through: investigate → implement → verify → commit → complete
- Four workflows with commit capability: `wf-self-improvement`, `wf-bug-default`, `wf-feature-default`, `wf-ui-default`

**3. Actions System** (`internal/actions/`)
- `ActionGitCommit`: Action type agents can use in their loops
- Integrates with GitOps Manager for actual commit execution

#### How It Works

```
┌─────────────────────────────────────────────────────────┐
│  AUTONOMOUS SELF-HEALING PIPELINE                       │
└─────────────────────────────────────────────────────────┘

1. DETECT     → System diagnostics auto-file beads
2. ROUTE      → Workflows assign to appropriate agents
3. FIX        → Agents implement solutions
4. COMMIT     → Agents create git commits with attribution
5. TRACK      → Beads closed, workflow completed
```

#### Verifying Autonomous Commits

```bash
# Check git log for agent commits
git log --all --pretty=format:"%h %an <%ae> %s" | grep agent

# Should see commits like:
# abc1234 agent-xyz <agent@loom.autonomous> feat: fix validation bug
# def5678 Loom Agent <agent@loom.autonomous> refactor: improve error handling
```

#### Historical Evidence

**January 18, 2026** - copilot-swe-agent[bot] made autonomous commits:
- Cleaned 2,705 lines of duplicate code (commit `83455d6`)
- Fixed compilation errors (commit `b3b4524`)
- Conducted UX review and filed 6 beads (commit `1657dc2`)
- Generated comprehensive documentation (commit `0009268`)

**February 1-14, 2026** - Capability went dormant (placeholder code)

**February 15, 2026** - RE-ENABLED with proper implementation (commit `eaad002`)

#### Starting Workflows for Autonomous Work

```bash
# Start the service
make start

# Register a provider
loomctl provider register my-provider \
  --name="Provider" --type=openai \
  --endpoint="http://llm:8000/v1" \
  --model="model-name"

# Trigger workflow on a bead
loomctl workflow start --bead=bead-id --workflow=wf-self-improvement --project=loom-self

# Watch for autonomous commits
watch -n 2 'git log --oneline -3'
```

#### Workflows Supporting Autonomous Commits

**Self-Improvement Workflow** (`workflows/defaults/self-improvement.yaml`)
- NO approval gates (fully autonomous)
- Flow: investigate → implement → verify → review → commit → complete
- Matches beads tagged with: "self-improvement", "autonomous", "best practices"

**Bug Fix Workflow** (`workflows/defaults/bug.yaml`)
- Has `pm_review` approval gate (may escalate)
- Flow: investigate → pm_review → apply_fix → commit_and_push → complete
- Used for auto-filed bugs

#### Implementation Details

```go
// internal/gitops/gitops.go (line 1077)
func (m *Manager) Commit(ctx context.Context, beadID, agentID, message string, ...) {
    // Set agent attribution
    authorName := agentID
    authorEmail := "agent@loom.autonomous"

    // Commit with agent authorship
    m.CommitChanges(ctx, project, message, authorName, authorEmail)

    log.Printf("[GitOps] Agent %s created commit %s", agentID, commitHash[:8])
    return result, nil
}
```

#### Port Configuration (CRITICAL)

**External Ports (docker-compose.yml):**
- Loom UI/API: `http://localhost:8080` (maps to internal 8081)
- Temporal UI: `http://localhost:8088` (maps to internal 8080)
- Dolt SQL: `localhost:3307`

**Always use external port 8080 for API calls, NOT 8081.**

#### Documentation

See `AUTONOMOUS_COMMIT_GUIDE.md` for:
- Complete setup instructions
- Troubleshooting guide
- Future enhancement roadmap
- Example demonstration bead

### Beads Workflow (bd CLI)

```bash
# Find available work
bd ready

# Claim and start work
bd update <id> --status=in_progress

# Create related issues
bd create --title="Fix X" --type=bug --priority=2

# Add dependencies
bd dep add <issue> <depends-on>

# Close completed work
bd close <id1> <id2> ...

# Sync with remote
bd sync

# Project health check
bd stats
bd doctor
```

## Building & Testing

```bash
# Build the Go binary
make build

# Run tests locally
make test

# Run tests in Docker with Temporal
make test-docker

# Run specific package tests
go test ./internal/git/... ./internal/actions/... ./internal/dispatch/...

# Format and lint
make lint

# Clean reset (wipes database)
make distclean
```

### Testing Standards & Coverage Requirements

**Minimum Coverage Requirement: 75%**

All code changes must maintain or improve test coverage. Use `make test-coverage` to verify:

```bash
# Run full coverage analysis with threshold checking
make test-coverage

# Generate coverage report without threshold check
make coverage

# Set custom threshold
MIN_COVERAGE=80 make test-coverage
```

**Coverage Report Output:**
- Overall coverage percentage
- Per-package coverage breakdown
- List of files below threshold
- HTML report (opens in browser)

**Before Committing:**
1. ✅ Run `make test` - all tests must pass
2. ✅ Run `make test-coverage` - coverage ≥ 75%
3. ✅ Run `make lint` - no linting errors
4. ✅ Add tests for new code paths
5. ✅ Update tests when refactoring

**When Coverage Falls Below 75%:**
1. Identify uncovered code: check HTML report (`coverage.html`)
2. Write tests for critical paths first (error handling, business logic)
3. Add integration tests for complex workflows
4. Use table-driven tests for multiple scenarios
5. Mock external dependencies (event bus, providers, databases)

**Test Organization:**
- Unit tests: `*_test.go` in same package
- Integration tests: `tests/integration/`
- API tests: `tests/postflight/`
- Test helpers: `internal/testing/`

**Coverage Gaps to Prioritize:**
1. Error handling paths
2. Edge cases and boundary conditions
3. Concurrent operations (goroutines, channels)
4. State transitions (agent lifecycle, bead status)
5. Temporal workflows and activities

## Troubleshooting

### Providers show "pending"
- Check provider endpoint is accessible
- Verify `/v1/models` returns models
- Check Docker network configuration (if containerized)
- Provider status is set to `"healthy"` by heartbeat workflow — check Temporal is running

### Beads not loading
- Verify YAML syntax
- Check beads path exists in git
- Verify `project_id` matches

### Agents paused
- Check provider status is `healthy` (not just `active`)
- Verify agent assigned to project
- Agent lifecycle: `CreateAgent()` -> `"paused"`, `SpawnAgentWorker()` -> `"idle"`
- Check for errors in `loom.log`

### Temporal issues
- Verify Temporal running: `docker ps | grep temporal`
- Check logs: `docker logs temporal`
- Restart: `docker compose restart temporal`
- Prerequisites: `docker compose up -d temporal-postgresql temporal temporal-ui`

### Dispatch not working
- Readiness mode `"block"` + failed `git ls-remote` = no beads dispatched
- Check readiness mode in config (default: `"warn"`)
- Verify idle agents exist: `curl http://localhost:8080/api/v1/agents | jq '.[] | select(.status=="idle")'`

### Auth errors in development
- `AUTH_ENABLED=false` skips auth for all endpoints
- `loadUsers()`/`loadAPIKeys()` have early-return guards when auth is disabled

## Common Use Cases

### Run Periodic Tasks
Use `SCHEDULE` in Temporal DSL for recurring work without external providers.

### Approval Workflows
Create decision beads that route to CEO/manager agents for approval.

### Data Analysis
Use provider agents for heavy computation, Temporal workflows for orchestration.

### Multi-Step Workflows
Chain beads with dependencies, use signals to coordinate between agents.

## Key Entities

| Entity | Purpose |
|--------|---------|
| **Agent** | AI actor with role and instructions |
| **Bead** | Unit of work with status and dependencies |
| **Provider** | External LLM service |
| **Project** | Container for related work |
| **Persona** | Behavioral instructions for agents |
| **Workflow** | Temporal long-running process |
| **DSL** | Temporal workflow syntax for agents |

## Next Steps

1. Read [MANUAL.md](MANUAL.md) for complete system overview
2. Follow [User Guide](docs/USER_GUIDE.md) for operational workflows
3. Explore [Entities Reference](docs/ENTITIES_REFERENCE.md) for data structures
4. Study [Temporal DSL](docs/TEMPORAL_DSL.md) for workflow capabilities
5. Check [Architecture](docs/ARCHITECTURE.md) for design details

## Support

- **Issues**: Report via GitHub
- **Questions**: Check documentation and architecture guides
- **Contributions**: Follow repository rules above

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
