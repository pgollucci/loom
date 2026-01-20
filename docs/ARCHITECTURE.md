# AgentiCorp Architecture Guide

This document describes the architecture of AgentiCorp, the Agent Orchestration System for managing distributed AI workflows.

## System Overview

AgentiCorp is a comprehensive agent orchestration platform that:
- Coordinates multiple AI agents with different roles (personas)
- Manages work items (beads) through a distributed workflow engine
- Integrates with external LLM providers for agent execution
- Uses Temporal for reliable workflow orchestration
- Persists all state to a SQLite database
- Provides a real-time web UI for monitoring and control

## Core Components

### 1. Agent System

**Purpose**: Manage autonomous AI agents with role-based personas

**Key Files**: 
- `internal/agenticorp/worker_manager.go`
- `internal/models/agent.go`

**Concepts**:
- **Agent**: An autonomous actor with a defined role and persona
- **Persona**: Instructions, guidelines, and behavioral rules for an agent
- **Status**: `idle`, `working`, `paused`, `complete`
- **Role**: Org chart position (CEO, CFO, Engineer, etc.)

**Workflow**:
1. Agents are created from personas on project assignment
2. Agents remain paused until a provider becomes available
3. When providers are healthy, agents resume and can accept work
4. Agents process beads, make decisions, request escalations

**Database**: `agents` table with status, role, and capability tracking

### 2. Bead System (Work Items)

**Purpose**: Define and track discrete units of work in the system

**Key Files**:
- `internal/models/bead.go`
- `docs/BEADS_WORKFLOW.md`

**Concepts**:
- **Bead**: A YAML-defined work item with type, priority, and dependencies
- **Status**: `open`, `in_progress`, `done`, `blocked`
- **Type**: Describes the work (feature, bugfix, test, decision, etc.)
- **Dependencies**: `blocked_by`, `blocks`, `parent`, `children`

**Workflow**:
1. Beads are YAML files in `.beads/beads/` directories
2. Each bead has a unique ID and metadata
3. Beads are loaded at startup and on project changes
4. Agents process ready beads (no blocking dependencies)
5. Completion updates the work graph in real-time

**Database**: `beads` table with status, priority, and dependency tracking

### 3. Provider System

**Purpose**: Interface with external LLM providers (vLLM, Ollama, OpenAI, etc.)

**Key Files**:
- `pkg/provider/provider.go`
- `internal/models/provider.go`
- `internal/agenticorp/agenticorp.go` (provider management)

**Concepts**:
- **Provider**: An external LLM service (local or cloud)
- **Endpoint**: Network address for provider communication
- **Model**: LLM served by the provider (e.g., Nemotron, GPT-4)
- **Status**: `pending`, `active`, `error`

**Workflow**:
1. Providers are registered via UI with endpoint and model info
2. Immediate health check validates provider availability
3. Provider heartbeat workflow monitors health (30s interval)
4. On status change, agents resume or pause accordingly
5. Dispatcher routes work to healthy providers

**Database**: `providers` table with endpoint, status, and heartbeat tracking

### 4. Project System

**Purpose**: Organize work into distinct projects with independent workflows

**Key Files**:
- `internal/models/project.go`
- `internal/agenticorp/project_manager.go`

**Concepts**:
- **Project**: A container for beads, agents, and work
- **Git Integration**: Projects map to git repos with branches
- **Beads Path**: Directory where work items are stored
- **Sticky**: Auto-registered on startup
- **Perpetual**: Never completes (continuous operations)

**Workflow**:
1. Projects are defined in `config.yaml`
2. Beads are loaded from git at startup
3. Agents are assigned to projects
4. Work is tracked and dispatched per project
5. Project state persists across restarts

**Database**: `projects` table with git, status, and configuration

### 5. Temporal Orchestration

**Purpose**: Provide reliable, durable workflow execution with temporal primitives

**Key Files**:
- `internal/temporal/manager.go` (orchestration)
- `internal/temporal/workflows/` (workflow definitions)
- `internal/temporal/activities/` (work implementations)
- `internal/temporal/dsl_parser.go` (DSL parsing)
- `internal/temporal/dsl_executor.go` (DSL execution)

**Concepts**:
- **Workflow**: Long-running business process with retry/resume semantics
- **Activity**: Atomic unit of work within a workflow
- **Signal**: Message sent to running workflows
- **Query**: Request for state from running workflows
- **Schedule**: Recurring workflow execution

**Key Workflows**:
- `AgentiCorpHeartbeatWorkflow`: Master clock (10s interval)
- `DispatcherWorkflow`: Work distribution (5s interval)
- `ProviderHeartbeatWorkflow`: Provider health checks (30s interval)
- `BeadProcessingWorkflow`: Agent work execution
- `DecisionWorkflow`: Escalation and approval workflows

**DSL Support**:
- Agents/providers can embed `<temporal>...</temporal>` blocks
- DSL is parsed, executed, and stripped before sending to providers
- See `docs/TEMPORAL_DSL.md` for complete syntax

### 6. Database Layer

**Purpose**: Persist all system state across restarts

**Key Files**:
- `internal/database/database.go`
- `internal/database/models.go`

**Implementation**: SQLite with embedded database

**Key Tables**:
- `agents`: Agent state, role, status
- `beads`: Work items with dependencies
- `providers`: Provider configuration and health
- `projects`: Project metadata and configuration
- `decisions`: Approval/escalation requests
- `personas`: Agent role definitions

**Features**:
- Automatic schema creation on startup
- Transaction support for consistency
- Backup functionality
- Clean reset with `make distclean`

### 7. Event Bus

**Purpose**: Coordinate system-wide events

**Key Files**:
- `internal/temporal/eventbus/event_bus.go`

**Event Types**:
- `EventTypeProviderRegistered`: New provider added
- `EventTypeProviderDeleted`: Provider removed
- `EventTypeBeadStatusChanged`: Work item status change
- `EventTypeAgentStatusChanged`: Agent status change

### 8. Web UI

**Purpose**: Real-time monitoring and control of the system

**Key Files**:
- `web/static/js/app.js`: UI logic
- `web/static/index.html`: UI structure
- `web/static/css/style.css`: UI styling

**Sections**:
- **Project Viewer**: Browse projects, agents, and beads
- **Kanban Board**: Visualize work by status
- **Providers**: Register and manage LLM providers
- **Agents**: View agent assignments and status
- **Decisions**: Approve/deny escalations
- **Personas**: Define agent roles
- **Projects**: Manage project configuration
- **CEO REPL**: Direct query interface
- **System Status**: Overall health overview

## Data Flow

### Work Distribution Flow

```
Beads Load (Startup)
    ↓
Provider Registration (UI/API)
    ↓
Provider Heartbeat (Temporal, 30s)
    ↓
Provider Health Check (Immediate on registration)
    ↓
Agent Resume (When provider healthy)
    ↓
Dispatcher (Temporal, 5s)
    ↓
Get Ready Beads (No blocking dependencies)
    ↓
Route to Agent (Best agent for bead type)
    ↓
Agent Execution (Via Provider)
    ↓
Bead Complete / Update Dependencies
    ↓
Next Ready Beads Available
```

### Agent Processing Flow

```
Agent Receives Bead
    ↓
Extract Temporal DSL (if any) from persona instructions
    ↓
Execute Temporal DSL (workflows, schedules, queries)
    ↓
Strip DSL from instructions (clean text)
    ↓
Send Clean Instructions + Bead to Provider
    ↓
Provider Executes (may return response with DSL)
    ↓
Parse Provider Response (extract DSL if present)
    ↓
Execute Response DSL (workflows, signals, etc.)
    ↓
Store Clean Response in Database
    ↓
Update Bead Status
```

## Configuration

### config.yaml

Main configuration file with:

```yaml
# API Configuration
api:
  port: 8080
  host: 0.0.0.0

# Database
database:
  path: ./agenticorp.db

# Temporal
temporal:
  host: temporal:7233
  namespace: agenticorp-default
  task_queue: agenticorp-tasks
  enabled: true

# Projects
projects:
  - id: myproject
    name: My Project
    git_repo: https://github.com/user/repo
    branch: main
    beads_path: .beads

# Providers (can also be registered via UI)
providers:
  - id: local-vllm
    name: Local vLLM
    type: local
    endpoint: http://localhost:8000
    model: nvidia/Nemotron
```

## Deployment

### Local Development

```bash
# Start full stack with Docker
docker compose up -d

# Or run locally with external Temporal
make run
```

### Docker Deployment

- AgentiCorp container on port 8080
- Temporal server on port 7233
- Temporal UI on port 8088
- PostgreSQL for Temporal state

## State Persistence

All state is persisted to:
1. **SQLite Database**: Core application state
2. **Temporal Server**: Workflow execution state
3. **Bead YAML Files**: Work item definitions

State survives container restarts. Clean with:

```bash
make distclean  # Wipes database and Temporal state
```

## High-Level Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                      Web UI (Port 8080)                     │
│  Projects | Providers | Agents | Beads | Decisions | REPL  │
└────────────────┬──────────────────────────────────────────┘
                 │
┌────────────────▼──────────────────────────────────────────┐
│              AgentiCorp Core Engine                        │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │   Agent     │  │   Project   │  │   Bead      │     │
│  │   Manager   │  │   Manager   │  │   Manager   │     │
│  └────────┬────┘  └─────────────┘  └─────────────┘     │
│           │                                              │
│  ┌────────▼─────────────────────────────────────────┐  │
│  │        Dispatcher (Temporal Workflow)            │  │
│  │         - Routes work to agents                  │  │
│  │         - Manages dependencies                   │  │
│  └────────┬─────────────────────────────────────────┘  │
│           │                                              │
└───────────┼──────────────────────────────────────────┬─┘
            │                                          │
      ┌─────▼──────────┐                    ┌─────────▼─────┐
      │  Temporal      │                    │   Database    │
      │  (Workflows)   │                    │   (SQLite)    │
      │  (Port 7233)   │                    │               │
      └────────────────┘                    └───────────────┘
            │
      ┌─────▼──────────────────────┐
      │  Provider Heartbeat        │
      │  - Check health (30s)      │
      │  - Update status           │
      └─────────┬──────────────────┘
                │
         ┌──────▼──────────┐
         │  LLM Providers  │
         │  (vLLM, Ollama) │
         └─────────────────┘
```

## Extensions

AgentiCorp can be extended via:

1. **Custom Personas**: Add new agent roles in `personas/`
2. **Custom Beads**: Define work items in project `.beads/` directories
3. **Temporal Workflows**: Add workflows in `internal/temporal/workflows/`
4. **Custom Providers**: Register new LLM endpoints
5. **Temporal DSL**: Use DSL in agent instructions for workflows

See individual documentation files for details.
