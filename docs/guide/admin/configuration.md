# Configuration

Loom reads its configuration from `config.yaml` (override with `-config /path/to/config.yaml`).

## Server

```yaml
server:
  http_port: 8081
  grpc_port: 9090
  enable_http: true
  enable_https: false
  tls_cert_file: ""
  tls_key_file: ""
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
```

## Database

```yaml
database:
  type: postgres          # sqlite or postgres
  # PostgreSQL settings (production)
  postgres_host: pgbouncer
  postgres_port: 5432
  postgres_user: loom
  postgres_password: loom
  postgres_db: loom
```

## Agents

```yaml
agents:
  max_concurrent: 10
  default_persona_path: ./personas
  heartbeat_interval: 30s
  file_lock_timeout: 10m
```

## Dispatch

```yaml
dispatch:
  max_hops: 20           # Max redispatches before escalation
```

## Environment Variables

| Variable | Description |
|---|---|
| `LOOM_PASSWORD` | Master password for UI login and key encryption |
| `NATS_URL` | NATS server URL |
| `CONNECTORS_SERVICE_ADDR` | Remote connectors service gRPC address |
| `OTEL_ENDPOINT` | OpenTelemetry collector endpoint |
| `DB_TYPE` | Database type (`sqlite` or `postgres`) |
| `POSTGRES_HOST` | PostgreSQL host |
| `POSTGRES_PORT` | PostgreSQL port |
| `POSTGRES_USER` | PostgreSQL username |
| `POSTGRES_PASSWORD` | PostgreSQL password |
| `POSTGRES_DB` | PostgreSQL database name |
