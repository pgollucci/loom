# Microservices Architecture Review & Redesign Plan

## Executive Summary

**Date:** 2026-02-18
**Status:** 🔴 **CRITICAL ARCHITECTURAL GAPS IDENTIFIED**

The current Loom architecture is a **monolithic application with partial containerization**, not a true microservices architecture. This document outlines critical gaps and provides a comprehensive redesign plan.

---

## Current State Analysis

### ✅ What's Working

1. **Partial Containerization**
   - Per-project agent containers (`loom-project-agent`)
   - Docker Compose orchestration
   - Container-based isolation

3. **Internal Message Bus**
   - `AgentMessageBus` for inter-agent communication
   - Pub/sub pattern via in-memory EventBus
   - Message history and filtering

4. **Connector Abstraction**
   - Location-transparent connector interface
   - Health monitoring
   - Configuration management

### ❌ Critical Architectural Gaps

#### 1. **No External Message Bus**

**Current Problem:**
```go
// internal/messaging/bus.go
type AgentMessageBus struct {
    eventBus      *eventbus.EventBus  // In-memory only!
    subscriptions map[string]*Subscription
    history       map[string][]*AgentMessage
}
```

**Issues:**
- ❌ In-memory only (within control plane container)
- ❌ Project containers can't subscribe to messages
- ❌ No persistence - messages lost on restart
- ❌ No cross-container communication
- ❌ Tight coupling between services

**Required:**
- External message broker (RabbitMQ, NATS, or Kafka)
- TCP-based persistent message queue
- All containers connect to same message bus
- Durable queues for reliability

---

#### 2. **No Service-to-Service Communication Protocol**

**Current Problem:**
```go
// internal/executor/shell_executor.go
// Direct execution - no service boundary
cmd := exec.CommandContext(cmdCtx, parts[0], parts[1:]...)
cmd.Dir = workingDir
err = cmd.Run()
```

**Issues:**
- ❌ No gRPC or protobuf for typed contracts
- ❌ No service registry (Consul, etcd)
- ❌ HTTP REST only (not suitable for high-throughput)
- ❌ No circuit breakers or retries
- ❌ No distributed tracing between services

**Required:**
- gRPC with protobuf for service-to-service calls
- Service registry for discovery
- API gateway for routing
- OpenTelemetry for distributed tracing

---

#### 3. **Monolithic Database Access**

**Current Problem:**
```go
// Direct SQLite access from control plane
db, err := sql.Open("sqlite3", dbPath)
```

**Issues:**
- ❌ SQLite doesn't support concurrent writes from multiple containers
- ❌ No database service abstraction
- ❌ Project containers can't access database
- ❌ No connection pooling
- ❌ No transactions across services

**Required:**
- PostgreSQL container as database service
- Database access via gRPC service
- Connection pooling (PgBouncer)
- SAGA pattern for distributed transactions

---

#### 4. **Project Containers Isolated**

**Current Problem:**
```dockerfile
# Dockerfile.project
# Project agent has no message bus connection
# No database access
# No service discovery
```

**Issues:**
- ❌ Project containers can't communicate with control plane
- ❌ No way to send results back
- ❌ No way to receive tasks
- ❌ No shared persistence layer

**Required:**
- Message bus client in every project container
- Standardized request/response protocol
- Service mesh for security and observability

---

## Recommended Architecture

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                       Service Mesh (Istio/Linkerd)              │
│                  (Security, Observability, Traffic Management)   │
└────────────────────────┬────────────────────────────────────────┘
                         │
┌────────────────────────┼────────────────────────────────────────┐
│                 Message Bus (RabbitMQ/NATS)                      │
│           Topics: tasks, results, events, logs, metrics          │
└────┬────────┬────────┬────────┬────────┬────────┬───────────────┘
     │        │        │        │        │        │
┌────▼────┐ ┌▼───────┐ ┌▼─────┐ ┌▼─────┐ ┌▼─────┐ ┌▼──────────┐
│Control  │ │Project │ │Project│ │Temp- │ │Connec│ │API Gateway│
│Plane    │ │Agent 1 │ │Agent 2│ │oral  │ │tors  │ │  (Traefik)│
│Service  │ │        │ │       │ │Worker│ │Service│ │           │
└────┬────┘ └────────┘ └───────┘ └──────┘ └──────┘ └───────────┘
     │                                 │
     │         ┌───────────────────────┴─────────┐
     │         │                                  │
┌────▼─────┐  ┌▼──────────┐  ┌────────────┐  ┌──▼─────────┐
│PostgreSQL│  │Database    │  │Object      │  │Service     │
│Primary   │  │Service     │  │Storage     │  │Registry    │
│          │  │(gRPC)      │  │(MinIO/S3)  │  │(Consul)    │
└──────────┘  └────────────┘  └────────────┘  └────────────┘
```

### Service Breakdown

#### **1. Message Bus Service (RabbitMQ/NATS)**

**Why RabbitMQ:**
- ✅ Persistent queues
- ✅ Topic-based routing
- ✅ Dead letter queues
- ✅ High availability clustering
- ✅ Management UI

**Alternative - NATS:**
- ✅ Lighter weight
- ✅ Better for high-throughput
- ✅ JetStream for persistence
- ✅ Simpler operations

**Topics:**
- `loom.tasks.{project_id}` - Task assignments
- `loom.results.{project_id}` - Execution results
- `loom.events.{type}` - System events
- `loom.logs.{service}` - Structured logs
- `loom.metrics.{service}` - Metrics data

---

#### **2. Database Service (PostgreSQL + gRPC)**

**Service Definition (protobuf):**
```protobuf
service DatabaseService {
  rpc ExecuteQuery(QueryRequest) returns (QueryResponse);
  rpc BeginTransaction(TransactionRequest) returns (Transaction);
  rpc Commit(Transaction) returns (CommitResponse);
  rpc Rollback(Transaction) returns (RollbackResponse);
  rpc GetBead(GetBeadRequest) returns (Bead);
  rpc SaveBead(SaveBeadRequest) returns (SaveBeadResponse);
  rpc ListBeads(ListBeadsRequest) returns (ListBeadsResponse);
}
```

**Benefits:**
- ✅ Type-safe database operations
- ✅ Connection pooling
- ✅ Query caching
- ✅ Access control
- ✅ Audit logging

---

#### **3. Control Plane Service**

**Responsibilities:**
- Dispatch beads to project agents
- Coordinate workflows via the workflow engine
- Monitor health and metrics
- Manage connectors
- Serve web UI

**Communication:**
- Publishes tasks to `loom.tasks.{project_id}`
- Subscribes to `loom.results.*`
- Subscribes to `loom.events.*`
- Calls Database Service via gRPC

---

#### **4. Project Agent Service**

**Responsibilities:**
- Execute assigned beads
- Run tests, builds, lints
- Commit and push code
- Report results

**Communication:**
- Subscribes to `loom.tasks.{project_id}`
- Publishes to `loom.results.{project_id}`
- Calls Database Service via gRPC
- Calls Connectors Service via gRPC

**Container Environment:**
```yaml
services:
  project-agent-loom:
    environment:
      - MESSAGE_BUS_URL=amqp://rabbitmq:5672
      - DATABASE_SERVICE=database-service:50051
      - CONNECTOR_SERVICE=connectors-service:50052
      - PROJECT_ID=loom
      - SUBSCRIBE_TOPIC=loom.tasks.loom
      - PUBLISH_TOPIC=loom.results.loom
```

---

#### **5. Connectors Service**

**Responsibilities:**
- Manage external service connections
- Proxy requests to Prometheus, Grafana, etc.
- Handle authentication
- Health monitoring

**Communication:**
- gRPC service for connector operations
- Publishes health events to message bus
- Independent scalability

---

---

### Message Protocol Design

#### **Task Assignment Message**

```json
{
  "type": "task.assigned",
  "project_id": "loom",
  "bead_id": "bd-001",
  "assigned_to": "agent-123",
  "task_data": {
    "title": "Fix authentication bug",
    "description": "...",
    "context": {...}
  },
  "correlation_id": "uuid",
  "timestamp": "2026-02-18T16:00:00Z"
}
```

#### **Result Message**

```json
{
  "type": "task.completed",
  "project_id": "loom",
  "bead_id": "bd-001",
  "agent_id": "agent-123",
  "result": {
    "status": "success",
    "commits": ["abc123"],
    "output": "...",
    "artifacts": [...]
  },
  "correlation_id": "uuid",
  "timestamp": "2026-02-18T16:30:00Z"
}
```

---

## Implementation Phases

### Phase 1: Message Bus Foundation (Week 1)

**Tasks:**
1. Add RabbitMQ container to docker-compose.yml
2. Create `internal/messagebus` package with RabbitMQ client
3. Define message schemas in `pkg/messages`
4. Implement publish/subscribe wrappers
5. Add message bus health checks

**Deliverable:** All containers can send/receive messages

---

### Phase 2: Database Service (Week 2)

**Tasks:**
1. Replace SQLite with PostgreSQL container
2. Create Database Service with gRPC
3. Define protobuf schemas for database operations
4. Implement service in `internal/dbservice`
5. Migrate control plane to use Database Service
6. Add connection pooling (PgBouncer)

**Deliverable:** Database access via gRPC service

---

### Phase 3: Project Agent Communication (Week 3)

**Tasks:**
1. Add message bus client to project-agent containers
2. Implement task subscription in project agents
3. Implement result publishing from project agents
4. Update dispatcher to publish tasks instead of direct calls
5. Add correlation IDs for request tracking

**Deliverable:** Project agents receive tasks and publish results via message bus

---

### Phase 4: Connectors Service (Week 4) ✅ COMPLETE

**Tasks:**
1. ✅ Extract connector management to separate service (`cmd/connectors-service/`)
2. ✅ Define protobuf for connector operations (`api/proto/connectors/`)
3. ✅ Implement gRPC Connectors Service (`internal/connectors/grpc_server.go`)
4. ✅ Update control plane to call Connectors Service (gRPC client + `ConnectorService` interface)
5. ✅ Connector service in docker-compose and Kubernetes manifests

**Deliverable:** Connectors as independent microservice

---

### Phase 5: Service Mesh & Observability (Week 5) ✅ COMPLETE

**Tasks:**
1. ✅ Add Linkerd service mesh (K8s manifests with authorization policies + retry budgets)
2. ✅ Configure mTLS between services (Linkerd `MeshTLSAuthentication` policies)
3. ✅ Add distributed tracing (Jaeger + OTel Collector + code instrumentation)
4. ✅ Add metrics collection (Prometheus + custom `loom.*` metrics)
5. ✅ Add centralized logging (Loki + Promtail with Docker container log scraping)

**Deliverable:** Full observability and security

---

## Migration Strategy

### Backwards Compatibility

During migration, support both old and new communication methods:

```go
// Hybrid dispatcher
func (d *Dispatcher) DispatchBead(bead *models.Bead) error {
    if d.useLegacyMode {
        // Old direct execution
        return d.legacyDispatch(bead)
    } else {
        // New message-based dispatch
        return d.publishTaskMessage(bead)
    }
}
```

### Feature Flags

```yaml
features:
  use_message_bus: true
  use_database_service: false  # Migrate gradually
  use_connectors_service: false
```

---

## Technology Recommendations

### Message Bus: **NATS with JetStream**

**Why:**
- ✅ Simpler than RabbitMQ
- ✅ Better performance (written in Go)
- ✅ Native request-reply pattern
- ✅ Built-in persistence with JetStream
- ✅ Excellent Go client library
- ✅ Lower resource usage

**Example:**
```go
// internal/messagebus/nats.go
type NatsMessageBus struct {
    conn *nats.Conn
    js   nats.JetStreamContext
}

func (mb *NatsMessageBus) PublishTask(projectID string, task *Task) error {
    subject := fmt.Sprintf("loom.tasks.%s", projectID)
    data, _ := json.Marshal(task)
    _, err := mb.js.Publish(subject, data)
    return err
}

func (mb *NatsMessageBus) SubscribeTasks(projectID string, handler func(*Task)) error {
    subject := fmt.Sprintf("loom.tasks.%s", projectID)
    _, err := mb.js.Subscribe(subject, func(msg *nats.Msg) {
        var task Task
        json.Unmarshal(msg.Data, &task)
        handler(&task)
        msg.Ack()
    }, nats.Durable("agent-"+projectID))
    return err
}
```

### Database: **PostgreSQL 15**

**Why:**
- ✅ ACID compliance
- ✅ JSON support for flexible schemas
- ✅ Connection pooling
- ✅ Replication for HA
- ✅ Proven at scale

### Service Mesh: **Linkerd**

**Why:**
- ✅ Simpler than Istio
- ✅ Written in Rust (fast, secure)
- ✅ Automatic mTLS
- ✅ Lower resource overhead
- ✅ Better for smaller deployments

---

## Success Criteria

### Performance
- [ ] Task dispatch latency < 100ms
- [ ] Message throughput > 10,000 msgs/sec
- [ ] Database query latency < 10ms (p99)
- [ ] Service discovery time < 50ms

### Reliability
- [ ] No message loss (persistent queues)
- [ ] Graceful degradation on service failure
- [ ] Automatic retry with exponential backoff
- [ ] Circuit breakers prevent cascading failures

### Scalability
- [ ] Horizontal scaling of all services
- [ ] Independent scaling (agents, workers, connectors)
- [ ] Database read replicas for scale
- [ ] Message bus clustering

### Observability
- [ ] Distributed traces for all requests
- [ ] Centralized logging with correlation IDs
- [ ] Service-level metrics (RED method)
- [ ] Real-time health dashboards

---

## Risks & Mitigation

### Risk 1: Complexity Increase

**Mitigation:**
- Start with NATS (simpler than RabbitMQ)
- Use managed services where possible
- Comprehensive documentation
- Training for team

### Risk 2: Migration Downtime

**Mitigation:**
- Feature flags for gradual rollout
- Dual-write during migration
- Automated rollback procedures
- Extensive testing in staging

### Risk 3: Performance Regression

**Mitigation:**
- Load testing before/after
- Continuous benchmarking
- Performance budgets
- Rollback plan

---

## Next Steps

1. **Review & Approval** - Team reviews this plan
2. **Proof of Concept** - Build NATS + gRPC prototype
3. **Architecture Decision Record** - Document decisions
4. **Phase 1 Implementation** - Start with message bus
5. **Iterative Migration** - One service at a time

---

## References

- [Microservices Patterns](https://microservices.io/patterns/index.html)
- [NATS Documentation](https://docs.nats.io/)
- [gRPC Best Practices](https://grpc.io/docs/guides/performance/)
- [Service Mesh Comparison](https://servicemesh.es/)
- [12-Factor App](https://12factor.net/)
