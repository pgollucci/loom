# Deployment

Loom runs as a Docker Compose stack for development and Kubernetes for production.

## Docker Compose (Development)

```bash
# Start all services
make start

# View logs
make logs

# Stop
make stop

# Rebuild and restart
make restart
```

The stack includes:

| Service | Port | Description |
|---|---|---|
| loom | 8080 | Control plane API + Web UI |
| nats | 4222 | Message bus |
| loom-postgresql | 5432 | Application database |
| pgbouncer | 5433 | Connection pooler |
| connectors-service | 50051 | Connector management (gRPC) |
| loom-agent-coder | -- | Coder agent |
| loom-agent-reviewer | -- | Reviewer agent |
| loom-agent-qa | -- | QA agent |
| prometheus | 9090 | Metrics |
| grafana | 3000 | Dashboards |
| jaeger | 16686 | Distributed tracing |
| loki | 3100 | Log aggregation |
| otel-collector | 4317 | OpenTelemetry collector |

## Kubernetes (Production)

Kubernetes manifests are in `deploy/k8s/`:

```bash
# Apply base manifests
kubectl apply -k deploy/k8s/overlays/local

# With Linkerd service mesh
linkerd inject deploy/k8s/base/ | kubectl apply -f -

# Apply Linkerd policies
kubectl apply -f deploy/k8s/linkerd/
```

See the [Kubernetes guide](kubernetes.md) for detailed production deployment.
