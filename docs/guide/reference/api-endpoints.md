# API Endpoints

All endpoints are prefixed with `/api/v1/`.

## Beads

| Method | Path | Description |
|---|---|---|
| GET | `/beads` | List beads (filter by project_id, status, priority, type) |
| POST | `/beads` | Create a bead |
| GET | `/beads/{id}` | Get bead details |
| PUT | `/beads/{id}` | Update a bead |
| DELETE | `/beads/{id}` | Delete a bead |
| GET | `/beads/{id}/workflow` | Get workflow execution for bead |

## Projects

| Method | Path | Description |
|---|---|---|
| GET | `/projects` | List projects |
| POST | `/projects` | Create a project |
| GET | `/projects/{id}` | Get project details |
| PUT | `/projects/{id}` | Update a project |
| DELETE | `/projects/{id}` | Delete a project |
| POST | `/projects/bootstrap` | Bootstrap project from PRD |
| GET | `/projects/{id}/git-key` | Get SSH public key |
| POST | `/projects/{id}/git-pull` | Pull from remote |
| POST | `/projects/{id}/git-push` | Push to remote |
| GET | `/projects/{id}/git-status` | Git status |

## Agents

| Method | Path | Description |
|---|---|---|
| GET | `/agents` | List agents |
| POST | `/agents` | Create an agent |
| GET | `/agents/{id}` | Get agent details |
| PUT | `/agents/{id}` | Update an agent |
| DELETE | `/agents/{id}` | Delete an agent |
| POST | `/agents/{id}/clone` | Clone an agent |

## Providers

| Method | Path | Description |
|---|---|---|
| GET | `/providers` | List providers (typically just TokenHub) |
| POST | `/providers` | Register a provider |
| GET | `/providers/{id}` | Get provider details |
| PUT | `/providers/{id}` | Update a provider |
| DELETE | `/providers/{id}` | Delete a provider |

## Decisions

| Method | Path | Description |
|---|---|---|
| GET | `/decisions` | List pending decisions |
| PUT | `/decisions/{id}` | Resolve a decision |

## Connectors

| Method | Path | Description |
|---|---|---|
| GET | `/connectors` | List connectors |
| POST | `/connectors` | Create a connector |
| GET | `/connectors/{id}` | Get connector details |
| PUT | `/connectors/{id}` | Update a connector |
| DELETE | `/connectors/{id}` | Delete a connector |
| GET | `/connectors/health` | Health check all connectors |
| GET | `/connectors/{id}/health` | Health check single connector |
| POST | `/connectors/{id}/test` | Test connector connectivity |

## Authentication

| Method | Path | Description |
|---|---|---|
| POST | `/auth/login` | Login (returns JWT) |
| POST | `/auth/api-keys` | Create API key |
| GET | `/auth/api-keys` | List API keys |
| DELETE | `/auth/api-keys/{id}` | Revoke API key |

## Analytics

| Method | Path | Description |
|---|---|---|
| GET | `/analytics/change-velocity` | Change velocity metrics |
| GET | `/workflows/analytics` | Workflow analytics |

## Events

| Method | Path | Description |
|---|---|---|
| GET | `/events/stream` | SSE event stream |
| GET | `/activity-feed` | Activity feed |
| GET | `/activity-feed/stream` | SSE activity stream |
| GET | `/notifications` | User notifications |
| POST | `/notifications/{id}/read` | Mark notification read |

## Health

| Method | Path | Description |
|---|---|---|
| GET | `/health/live` | Liveness probe |
| GET | `/health/ready` | Readiness probe |
| GET | `/metrics` | Prometheus metrics |
