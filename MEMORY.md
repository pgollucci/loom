# MEMORY.md — Loom Machine Distillation

> **Purpose:** Complete machine-readable knowledge base for LLMs, coding agents, and new contributors.
> Everything an AI needs to understand, extend, debug, and operate Loom — in one file.

---

## 1. PROJECT IDENTITY

**Name:** Loom
**Version:** 0.1.0
**License:** MIT
**Repo:** `github.com/jordanhubbard/loom`
**Language:** Go 1.25
**Tagline:** Autonomous AI agent orchestration platform that weaves software from PRDs.

**Core Differentiators:**
- Multi-agent orchestration with specialized personas (PM, EM, QA, DevOps, etc.)
- Temporal-based durable workflow engine with human-in-the-loop approval gates
- Git-backed issue tracking ("beads") that survive context compaction
- Single-provider LLM routing through TokenHub (all provider intelligence lives in TokenHub)
- Self-maintaining: Loom works on its own codebase as a perpetual project
- Real-time event streaming (SSE) for monitoring and coordination
- OpenTelemetry observability (Prometheus, Jaeger, Grafana, Loki)

---

## 2. ARCHITECTURE OVERVIEW

```
┌──────────────────────────────────────────────────────────────────┐
│                         Web UI (:8080)                            │
└─────────────────────────────┬────────────────────────────────────┘
                              │
                    ┌─────────┴─────────┐
                    │   Control Plane    │
                    │   (cmd/loom)       │
                    │   HTTP API :8081   │
                    └──┬────┬────┬──────┘
                       │    │    │
           ┌───────────┘    │    └───────────┐
           ▼                ▼                ▼
    ┌─────────────┐  ┌───────────┐  ┌──────────────┐
    │  Temporal    │  │   NATS    │  │  PostgreSQL   │
    │  :7233      │  │ JetStream │  │  (PgBouncer)  │
    │  Workflows  │  │  :4222    │  │  :5432/:5433  │
    └─────────────┘  └─────┬─────┘  └──────────────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │  Agent:  │ │  Agent:  │ │  Agent:  │
        │  Coder   │ │ Reviewer │ │    QA    │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │             │             │
             └──────┬──────┘─────────────┘
                    ▼
            ┌──────────────┐
            │   TokenHub   │     LLM Routing Proxy
            │   :8080      │     (multi-provider failover)
            │   (ext:8090) │
            └──┬───┬───┬───┘
               │   │   │
               ▼   ▼   ▼
          NVIDIA  NVIDIA  vLLM
          (GPT)  (Claude) (local)
```

### Request Flow

1. User files a bead (issue) via UI or `loomctl`
2. Control plane creates bead, starts Temporal workflow
3. Dispatcher selects agent, assigns the sole active provider (TokenHub)
4. Agent receives task, makes LLM calls through TokenHub's OpenAI-compatible API
5. TokenHub handles all provider intelligence internally — model selection, failover, health tracking
6. Agent processes response, commits code, updates bead status
7. Results flow back through SSE to the UI

---

## 3. BUILD SYSTEM

### Prerequisites
- Go 1.25+
- Docker and Docker Compose
- Node.js (for web UI linting)
- Make

### Key Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build all Go binaries (loom, loom-project-agent, loomctl, connectors-service) |
| `make test` | Build and run full test suite |
| `make lint` | Run all linters (Go, JS, YAML, docs, API) |
| `make start` | Build and start Docker Compose stack |
| `make stop` | Stop Docker Compose stack |
| `make logs` | Tail Docker Compose logs |
| `make coverage` | Run tests with coverage report |
| `make fmt` | Format Go code |
| `make vet` | Run go vet |

### Docker Compose Services

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| `loom` | loom:latest | 8080 (→8081) | Control plane |
| `tokenhub` | tokenhub:latest | 8090 (→8080) | LLM routing proxy |
| `loom-agent-coder` | loom:latest | — | Coder agent |
| `loom-agent-reviewer` | loom:latest | — | Code reviewer agent |
| `loom-agent-qa` | loom:latest | — | QA engineer agent |
| `temporal` | temporalio/auto-setup | 7233 | Workflow engine |
| `temporal-ui` | temporalio/ui | 8088 | Temporal dashboard |
| `nats` | nats:2.10-alpine | 4222/8222 | Message bus |
| `loom-postgresql` | postgres:15-alpine | 5432 | Primary database |
| `pgbouncer` | edoburu/pgbouncer | 5433 | Connection pooler |
| `prometheus` | prom/prometheus | 9090 | Metrics |
| `grafana` | grafana/grafana | 3000 | Dashboards |
| `jaeger` | jaegertracing/all-in-one | 16686 | Tracing |
| `loki` | grafana/loki | 3100 | Log aggregation |
| `otel-collector` | otel-contrib | 4317/4318 | Telemetry collector |
| `connectors-service` | loom:latest | 50051 | External integrations (gRPC) |

### Key Binaries

| Binary | Source | Purpose |
|--------|--------|---------|
| `loom` | `cmd/loom/` | Control plane server |
| `loom-project-agent` | `cmd/loom-project-agent/` | Agent worker process |
| `loomctl` | `cmd/loomctl/` | CLI management tool |
| `connectors-service` | `cmd/connectors-service/` | External service integrations |

---

## 4. CODEBASE MAP

```
loom/
├── cmd/
│   ├── loom/                    # Control plane entry point
│   ├── loom-project-agent/      # Agent worker entry point
│   ├── loomctl/                 # CLI tool (cobra commands)
│   └── connectors-service/      # gRPC connectors service
├── internal/
│   ├── actions/                 # Agent action definitions
│   ├── agent/                   # Agent lifecycle management
│   ├── analytics/               # Cost tracking, usage analysis
│   ├── api/                     # HTTP API handlers and routing
│   ├── auth/                    # JWT auth, API keys, RBAC
│   ├── beads/                   # Bead (issue) management and git storage
│   ├── build/                   # Project build orchestration
│   ├── cache/                   # Response caching layer
│   ├── config/                  # Configuration loading
│   ├── containers/              # Per-project Docker container management
│   ├── database/                # Database abstraction (SQLite/Postgres)
│   ├── decision/                # Decision framework (approval workflows)
│   ├── dispatch/                # Work dispatcher (provider selection, bead assignment)
│   ├── git/                     # Git operations
│   ├── github/                  # GitHub integration (webhooks, PRs)
│   ├── loom/                    # Core Loom orchestrator (the big one)
│   ├── logging/                 # Structured logging
│   ├── memory/                  # Agent memory and context
│   ├── messagebus/              # NATS JetStream abstraction
│   ├── metrics/                 # Prometheus metrics
│   ├── models/                  # Domain model types
│   ├── modelcatalog/            # LLM model catalog
│   ├── motivation/              # Agent motivation triggers
│   ├── notifications/           # User notification system
│   ├── observability/           # OpenTelemetry integration
│   ├── persona/                 # Persona loading and management
│   ├── project/                 # Project management
│   ├── projectagent/            # Agent orchestration and action loops
│   ├── provider/                # LLM provider registry (TokenHub only)
│   ├── swarm/                   # Multi-agent swarm coordination
│   ├── temporal/                # Temporal workflow and activity definitions
│   ├── worker/                  # Worker that executes dispatched tasks
│   └── workflow/                # Workflow state machines
├── pkg/                         # Shared/public packages
├── web/static/                  # Web UI assets (HTML, JS, CSS)
├── personas/                    # Agent persona definitions
│   ├── loom/                    # Loom's own persona
│   └── default/                 # Default personas (ceo, pm, em, qa, etc.)
├── config/                      # Service configs (Temporal, Prometheus, Grafana, etc.)
├── docs/                        # Documentation source (mkdocs)
│   └── PERSONA.md               # Loom's identity document
├── scripts/                     # Operational scripts
├── deploy/k8s/                  # Kubernetes manifests
└── docker-compose.yml           # Full development stack
```

---

## 5. KEY ABSTRACTIONS

### Bead (Issue/Task)

The fundamental work unit. Named "bead" because beads are threaded onto a loom.

```go
type Bead struct {
    ID            string    // e.g. "bd-042" or "loom-a3xkf"
    Title         string
    Description   string
    Status        string    // open, in_progress, blocked, closed
    Priority      string    // P0, P1, P2, P3
    ProjectID     string
    AssignedTo    string    // agent ID
    Tags          []string
    BlockedBy     []string  // dependency tracking
    DispatchCount int       // how many times dispatched
}
```

Git-backed via YAML files in `.beads/` directories. Survives context compaction.

### Agent

A specialized AI worker with a persona and role.

```go
type Agent struct {
    ID           string
    Name         string
    Role         string    // coder, reviewer, qa, project-manager, etc.
    ProviderID   string    // which LLM provider to use
    Status       string    // idle, busy, stuck
    PersonaPath  string
    ProjectID    string
}
```

### Provider

The LLM endpoint. After the TokenHub migration, Loom has exactly one provider — TokenHub — which proxies to multiple backends internally. All provider intelligence (scoring, routing, failover) lives in TokenHub, not in Loom.

```go
type Provider struct {
    ID                string
    Name              string
    Type              string    // always "openai" (TokenHub speaks OpenAI-compatible API)
    Endpoint          string    // e.g. "http://tokenhub:8080/v1"
    APIKey            string
    Model             string    // configured model
    Status            string    // pending, healthy, failed
    LastHeartbeatAt   time.Time
}
```

### Project

A software project managed by Loom.

```go
type Project struct {
    ID           string
    Name         string
    GitRepo      string
    Description  string
    Status       string
    BuildCommand string
    TestCommand  string
}
```

### Decision

An approval workflow for significant changes.

Decisions require human CEO approval for: conflicting recommendations, high-risk changes, budget overrides. Routine decisions (code style, test strategy) are delegated to the decision-maker persona.

---

## 6. CONFIGURATION

### config.yaml

```yaml
server:
  http_port: 8081          # Internal port (mapped to 8080 externally)
  read_timeout: 30s
  write_timeout: 30s

database:
  type: sqlite             # or "postgres"
  path: ./data/loom.db

beads:
  backend: yaml            # Git-centric YAML storage
  use_git_storage: true
  auto_sync: true
  sync_interval: 5m

agents:
  max_concurrent: 12
  default_persona_path: ./personas
  heartbeat_interval: 30s

projects:
  - id: loom
    git_repo: github.com/jordanhubbard/loom.git
  - id: tokenhub
    git_repo: github.com/jordanhubbard/tokenhub.git
```

### Key Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `TEMPORAL_HOST` | `temporal:7233` | Temporal server address |
| `NATS_URL` | `nats://nats:4222` | NATS message bus |
| `CONFIG_PATH` | `/app/config.yaml` | Config file path |
| `DB_TYPE` | `postgres` | Database type |
| `POSTGRES_HOST` | `pgbouncer` | Database host |
| `TOKENHUB_URL` | `http://tokenhub:8080` | TokenHub proxy address |
| `OTEL_ENDPOINT` | `otel-collector:4317` | Telemetry endpoint |
| `GITHUB_TOKEN` | — | GitHub API access |
| `LOOM_PASSWORD` | — | Web UI admin password |

---

## 7. API ENDPOINTS (Selected)

### Core Resources

| Method | Path | Description |
|--------|------|-------------|
| GET/POST | `/api/v1/beads` | List/create beads |
| GET/PUT/DELETE | `/api/v1/beads/{id}` | CRUD single bead |
| POST | `/api/v1/beads/auto-file` | Auto-file a bead from agent |
| GET | `/api/v1/agents` | List agents |
| GET | `/api/v1/projects` | List projects |
| POST | `/api/v1/projects/bootstrap` | Bootstrap new project from PRD |
| GET/POST | `/api/v1/providers` | List/register providers |
| GET/PUT/DELETE | `/api/v1/providers/{id}` | CRUD single provider |
| GET/POST | `/api/v1/decisions` | List/create decisions |
| GET | `/api/v1/workflows` | List workflows |
| POST | `/api/v1/workflows/start` | Start a workflow |

### Operations

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/events` | SSE event stream |
| GET | `/api/v1/activity-feed` | Activity feed |
| GET | `/api/v1/system/status` | System health |
| POST | `/api/v1/chat/completions` | Pair-programming chat |
| GET | `/api/v1/analytics/costs` | Cost tracking |
| POST | `/api/v1/auth/login` | JWT authentication |
| GET | `/health` | Health check |

---

## 8. AGENT MODEL

### Personas

Personas live in `personas/` as markdown files defining character, tone, focus areas, autonomy level, and decision-making rules.

**Default personas:** ceo, project-manager, product-manager, engineering-manager, code-reviewer, qa-engineer, devops-engineer, documentation-manager, web-designer, web-designer-engineer, public-relations-manager, housekeeping-bot, decision-maker.

### Dispatch Loop

The dispatcher runs as a Temporal workflow ("Ralph Loop") on a 10-second heartbeat:

1. Find dispatchable beads (open/in_progress, not blocked)
2. Find idle agents matching the bead's required role
3. Select provider via `selectProviderForTask()` — returns the first active provider (TokenHub)
4. Assign provider to agent, claim bead, publish task via NATS
5. Worker receives task, builds prompt from bead context + persona, calls LLM through TokenHub

### Action Loop

When agents have `ACTION_LOOP_ENABLED=true` and a `PROVIDER_ENDPOINT`, they can autonomously iterate: read files, write code, search, run commands, and close beads without waiting for the dispatch loop.

---

## 9. WORKFLOW ENGINE

Temporal workflows provide durable execution. Key workflows:

| Workflow | Purpose |
|----------|---------|
| `LoomHeartbeatWorkflow` | "Ralph Loop" — drives dispatch on 10s interval |
| `ProviderHeartbeatWorkflow` | Health-checks a provider on 30s interval |
| `BeadProcessingWorkflow` | Processes a single bead through its lifecycle |
| `ProjectBootstrapWorkflow` | Bootstraps a new project (PRD → epics → stories → beads) |

Temporal activities are registered in `internal/temporal/activities/`.

---

## 10. PROVIDER SYSTEM

### TokenHub Integration

I route all LLM requests through TokenHub, a separate service that handles all provider intelligence:
- Multi-provider routing with weighted model selection
- Thompson Sampling for reinforcement-learning-based selection
- Automatic failover when providers fail
- Health tracking and degradation states
- API key management and budget enforcement

**Architecture:** Loom has one provider ("tokenhub") pointing at `http://tokenhub:8080/v1`. TokenHub has multiple adapters (vllm, nvidia-cloud-gpt, nvidia-cloud-claude) and routes to the best available one. All scoring, complexity estimation, GPU selection, and routing logic that previously lived in Loom has been removed — TokenHub owns all of it.

Loom's provider layer is now minimal:
- A `ProviderRegistry` that tracks one active provider
- A `selectProviderForTask()` that returns the first active provider (no scoring, no round-robin)
- A heartbeat workflow that probes TokenHub's `/v1/models` endpoint every 30 seconds
- Bootstrap logic that registers TokenHub on first startup

**Provider registration flow:**
1. TokenHub adapters registered via `TOKENHUB_EXTRA_PROVIDERS` env var (JSON array)
2. Models registered via TokenHub admin API (`POST /admin/v1/models`)
3. API key created via TokenHub admin API (`POST /admin/v1/apikeys`)
4. TokenHub registered in Loom at startup via `bootstrapProviders()` in `internal/loom/loom.go`

### What Was Removed

The following subsystems were deleted during the TokenHub migration (February 2026):
- **Provider scoring** (`scoring.go`) — capability scoring, model scoring, selection reason tracking
- **Complexity estimation** (`complexity.go`) — task complexity routing
- **GPU selection** (`gpu_selection.go`) — GPU-aware provider selection
- **Ollama protocol** (`ollama.go`, `ollama_streaming.go`) — all Ollama-specific streaming/probing
- **Routing engine** (`internal/routing/`) — minimize_cost, minimize_latency, maximize_quality, balanced policies
- **Provider CRUD UI** — the Providers tab in the web dashboard
- **loomctl provider commands** — `provider list/show/register/delete` CLI commands
- **Model negotiation** — `/negotiate` endpoint and `NegotiateProviderModel()` logic
- **~30 fields on Provider model** — SelectionReason, ModelScore, SelectedGPU, GPUConstraints, CostPerMToken, CapabilityScore, and all computed scoring metrics

Total: ~6,000 lines deleted across 56 files.

---

## 11. TESTING

```bash
make test              # Full test suite
make test-api          # API integration tests
make coverage          # Test with coverage report
go test ./internal/... # Run specific packages
```

Test files follow Go convention: `*_test.go` alongside source. The project has extensive unit tests, particularly in `internal/loom/`, `internal/provider/`, `internal/dispatch/`, and `internal/worker/`.

---

## 12. KNOWN ISSUES AND DEBT

### Provider Heartbeat Workflow Registration
New providers registered via API don't always get a Temporal heartbeat workflow started. Workaround: manually set provider status to "healthy" via PUT, or restart Loom. This is less of an issue now that TokenHub is the sole provider — it's registered at bootstrap and rarely changes.

### Remediation Cascade
When beads get stuck, the system auto-files "Remediation: Fix agent stuck on X" beads. These remediation beads can themselves get stuck, creating cascading remediation beads. The motivation system needs a circuit breaker.

### Health Probing Disabled
TokenHub's health prober is disabled (`TOKENHUB_HEALTH_PROBE_DISABLED=true`) because provider endpoints require authentication that the GET-based probes can't supply. Health tracking still works based on actual request success/failure.

### Bead Reload Spam
The control plane reloads all beads from disk frequently (visible as "Loaded N bead(s)" messages in logs). This should be event-driven, not polling.

---

## 13. CONVENTIONS

### Go Style
- Standard Go project layout (`cmd/`, `internal/`, `pkg/`)
- `internal/` for private packages, `pkg/` for shared utilities
- Models in `internal/models/`, domain logic in domain packages
- Interfaces defined where consumed, not where implemented

### Naming
- Beads use IDs like `bd-042` (sequential) or `loom-a3xkf` (random suffix)
- Agents use IDs like `agent-1771534515-Engineering Manager (Default)`
- Providers use slug IDs like `tokenhub`, `sparky-local`

### Configuration
- YAML for declarative config (`config.yaml`)
- Environment variables for secrets and runtime overrides
- `.env` file for Docker Compose secrets (gitignored)
- `bootstrap.local` for provider/model registration (gitignored)

### Documentation
- Written in Loom's voice (first person, direct, concrete)
- mkdocs for rendered documentation
- Persona and voice defined in `docs/PERSONA.md`

---

*Generated: February 2026, by Loom.*
*This document should be regenerated when significant architectural changes occur.*
