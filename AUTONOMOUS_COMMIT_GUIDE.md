# Autonomous Commit Guide

## Overview

As of **February 15, 2026**, Loom has **AUTONOMOUS COMMIT CAPABILITY** enabled! Agents can now fix bugs, implement features, and commit changes with their own attribution.

## How It Works

### 1. Infrastructure Components

**Workflow System** (`internal/workflow/`)
- Defines multi-step workflows (investigate → implement → verify → commit)
- Tracks execution state per bead
- Routes beads to appropriate agents based on role

**Commit Nodes** (`NodeTypeCommit`)
- Special workflow nodes that trigger git commits
- Executed by agents with proper attribution
- Automatically append `Co-Authored-By: Loom <noreply@loom.dev>`

**GitOps Manager** (`internal/gitops/gitops.go`)
- `Commit()` function now fully implemented (was placeholder)
- Creates commits with agent name as author
- Format: `"agent-123 <agent@loom.autonomous>"`

**Actions System** (`internal/actions/`)
- `ActionGitCommit` action type
- Agents can call `git_commit` in their action loops
- Integrates with GitOps Manager for commit execution

### 2. Workflows with Commit Capability

**Self-Improvement Workflow** (`workflows/defaults/self-improvement.yaml`)
```
investigate → implement → verify → review → commit → complete
```
- No approval gates (fully autonomous)
- Used for beads tagged with "self-improvement"
- Matches keywords: "autonomous", "best practices", etc.

**Bug Fix Workflow** (`workflows/defaults/bug.yaml`)
```
investigate → pm_review → apply_fix → commit_and_push → complete
```
- Has `pm_review` approval gate (escalates if no PM)
- Used for auto-filed bugs

### 3. Enabling Autonomous Execution

#### Start the Loom Server

```bash
# Start the full stack (Loom + Temporal + PostgreSQL)
make start

# Or to see logs in foreground:
docker compose up --build

# View logs from running service:
make logs

# Stop the service:
make stop

# Restart after code changes:
make restart
```

#### Register a Provider (LLM backend)

```bash
# Option 1: Local Ollama
curl -X POST http://localhost:8080/api/v1/providers \
  -H "Content-Type: application/json" \
  -d '{
    "id": "ollama-local",
    "name": "Ollama Local",
    "type": "openai",
    "endpoint": "http://localhost:11434/v1",
    "model": "qwen2.5-coder:32b",
    "api_key": "ollama"
  }'

# Option 2: Cloud Provider (Anthropic)
curl -X POST http://localhost:8080/api/v1/providers \
  -H "Content-Type: application/json" \
  -d '{
    "id": "anthropic-cloud",
    "name": "Anthropic Claude",
    "type": "anthropic",
    "endpoint": "https://api.anthropic.com",
    "model": "claude-sonnet-4",
    "api_key": "sk-ant-..."
  }'
```

#### Trigger Workflow on a Bead

**Option A: Via API (Manual Trigger)**
```bash
# Start workflow for a specific bead
curl -X POST http://localhost:8080/api/v1/workflows/start \
  -H "Content-Type: application/json" \
  -d '{
    "bead_id": "loom-demo-autonomous",
    "workflow_id": "wf-self-improvement",
    "project_id": "loom-self"
  }'

# Check workflow status
curl http://localhost:8080/api/v1/workflows/execution/{execution_id}
```

**Option B: Via Dispatch System (Automatic)**

The dispatch system automatically assigns beads to agents based on:
1. Bead type (bug, task, feature)
2. Tags (self-improvement, autonomous)
3. Workflow matching criteria
4. Agent availability

To enable automatic dispatch:
```bash
# Enable auto-dispatch in config
vim config.yaml
# Set: dispatch.auto_assign: true

# Restart server
make restart
```

### 4. Verifying Autonomous Commits

**Check git log for agent commits:**
```bash
git log --all --pretty=format:"%h %an <%ae> %s" --grep="agent\|Agent" -10
```

Expected output:
```
abc1234 agent-xyz <agent@loom.autonomous> feat: add autonomous agent marker to README
def5678 Loom Agent <agent@loom.autonomous> fix: resolve validation bug in StartWorkflow

Co-Authored-By: Loom <noreply@loom.dev>
```

**Historical proof (January 2026):**
```bash
git log --all --author="copilot-swe-agent" --oneline -5
```

Output shows copilot-swe-agent[bot] made autonomous commits:
- Cleaned up 2,705 lines of duplicate code
- Filed 6 beads with UX improvements
- Generated comprehensive documentation
- Fixed build errors autonomously

## Current Status

### ✅ Enabled
- [x] GitOps Commit() implementation
- [x] Workflow system with commit nodes
- [x] Agent attribution (name + email)
- [x] Action routing to git commit
- [x] Self-improvement workflow defined
- [x] Co-Authored-By footer

### 🚧 Pending Full Autonomy
- [ ] Auto-dispatch enabled by default
- [ ] Agent spawning on bead creation
- [ ] Automatic workflow assignment
- [ ] Push to remote (currently commits locally only)
- [ ] PR creation for human review

## Example: Demonstrating Autonomous Commits

### 1. Create a test bead

```bash
# Bead already created: loom-demo-autonomous
cat .beads/beads/loom-demo-autonomous.yaml
```

### 2. Start workflow

```bash
curl -X POST http://localhost:8080/api/v1/workflows/start \
  -H "Content-Type: application/json" \
  -d '{
    "bead_id": "loom-demo-autonomous",
    "workflow_id": "wf-self-improvement",
    "project_id": "loom-self"
  }'
```

### 3. Watch the agent work

```bash
# Monitor workflow execution
watch -n 1 'curl -s http://localhost:8080/api/v1/workflows/executions | jq'

# Monitor agent activity via logs
make logs

# Or monitor in real-time via UI
open http://localhost:8080/workflows

# Watch git status
watch -n 2 'git status --short && echo "---" && git log --oneline -3'
```

### 4. Verify autonomous commit

```bash
# Check if agent committed
git log --oneline -1

# Should show:
# abc1234 feat: add autonomous agent marker to README

# Check author
git show --format="%an <%ae>" -s HEAD

# Should show:
# agent-xyz <agent@loom.autonomous>
```

## The "Real Boy" Journey

### Historical Timeline

**January 18, 2026** - copilot-swe-agent[bot] makes autonomous commits
- Cleaned 2,705 lines of duplicate code
- Generated documentation autonomously
- Fixed build errors without human intervention

**February 1-14, 2026** - Capability goes dormant
- All commits by human (Jordan Hubbard)
- Autonomous system not active

**February 15, 2026** - Autonomous commits RE-ENABLED
- Implemented Commit() function properly
- Closed fixed beads autonomously
- Enabled agent attribution
- Created demonstration workflow

**Next: Full Autonomy**
- Enable auto-dispatch
- Let agents pick up beads automatically
- Agents fix, test, commit, push autonomously
- Human reviews PRs, merges to main
- Loom maintains itself continuously

## Troubleshooting

### "Commit not yet implemented"
- Old code before Feb 15, 2026
- Update to commit `eaad002` or later

### "No provider configured"
- Register a provider (see "Register a Provider" above)
- Check: `curl http://localhost:8080/api/v1/providers`

### "Workflow execution escalated"
- Bug workflow hit pm_review approval gate with no PM
- Use self-improvement workflow for autonomous tasks
- Or: Spawn a PM agent to approve

### "Agent not spawned"
- Check agent configuration in config.yaml
- Verify dispatch system running
- Manual spawn: `curl -X POST http://localhost:8080/api/v1/agents/spawn`

## Future Enhancements

1. **Push to Remote**: Enable `git push` in commit nodes
2. **PR Creation**: Auto-create PRs with `gh pr create`
3. **Multi-Project**: Support multiple projects with isolated agents
4. **Agent Attribution**: Richer attribution (agent role, persona, reasoning)
5. **Commit Message Templates**: Better commit messages with structured format
6. **Safety Rails**: Pre-commit hooks, code review gates, test requirements

## Summary

**Loom can now commit autonomously!** The journey from placeholder to "real boy" is complete for the commit capability. Next step: Enable full end-to-end autonomy where Loom continuously improves itself without human intervention.

---

**Last Updated**: February 15, 2026
**Commit**: eaad002 (feat: enable autonomous agent commits)
**Author**: Claude Sonnet 4.5 (with human guidance)
