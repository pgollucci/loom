# Developer Guide

This guide is for developers who want to understand Loom's internals, contribute code, or extend the platform.

## Architecture

Loom is a Go monorepo with a microservices runtime architecture:

```
cmd/
├── loom/                  # Control plane binary
├── loom-project-agent/    # Agent binary
├── connectors-service/    # Connectors gRPC service
└── loomctl/              # CLI tool

internal/
├── api/                   # HTTP API handlers
├── loom/                  # Core application logic
├── dispatch/              # Bead dispatcher
├── projectagent/          # Agent action loop
├── connectors/            # Connector service + gRPC
├── workflow/              # Workflow engine
├── telemetry/             # OpenTelemetry instrumentation
├── messagebus/            # NATS JetStream integration
└── ...

pkg/
├── models/                # Shared data models
├── connectors/            # Connector interfaces
├── config/                # Configuration loading
└── messages/              # Message bus protocols
```

## Key Concepts

- **Control Plane** (`cmd/loom`) -- Serves the API, runs the dispatcher, manages workflows
- **Agents** (`cmd/loom-project-agent`) -- Autonomous workers that subscribe to NATS topics
- **Connectors Service** (`cmd/connectors-service`) -- Manages external service integrations via gRPC
- **Dispatcher** -- Matches ready beads to available agents
- **Workflow Engine** -- Drives multi-step processes as DAGs
- **Message Bus** -- NATS with JetStream for inter-service communication

## Sections

- [Architecture](architecture.md) -- System design and data flow
- [Microservices](microservices.md) -- Service boundaries and communication
- [API Reference](api.md) -- REST API endpoints
- [Agent Actions](agent-actions.md) -- The agent action loop and available actions
- [Autonomous Bug Fixing](auto-bug-fix.md) -- End-to-end autonomous bug fix pipeline
- [Workflow System](workflows.md) -- Workflow engine internals
- [Connectors](connectors.md) -- Connector framework and gRPC service
- [Plugins](plugins.md) -- Plugin development
- [Hot Reload](hot-reload.md) -- Development hot-reload support
- [Contributing](contributing.md) -- How to contribute
