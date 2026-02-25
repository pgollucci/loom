# Security

## Authentication

- JWT tokens for web UI sessions (configurable expiry)
- API keys for programmatic access
- Master password for key encryption at rest

## Network Security

### Docker Compose

All services communicate on a private `loom-network` bridge. Only exposed ports:

| Port | Service | Access |
|---|---|---|
| 8080 | Loom Web UI | Public |
| 3000 | Grafana | Admin |
| 9090 | Prometheus | Admin |
| 16686 | Jaeger | Admin |

### Kubernetes (Linkerd)

- Automatic mTLS between all meshed services
- Authorization policies restrict which services can communicate
- Only the `loom` ServiceAccount can reach the Connectors Service
- SPIFFE-based identity verification

## Secrets Management

- API keys encrypted at rest using AES-256 with the master password
- SSH deploy keys encrypted in the database
- Kubernetes Secrets for production (integrate with external secret managers)

## Git Security

- Per-project SSH deploy keys (Ed25519)
- Repository-scoped access only
- Strict host key checking enabled
- Private keys never logged or exposed via API

## Recommendations

1. Change default credentials immediately
2. Use a strong, unique `LOOM_PASSWORD`
3. Use a strong, random `jwt_secret`
4. Enable HTTPS in production (set `enable_https: true` with valid TLS certificates)
5. Use Kubernetes NetworkPolicies or Linkerd AuthorizationPolicies to restrict access
6. Rotate API keys periodically
