# Deployment Guide: Workflow System (Gaps #1-3)

## Overview
This deployment includes 3 critical workflow system enhancements for autonomous self-healing:
- **Gap #1:** Multi-dispatch redispatch flag (agents get multiple turns)
- **Gap #2:** Commit serialization (prevents git conflicts)
- **Gap #3:** Agent role inference (accurate workflow routing)

## Pre-Deployment Checklist

### 1. Verify Build ✅
```bash
go test ./internal/workflow/... ./internal/dispatch/... ./internal/agent/...
go build -o loom ./cmd/loom
./loom --version
```

### 2. Backup Current State
```bash
# Backup database
cp ~/.loom/loom.db ~/.loom/loom.db.backup-$(date +%Y%m%d-%H%M%S)

# Backup beads
tar -czf ~/.loom/beads-backup-$(date +%Y%m%d-%H%M%S).tar.gz .beads/

# Note current commit
git rev-parse HEAD > ~/.loom/pre-deployment-commit.txt
```

### 3. Review Commits
```bash
git log --oneline origin/main..main  # If pushing to remote
# OR
git log --oneline -6  # Last 6 commits
```

**Expected commits:**
- `55cb079` - fix(agent): update tests to match new role inference behavior
- `b9d3258` - feat(workflow): implement agent role inference (Gap #3)
- `d47a5c4` - feat(workflow): implement commit serialization (Gap #2)
- `31fa491` - test(workflow): comprehensive unit tests for multi-dispatch
- `7a5ed10` - feat(workflow): implement multi-dispatch redispatch flag (Gap #1)

## Deployment Steps

### 1. Stop Loom (if running)
```bash
# Find loom process
ps aux | grep loom

# Graceful shutdown (if loom supports it)
killall -TERM loom

# Wait for processes to exit
sleep 5

# Force kill if needed
killall -9 loom
```

### 2. Deploy New Binary
```bash
# Option A: Replace in-place
cp ./loom /usr/local/bin/loom  # Or wherever loom is installed

# Option B: Install with make (if Makefile exists)
make install

# Verify version
loom --version
```

### 3. Database Migration (if needed)
```bash
# Check if migrations are needed
# The workflow system database schema should already exist from Phase 1-5
# No migrations needed for Gaps #1-3 (behavior changes only)

# Verify workflow tables exist
sqlite3 ~/.loom/loom.db "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'workflow%';"
# Should show: workflows, workflow_nodes, workflow_edges, workflow_executions, workflow_transitions
```

### 4. Start Loom
```bash
# Start in foreground for initial verification
loom start

# OR start as daemon (if supported)
loom daemon start

# OR with systemd (if configured)
systemctl start loom
```

### 5. Verify Startup
```bash
# Check logs
tail -f ~/.loom/logs/loom.log  # Or wherever logs are

# Verify workflow engine loaded
grep -i "workflow" ~/.loom/logs/loom.log | tail -20

# Check for commit queue processor
grep -i "commit.*queue" ~/.loom/logs/loom.log | tail -10
```

**Expected log entries:**
```
[Workflow] Workflow engine initialized
[Commit] Processing commit queue started
[Dispatcher] Commit lock initialized (timeout: 5m0s)
```

## Post-Deployment Verification

### 1. System Health Check
```bash
# Check loom status
loom status

# Verify agents are active
loom agent list

# Check workflow execution status
loom workflow list --active
```

### 2. Create Test Bead
```bash
# Create a simple bug bead to test workflow
loom bead create \
  --type bug \
  --title "Test: Verify multi-dispatch works" \
  --description "This is a test bead to verify Gap #1 implementation" \
  --priority P1

# Watch it get dispatched
tail -f ~/.loom/logs/loom.log | grep -i "dispatch\|workflow\|redispatch"
```

**What to look for:**
- `redispatch_requested = "true"` in bead context
- Agent gets multiple dispatch cycles
- Workflow advances through nodes correctly

### 3. Verify Role Assignment
```bash
# Check agent roles
loom agent list --format json | jq '.[] | {name: .name, persona: .persona, role: .role}'

# Expected output: Agents should have workflow role names
# Example:
# {
#   "name": "QA Agent",
#   "persona": "default/qa-engineer",
#   "role": "QA"
# }
```

### 4. Test Commit Serialization
```bash
# This requires multiple concurrent workflows reaching commit nodes
# Monitor commit lock activity:
grep -i "\[Commit\]" ~/.loom/logs/loom.log | tail -50

# Look for:
# - "Bead X queued for commit"
# - "Processing commit for bead X"
# - "Acquired commit lock"
# - "Releasing commit lock (held for Xms)"
```

## Monitoring & Metrics

### Key Metrics to Track

#### 1. Multi-Dispatch (Gap #1)
```bash
# Count beads with redispatch flag
sqlite3 ~/.loom/loom.db "SELECT COUNT(*) FROM beads WHERE json_extract(context, '$.redispatch_requested') = 'true';"

# Average dispatch count per bead
sqlite3 ~/.loom/loom.db "SELECT AVG(CAST(json_extract(context, '$.dispatch_count') AS INTEGER)) FROM beads WHERE type = 'bug';"

# Success metric: >1.5 average dispatch count for bug beads
```

#### 2. Commit Lock (Gap #2)
```bash
# Monitor commit queue depth (in logs)
grep "Bead.*queued for commit" ~/.loom/logs/loom.log | wc -l

# Average commit hold time (parse from logs)
grep "Releasing commit lock (held for" ~/.loom/logs/loom.log | \
  tail -100 | \
  awk '{print $(NF-1)}' | \
  # Convert to ms and average

# Success metrics:
# - No git conflicts in logs
# - Commit hold time < 30 seconds (typically)
# - Queue depth < 10 (under normal load)
```

#### 3. Role-Based Routing (Gap #3)
```bash
# Count beads routed by role vs persona fallback
grep "role match" ~/.loom/logs/loom.log | wc -l
grep "persona fallback" ~/.loom/logs/loom.log | wc -l

# Check agent role distribution
sqlite3 ~/.loom/loom.db "SELECT role, COUNT(*) FROM agents GROUP BY role;"

# Success metric: >80% role matches (not persona fallback)
```

#### 4. Workflow Completion
```bash
# Count completed workflows
sqlite3 ~/.loom/loom.db "SELECT COUNT(*) FROM workflow_executions WHERE status = 'completed';"

# Average workflow duration
sqlite3 ~/.loom/loom.db "SELECT AVG(julianday(completed_at) - julianday(started_at)) * 24 * 60 FROM workflow_executions WHERE status = 'completed';"

# Escalation rate
sqlite3 ~/.loom/loom.db "SELECT COUNT(*) FROM workflow_executions WHERE status = 'escalated';"

# Success metrics:
# - >50% completion rate
# - <5 minute average duration
# - <10% escalation rate
```

### Monitoring Commands

#### Real-time Workflow Monitoring
```bash
# Watch workflow activity
watch -n 5 'sqlite3 ~/.loom/loom.db "SELECT status, COUNT(*) FROM workflow_executions GROUP BY status;"'

# Monitor dispatch activity
tail -f ~/.loom/logs/loom.log | grep -E '\[Dispatcher\]|\[Workflow\]|\[Commit\]'

# Watch commit lock
tail -f ~/.loom/logs/loom.log | grep '\[Commit\]'
```

#### Periodic Health Checks
```bash
# Create monitoring script: ~/.loom/monitor.sh
cat > ~/.loom/monitor.sh << 'EOF'
#!/bin/bash
echo "=== Loom Workflow System Health Check ==="
echo "Time: $(date)"
echo ""

echo "Active Workflows:"
sqlite3 ~/.loom/loom.db "SELECT status, COUNT(*) FROM workflow_executions GROUP BY status;"
echo ""

echo "Commit Queue Activity (last 10):"
grep "\[Commit\]" ~/.loom/logs/loom.log | tail -10
echo ""

echo "Agent Status:"
sqlite3 ~/.loom/loom.db "SELECT status, COUNT(*) FROM agents GROUP BY status;"
echo ""

echo "Recent Beads:"
sqlite3 ~/.loom/loom.db "SELECT id, type, status, priority FROM beads ORDER BY created_at DESC LIMIT 5;"
EOF

chmod +x ~/.loom/monitor.sh

# Run every 5 minutes
# */5 * * * * ~/.loom/monitor.sh >> ~/.loom/monitor.log 2>&1
```

## Rollback Procedure

If issues arise, rollback to previous version:

### 1. Stop Loom
```bash
killall -TERM loom
```

### 2. Restore Previous Binary
```bash
# Get pre-deployment commit
PREV_COMMIT=$(cat ~/.loom/pre-deployment-commit.txt)

# Checkout previous version
git checkout $PREV_COMMIT

# Rebuild
go build -o loom ./cmd/loom
cp ./loom /usr/local/bin/loom
```

### 3. Restore Database (if needed)
```bash
# Find latest backup
ls -lt ~/.loom/*.backup-* | head -1

# Restore
cp ~/.loom/loom.db.backup-YYYYMMDD-HHMMSS ~/.loom/loom.db
```

### 4. Restart Loom
```bash
loom start
```

## Known Issues & Workarounds

### Issue 1: Commit Lock Timeout
**Symptom:** "WARNING: Previous commit by agent X timed out after 5m"

**Impact:** Previous commit was interrupted, lock forcibly released

**Action:** Check why commit took >5 minutes:
- Large file changes?
- Git push failures?
- Network issues?

**Workaround:** Increase timeout in dispatcher.go if needed (default: 5 minutes)

### Issue 2: Redispatch Loop
**Symptom:** Bead dispatched >10 times without completion

**Impact:** Agent stuck in investigation loop

**Action:** Check cycle detection:
```bash
sqlite3 ~/.loom/loom.db "SELECT * FROM workflow_executions WHERE cycle_count > 3;"
```

**Workaround:** System auto-escalates after 3 cycles (already implemented)

### Issue 3: Role Mismatch
**Symptom:** Agent with wrong role assigned to workflow node

**Impact:** Task routed to incorrect agent type

**Action:** Verify role mapping:
```bash
# Check agent roles
loom agent list

# Update persona if needed
loom agent update <agent-id> --persona default/qa-engineer
```

**Workaround:** System falls back to persona matching (existing behavior)

## Performance Considerations

### Expected Performance Impact

1. **Multi-Dispatch (Gap #1):**
   - Impact: Negligible (<1ms per dispatch)
   - Memory: +8 bytes per bead (flag in context)

2. **Commit Lock (Gap #2):**
   - Impact: Serialized commits add latency for concurrent commits
   - Typical: <100ms queue wait time
   - Worst case: 5 minute timeout if commit hangs

3. **Role Inference (Gap #3):**
   - Impact: Negligible (<1ms per agent creation)
   - Memory: No change (Role field already existed)

### Scaling Considerations

- **Commit queue capacity:** 100 pending commits (adjust if needed)
- **Commit timeout:** 5 minutes (adjust for large repos)
- **Workflow cycle limit:** 3 cycles before escalation
- **Max dispatch hops:** Configured in dispatcher settings

## Support & Troubleshooting

### Debug Mode
```bash
# Enable verbose logging
loom start --log-level debug

# OR set environment variable
export LOOM_LOG_LEVEL=debug
loom start
```

### Useful Log Queries
```bash
# Find workflow failures
grep "Failed to" ~/.loom/logs/loom.log | grep -i workflow

# Find commit conflicts
grep -i "conflict" ~/.loom/logs/loom.log

# Find role routing issues
grep -i "role" ~/.loom/logs/loom.log | grep -i "mismatch\|fallback"
```

### Database Inspection
```bash
# Workflow execution details
sqlite3 ~/.loom/loom.db "SELECT * FROM workflow_executions WHERE status = 'active' ORDER BY started_at DESC LIMIT 10;"

# Stuck workflows (running >1 hour)
sqlite3 ~/.loom/loom.db "SELECT * FROM workflow_executions WHERE status = 'active' AND julianday('now') - julianday(started_at) > 1/24;"

# Commit node executions
sqlite3 ~/.loom/loom.db "SELECT * FROM workflow_executions WHERE current_node_key LIKE '%commit%';"
```

## Success Criteria

Deployment is successful when:

✅ All tests pass (`go test ./...`)
✅ Loom starts without errors
✅ Workflow engine initialized
✅ Commit queue processor running
✅ Agents have correct roles assigned
✅ Multi-turn investigations work (dispatch_count > 1)
✅ No git conflicts in concurrent workflows
✅ Role-based routing works (>80% role matches)
✅ At least 1 workflow completes end-to-end

## Next Steps After Deployment

1. **Monitor for 24 hours** - Watch metrics and logs for anomalies
2. **Create test scenarios** - File deliberate bugs to test self-healing
3. **Measure improvements** - Compare before/after metrics
4. **Document learnings** - Update runbook with operational insights
5. **Tune parameters** - Adjust timeouts, queue sizes based on production data

## Contact

For issues or questions:
- Review logs: `~/.loom/logs/loom.log`
- Check status: `loom status --verbose`
- File bug: `loom bead create --type bug`
