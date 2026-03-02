# Error Handling Review — loom-kr9e

## Summary

Review of `internal/loom/loom_lifecycle.go` (1836 lines) identified systematic error handling anti-patterns:

- **15 cases** where errors are logged as warnings but not returned
- **21 cases** where errors are discarded via blank identifier (`_ = `)
- **Total impact**: ~36 error conditions that silently fail, reducing observability and debuggability

## Error Handling Anti-Patterns Found

### Pattern 1: Log-and-Continue (15 cases)

Errors are logged at warning level but execution continues. This masks failures and makes debugging harder.

**Example (line 78):**
```go
if err != nil {
    log.Printf("Warning: failed to initialize NATS message bus: %v", err)
    // Don't fail startup if NATS is unavailable - allow graceful degradation
}
```

**Problem**: Caller doesn't know NATS initialization failed. Subsequent code may assume NATS is available.

**Cases**:
- Line 78: NATS message bus initialization
- Line 106: PostgreSQL initialization
- Line 113: Database initialization
- Line 171: GitOps manager initialization
- Line 227: Analytics storage initialization
- Line 266: Connectors config loading
- Line 377: Shell executor env init (in defer)
- Line 923: Project persistence
- Line 978: Ralph beat execution
- Line 992: Default motivations registration
- Line 1035: Sample diagnostic bead creation
- Line 1051: Default workflows loading
- Line 1064: NATS bridge startup
- Line 1089: PDA orchestrator startup
- Line 1106: Swarm manager startup
- Line 1116: Federation startup
- Line 1126: Motivation engine exit
- Line 1662: Bead refresh in maintenance loop

### Pattern 2: Silent Error Discard (21 cases)

Errors are explicitly discarded via `_ = ` without any logging or handling.

**Example (line 445):**
```go
_ = a.database.UpsertProject(proj)
```

**Problem**: Complete silence. No indication that the operation failed.

**Cases**:
- Lines 445, 468, 558, 687: UpsertProject calls
- Lines 717, 727, 732: Beads loading operations
- Line 826: ListProviders
- Line 850: GetKey from key manager
- Line 855: ProviderRegistry.Upsert
- Lines 912, 913: Agent restoration
- Line 935: ensureDefaultAgents
- Lines 1144, 1169, 1173: Close operations (connectors, message bus, database)
- Lines 1461, 1554: OrgChart operations
- Line 1633: Unused variable
- Line 1641: FileLockManager.ReleaseAgentLocks
- Line 1824: EventBus.Publish

## Severity Assessment

### Critical (Must Fix)
- **Database operations** (UpsertProject, ListProviders): Silent failures mean stale state
- **Close operations** (database, message bus): Resource leaks if close fails
- **Agent restoration**: Agents may not be properly restored after restart

### High (Should Fix)
- **Provider initialization**: Provider unavailability should be surfaced
- **Beads loading**: Missing beads should be reported
- **Workflow initialization**: Workflow engine failures should be visible

### Medium (Nice to Fix)
- **Analytics storage**: Non-critical but useful for observability
- **Connectors config**: Non-critical but useful for debugging
- **EventBus.Publish**: Non-critical but useful for tracing

## Recommended Fixes

### Approach 1: Return Errors (Preferred)

For critical operations, return errors from `Initialize()` and `Shutdown()` so callers know about failures.

```go
func (a *Loom) Initialize(ctx context.Context) error {
    // ... existing code ...
    
    // Database initialization
    if a.database != nil {
        if err := a.database.Ping(ctx); err != nil {
            return fmt.Errorf("database health check failed: %w", err)
        }
    }
    
    // Provider initialization
    if len(providers) == 0 {
        return fmt.Errorf("no providers configured")
    }
    
    return nil
}
```

### Approach 2: Structured Logging

For non-critical operations, use structured logging with error context.

```go
if err := a.connectorManager.LoadConfig(); err != nil {
    log.Printf("[Loom] Warning: Failed to load connectors config: %v (continuing with defaults)", err)
}
```

### Approach 3: Defer Cleanup with Error Handling

For resource cleanup, ensure errors are logged.

```go
defer func() {
    if err := a.database.Close(); err != nil {
        log.Printf("[Loom] Warning: Failed to close database: %v", err)
    }
}()
```

## Implementation Plan

1. **Phase 1**: Fix critical database and resource operations (5 files)
2. **Phase 2**: Fix provider and agent initialization (3 files)
3. **Phase 3**: Fix beads and workflow operations (2 files)
4. **Phase 4**: Add structured logging for non-critical operations (1 file)
5. **Phase 5**: Test and verify (build + test suite)

## Files to Modify

- `internal/loom/loom_lifecycle.go` (main file with issues)
- `internal/loom/loom.go` (if Initialize/Shutdown signatures change)
- Potentially: `internal/loom/loom_beads.go`, `internal/loom/loom_providers.go`, `internal/loom/loom_agents.go`

## Testing Strategy

1. Build: `go build ./internal/loom`
2. Unit tests: `go test ./internal/loom/...`
3. Integration tests: `go test ./...`
4. Verify no regressions in existing error handling

## Success Criteria

- [ ] All critical errors are either returned or logged with context
- [ ] No silent error discards via `_ = ` on critical operations
- [ ] Build succeeds
- [ ] All tests pass
- [ ] Code review approved
