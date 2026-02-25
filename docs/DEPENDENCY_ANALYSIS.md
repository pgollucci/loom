# Circular Dependency Analysis for Loom

## Summary

After analyzing the import statements across all major internal packages, **no circular dependencies were found** in the Loom codebase.

## Dependency Graph (Key Packages)

### Top-Level Package: internal/loom
Imports 35+ internal packages but is not imported by any internal package (it's the main orchestrator).

### Core Dependencies (Leaf Nodes - No Internal Imports)
- `pkg/models` - Data models only
- `pkg/config` - Configuration (imports only pkg/secrets)
- `pkg/secrets` - Standard library only
- `internal/memory` - imports only pkg/models
- `internal/workflow` - imports only internal/telemetry
- `internal/swarm` - imports internal/messagebus, pkg/messages
- `internal/containers` - imports pkg/messages, pkg/models

### Mid-Level Packages
- `internal/database` → memory, workflow, pkg/models
- `internal/provider` → (self-reference only)
- `internal/beads` → observability, pkg/config, pkg/models
- `internal/gitops` → database, keymanager, observability, pkg/models
- `internal/project` → gitops, pkg/models
- `internal/executor` → containers, pkg/models
- `internal/analytics` → database

### Higher-Level Packages
- `internal/actions` → build, containers, executor, files, git, gitops, linter, lsp, provider, testing
- `internal/worker` → actions, database, memory, provider
- `internal/agent` → actions, analytics, database, observability, provider, telemetry, eventbus, worker
- `internal/dispatch` → agent, beads, containers, database, gitops, memory, observability, project, provider, swarm, telemetry, eventbus, worker, workflow
- `internal/ralph` → beads, database, dispatch

## Verification

The dependency graph forms a **directed acyclic graph (DAG)**:

1. **pkg/models, pkg/secrets** are leaf nodes with no internal dependencies
2. **pkg/config** depends only on pkg/secrets
3. **internal/memory, internal/workflow** are near-leaf nodes
4. **internal/database** depends on memory and workflow (both lower-level)
5. **internal/actions** depends on lower-level packages (executor, files, git, etc.)
6. **internal/worker** depends on actions (not vice versa)
7. **internal/agent** depends on worker and actions (not vice versa)
8. **internal/dispatch** depends on agent (not vice versa)
9. **internal/loom** is the top-level orchestrator, importing everything but imported by nothing

## Conclusion

The Loom codebase has a well-structured dependency hierarchy with no circular imports. The architecture follows a clean layered approach:

```
                    internal/loom (orchestrator)
                           |
        +------------------+------------------+
        |                  |                  |
   internal/dispatch  internal/ralph     internal/api
        |                  |
   internal/agent    internal/dispatch (shared)
        |
   internal/worker
        |
   internal/actions
        |
   internal/executor, internal/files, etc.
        |
   pkg/models (leaf)
```

---
*Analysis performed: February 2026*
