# Kubernetes Deployment

Loom provides Kustomize-based Kubernetes manifests for production deployment.

## Directory Structure

```
deploy/k8s/
├── base/                    # Base manifests
│   ├── namespace.yaml       # loom namespace
│   ├── postgresql.yaml      # PostgreSQL StatefulSet
│   ├── pgbouncer.yaml       # Connection pooler
│   ├── nats.yaml            # NATS with JetStream
│   ├── loom.yaml            # Loom control plane
│   ├── connectors-service.yaml  # Connectors microservice
│   └── kustomization.yaml
├── overlays/
│   └── local/               # Local k3d/kind overlay
│       └── kustomization.yaml
└── linkerd/
    ├── authorization-policies.yaml  # mTLS authorization
    └── retry-budgets.yaml           # Service profiles
```

## Deploying

```bash
# Create namespace and apply base
make k8s-apply

# Or manually:
kubectl apply -k deploy/k8s/overlays/local
```

## Linkerd Service Mesh

Loom ships with Linkerd configuration for automatic mTLS, authorization policies, and retry budgets.

```bash
# Install Linkerd
make linkerd-setup

# Verify
make linkerd-check

# Open dashboard
make linkerd-dashboard
```

### Authorization Policies

- Only meshed workloads in the `loom` namespace can access the Loom HTTP API
- Only the `loom` ServiceAccount can call the Connectors Service
- All inter-service traffic is encrypted with mTLS

### Retry Budgets

- Read operations (GET) are retryable
- Mutations (POST/PUT/DELETE) are not retried
- 20% retry budget with 10 retries/second floor
- Long-poll operations have 60s timeouts

## Secrets

```bash
# Create secrets (replace with real values)
kubectl create secret generic loom-secret \
  --namespace loom \
  --from-literal=password=your-password

kubectl create secret generic loom-postgresql-secret \
  --namespace loom \
  --from-literal=username=loom \
  --from-literal=password=your-db-password
```

## Scaling

```bash
kubectl scale deployment loom-agent-coder -n loom --replicas=3
kubectl scale deployment loom-agent-reviewer -n loom --replicas=2
```
