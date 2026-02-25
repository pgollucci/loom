# Loom Debug & Instrumentation System

This document defines the debug instrumentation system, event schema, and configuration for Loom's
built-in observability layer.

---

## Table of Contents

1. [Overview](#overview)
2. [Debug Levels](#debug-levels)
3. [Configuration](#configuration)
4. [JSON Event Schema](#json-event-schema)
5. [Event Categories](#event-categories)
6. [Frontend Events (Browser)](#frontend-events-browser)
7. [Backend Events (Server)](#backend-events-server)
8. [How to Read Debug Output](#how-to-read-debug-output)

---

## Overview

Loom's debug system instruments every layer of the stack:

| Layer | Mechanism | Output |
|---|---|---|
| Browser UI | `debug.js` patches `fetch`, `EventSource`, DOM events | `console.debug` as JSON |
| HTTP API | `loggingMiddleware` in `internal/api/server.go` | `stdout` as JSON (visible in `docker logs loom`) |
| Task Executor | `emitDebug()` calls in `internal/taskexecutor/` | `stdout` as JSON |

All events share a **common JSON schema** so they can be correlated across layers using the `ts`
(timestamp) field.

---

## Debug Levels

| Level | Value | Description |
|---|---|---|
| `off` | 0 | No debug output anywhere. Production default. |
| `standard` | 1 | Important lifecycle events only: CRUD at the edges — bead/agent/project create, update, delete, close; all HTTP errors (4xx/5xx). |
| `extreme` | 2 | Everything: every click, every fetch request and response body, every SSE message, every form submission, every navigation event, console.error/warn, all API traffic including health checks. Disregards disk space. |

### What each level captures

**`standard`** — captures:
- Bead events: created, updated, deleted, closed, claimed, blocked, unblocked, annotated, redispatched
- Agent events: created, deleted, started, stopped, paused, resumed
- Project events: created, bootstrapped, updated, deleted, closed
- All HTTP 4xx and 5xx responses
- System events: debug level changes, initialization

**`extreme`** — adds everything from standard, plus:
- Every UI click (button, link, checkbox, select)
- Every form submission (with field values; passwords redacted)
- Every `fetch()` call: URL, method, request body, response body, timing
- Every SSE connection open/close/error
- Every SSE message payload
- Every tab navigation change
- `console.error` and `console.warn` calls
- All HTTP requests including GET, health checks, static assets
- Full request/response bodies (up to 64 KB per body)
- Task executor internals: bead claims, worker transitions

---

## Configuration

### Server-side (config.yaml)

```yaml
# Debug level: "off" | "standard" | "extreme"
debug_level: extreme
```

### Browser-side override

Override the server default from your browser's DevTools console (persists in localStorage):

```javascript
LoomDebug.setLevel('extreme')   // Enable extreme logging
LoomDebug.setLevel('standard')  // Enable standard logging
LoomDebug.setLevel('off')       // Disable all logging
LoomDebug.clearOverride()       // Remove override, revert to server default on reload
```

Priority: `localStorage override > server config (GET /api/v1/config/debug)`.

### API endpoint

`GET /api/v1/config/debug` returns the server-configured debug level:

```json
{"level": "extreme"}
```

---

## JSON Event Schema

Every event — frontend and backend — uses this structure:

```json
{
  "ts":          "2026-02-25T10:30:00.123Z",
  "seq":         42,
  "schema":      "1",
  "debug_level": "extreme",
  "category":    "<category>",
  "action":      "<human-readable description>",
  "source":      "<component that emitted this event>",
  "data":        { "<event-specific fields>" },
  "duration_ms": 123
}
```

### Field definitions

| Field | Type | Required | Description |
|---|---|---|---|
| `ts` | ISO 8601 string | yes | UTC timestamp when the event was emitted |
| `seq` | integer | yes | Monotonically increasing sequence number; resets on page load / process restart |
| `schema` | string | yes | Schema version; currently `"1"` |
| `debug_level` | string | yes | Active debug level at time of emission: `standard` or `extreme` |
| `category` | string | yes | Event category (see [Event Categories](#event-categories)) |
| `action` | string | yes | Human-readable description of what happened |
| `source` | string | yes | Component that emitted the event (e.g. `"fetch"`, `"dom"`, `"api_middleware"`) |
| `data` | object | yes | Category-specific payload (see per-category field tables below) |
| `duration_ms` | integer | no | Elapsed time in milliseconds; present for timed operations (API responses, task execution) |

---

## Event Categories

| Category | Min Level | Description |
|---|---|---|
| `api_request` | extreme | Outgoing HTTP fetch request (browser) or incoming HTTP request (server) |
| `api_response` | extreme | HTTP response received (browser) or sent (server) |
| `api_error` | standard | HTTP 4xx/5xx response or network error |
| `bead_event` | standard | Bead lifecycle: created, updated, deleted, closed, claimed, blocked, redispatched, annotated |
| `agent_event` | standard | Agent lifecycle: created, deleted, started, stopped, paused, resumed |
| `project_event` | standard | Project lifecycle: created, bootstrapped, updated, deleted, closed |
| `sse_connect` | extreme | EventSource connection opened |
| `sse_event` | extreme | SSE message received |
| `sse_error` | standard | SSE connection error |
| `ui_click` | extreme | DOM click on any interactive element |
| `ui_form` | extreme | Form submission |
| `navigation` | extreme | Tab switch, hash change, page visibility change |
| `state_change` | extreme | Application state change |
| `error` | extreme | `console.error` or `console.warn` call captured |
| `system` | standard | Debug system lifecycle: init, level change |
| `task_executor` | standard/extreme | Task executor internals (backend only) |

---

## Frontend Events (Browser)

All frontend events are emitted via `console.debug` with prefix `[LOOM_DEBUG]`. Filter in DevTools
with: `[LOOM_DEBUG]`

### `api_request` (extreme)

Emitted when a `fetch()` call is initiated.

```json
{
  "category": "api_request",
  "action": "POST /api/v1/beads",
  "source": "fetch",
  "data": {
    "method": "POST",
    "url": "/api/v1/beads",
    "body": { "title": "...", "project_id": "..." },
    "content_type": "application/json"
  }
}
```

### `api_response` / `bead_event` (standard for mutations, extreme for reads)

Emitted when a `fetch()` call completes. If the URL matches a standard pattern (bead/agent/project
mutation), the category is set to the semantic category.

```json
{
  "category": "bead_event",
  "action": "bead created (201)",
  "source": "fetch",
  "duration_ms": 45,
  "data": {
    "method": "POST",
    "url": "/api/v1/beads",
    "status": 201,
    "ok": true,
    "response_body": { "id": "bd-042", "title": "..." }
  }
}
```

### `api_error` (standard)

Emitted for any HTTP error (4xx/5xx) or network failure.

```json
{
  "category": "api_error",
  "action": "POST /api/v1/beads → 422",
  "source": "fetch",
  "duration_ms": 12,
  "data": {
    "method": "POST",
    "url": "/api/v1/beads",
    "status": 422,
    "ok": false,
    "error": { "error": "title is required" }
  }
}
```

### `ui_click` (extreme)

```json
{
  "category": "ui_click",
  "action": "click: button#create-bead-btn",
  "source": "dom",
  "data": {
    "tag": "button",
    "id": "create-bead-btn",
    "classes": "btn primary",
    "text": "Create Bead",
    "href": null,
    "type": "button",
    "name": null,
    "value": null,
    "data-attrs": { "data-project-id": "loom" },
    "x": 412,
    "y": 308
  }
}
```

### `ui_form` (extreme)

Passwords are always redacted.

```json
{
  "category": "ui_form",
  "action": "form submit: login-form",
  "source": "dom",
  "data": {
    "form_id": "login-form",
    "action": "/login",
    "method": "POST",
    "fields": {
      "username": "admin",
      "password": "[REDACTED]",
      "remember": false
    }
  }
}
```

### `sse_connect` / `sse_event` / `sse_error` (extreme/standard)

```json
{ "category": "sse_connect", "action": "SSE open: /api/v1/events/stream",
  "source": "eventsource", "data": { "url": "/api/v1/events/stream" } }

{ "category": "sse_event", "action": "SSE message: message",
  "source": "eventsource",
  "data": { "event_type": "message", "last_event_id": "42",
            "data": { "type": "bead_updated", "bead_id": "bd-007" } } }

{ "category": "sse_error", "action": "SSE error: /api/v1/events/stream",
  "source": "eventsource",
  "data": { "url": "/api/v1/events/stream", "readyState": 2 } }
```

### `navigation` (extreme)

```json
{ "category": "navigation", "action": "tab switch: kanban",
  "source": "navigation",
  "data": { "target": "kanban", "label": "📋 Kanban" } }
```

### `system` (standard)

```json
{ "category": "system", "action": "debug system initialized",
  "source": "debug.js",
  "data": { "level": "extreme", "schema_version": "1",
            "url": "http://localhost:8080/", "user_agent": "Mozilla/5.0 ..." } }
```

---

## Backend Events (Server)

All backend events are written to **stdout** as newline-delimited JSON, visible via `docker logs loom`.

Filter backend debug lines:

```bash
docker logs loom 2>&1 | grep '"[LOOM_DEBUG]"' | jq .
```

(Note: backend events include a literal `[LOOM_DEBUG]` prefix in the JSON `action` field for easy grepping.)

### `api_request` (extreme, server-side)

```json
{
  "ts": "2026-02-25T10:30:00.123Z", "seq": 7, "schema": "1",
  "debug_level": "extreme", "category": "api_request",
  "action": "POST /api/v1/beads",
  "source": "api_middleware",
  "data": {
    "method": "POST", "path": "/api/v1/beads", "query": "",
    "remote_addr": "172.17.0.1:54321",
    "user_agent": "Mozilla/5.0 ...",
    "content_type": "application/json",
    "content_length": 128,
    "body_preview": "{\"title\":\"Fix login\",\"project_id\":\"loom\"}"
  }
}
```

### `api_response` (extreme) / `api_error` (standard)

```json
{
  "ts": "2026-02-25T10:30:00.168Z", "seq": 8, "schema": "1",
  "debug_level": "extreme", "category": "api_response",
  "action": "POST /api/v1/beads → 201",
  "source": "api_middleware",
  "duration_ms": 45,
  "data": {
    "method": "POST", "path": "/api/v1/beads", "status": 201,
    "body_preview": "{\"id\":\"bd-042\",\"title\":\"Fix login\",...}",
    "body_bytes": 312
  }
}
```

### `task_executor` (standard/extreme)

```json
{ "category": "task_executor", "action": "bead claimed",
  "source": "taskexecutor",
  "data": { "bead_id": "bd-042", "project_id": "loom",
            "agent_id": "exec-loom-1", "title": "Fix login" } }

{ "category": "task_executor", "action": "worker loop iteration",
  "source": "taskexecutor",
  "data": { "bead_id": "bd-042", "iteration": 3, "llm_calls": 2 } }
```

---

## How to Read Debug Output

### Browser

1. Open DevTools → Console tab
2. Filter by: `[LOOM_DEBUG]`
3. Each line is a JSON string. Right-click → "Store as global variable" → `temp1`, then `JSON.parse(temp1)` to inspect.

Or filter by category: `[LOOM_DEBUG].*bead_event`

### Server

```bash
# All debug events
docker logs loom 2>&1 | grep LOOM_DEBUG

# Pretty-print with jq (requires jq installed)
docker logs loom 2>&1 | grep LOOM_DEBUG | sed 's/.*LOOM_DEBUG //' | jq .

# Only errors
docker logs loom 2>&1 | grep LOOM_DEBUG | grep '"category":"api_error"' | jq .

# Follow live
docker logs -f loom 2>&1 | grep LOOM_DEBUG
```

### Correlating browser and server events

Both layers emit `ts` in ISO 8601 UTC. Sort/correlate by timestamp. The browser `seq` resets on
page load; the server `seq` resets on process restart. Use `ts` as the primary correlation key.
