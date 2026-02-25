# Configuration Reference

Complete reference for `config.yaml` settings and environment variables.

## config.yaml

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

database:
  type: postgres               # sqlite or postgres
  path: ./loom.db              # SQLite file path
  postgres_host: pgbouncer
  postgres_port: 5432
  postgres_user: loom
  postgres_password: loom
  postgres_db: loom

agents:
  max_concurrent: 10
  default_persona_path: ./personas
  heartbeat_interval: 30s
  file_lock_timeout: 10m

dispatch:
  max_hops: 20

security:
  jwt_secret: ""               # Set a strong random secret
  token_expiry: 24h
  api_key_enabled: true

cache:
  enabled: true
  backend: memory              # memory or redis
  default_ttl: 1h
  max_size: 10000
  max_memory_mb: 256
  cleanup_period: 5m
  redis_url: ""                # Required if backend=redis

hot_reload:
  enabled: false
  watch_dirs: ["./web/static"]
  patterns: ["*.html", "*.js", "*.css"]

web_ui:
  enabled: true
  static_path: ./web/static
  refresh_interval: 5

git:
  project_key_dir: /app/data/projects

readiness:
  mode: block                  # block or skip
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `LOOM_PASSWORD` | (required) | Master password |
| `NATS_URL` | | NATS server URL |
| `CONNECTORS_SERVICE_ADDR` | | Remote connectors gRPC address |
| `OTEL_ENDPOINT` | `otel-collector:4317` | OTel Collector gRPC endpoint |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `otel-collector:4317` | OTel exporter endpoint |
| `DB_TYPE` | config value | Database type |
| `POSTGRES_HOST` | config value | PostgreSQL host |
| `POSTGRES_PORT` | config value | PostgreSQL port |
| `POSTGRES_USER` | config value | PostgreSQL user |
| `POSTGRES_PASSWORD` | config value | PostgreSQL password |
| `POSTGRES_DB` | config value | PostgreSQL database |
| `CONFIG_PATH` | `config.yaml` | Path to config file |

### Agent Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PROJECT_ID` | (required) | Project to work on |
| `CONTROL_PLANE_URL` | (required) | Loom API URL |
| `NATS_URL` | | NATS server URL |
| `AGENT_ROLE` | | Agent role (coder, reviewer, qa) |
| `PROVIDER_ENDPOINT` | | LLM provider endpoint |
| `PROVIDER_MODEL` | | LLM model name |
| `PROVIDER_API_KEY` | | LLM API key |
| `ACTION_LOOP_ENABLED` | `false` | Enable multi-turn action loop |
| `MAX_LOOP_ITERATIONS` | `20` | Max action loop iterations |
| `WORK_DIR` | `/workspace` | Agent workspace directory |
| `OTEL_ENDPOINT` | | OTel Collector endpoint |
