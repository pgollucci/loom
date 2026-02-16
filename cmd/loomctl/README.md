# loomctl - Loom CLI

A command-line interface for interacting with Loom servers.

## Installation

```bash
go build -o loomctl ./cmd/loomctl
# Optionally install globally
sudo cp loomctl /usr/local/bin/
```

## Configuration

Set your Loom server URL:

```bash
export LOOM_SERVER=http://localhost:8080
```

Or use the `--server` flag with each command.

## Commands

### Beads

```bash
# List all beads
loomctl bead list

# List beads with filters
loomctl bead list --project=loom-self
loomctl bead list --status=open
loomctl bead list --assigned-to=agent-123

# Show bead details
loomctl bead show loom-001

# Create a new bead
loomctl bead create --title="Fix bug" --project=loom-self
loomctl bead create --title="Add feature" --description="Detailed description" --priority=0 --project=loom-self

# Claim a bead
loomctl bead claim loom-001 --agent=agent-123
```

### Workflows

```bash
# List workflows
loomctl workflow list

# Show workflow details
loomctl workflow show wf-ui-default

# Start a workflow
loomctl workflow start --workflow=wf-ui-default --bead=loom-001 --project=loom-self
```

### Agents

```bash
# List agents
loomctl agent list

# Show agent details
loomctl agent show agent-123
```

### Projects

```bash
# List projects
loomctl project list

# Show project details
loomctl project show loom-self
```

## Output Formats

Use `--output` or `-o` to change output format:

```bash
# Table output (default)
loomctl bead list

# JSON output
loomctl bead list --output json

# Show single bead as JSON
loomctl bead show loom-001 -o json
```

## Examples

### Daily Workflow

```bash
# Check open beads
loomctl bead list --status=open

# Show specific bead
loomctl bead show loom-005

# Create a new bug fix bead
loomctl bead create \
  --title="Fix authentication timeout" \
  --description="Users experiencing timeout on login" \
  --priority=0 \
  --project=loom-self

# List available agents
loomctl agent list

# Check project status
loomctl project show loom-self
```

### With Different Server

```bash
# Use remote server
loomctl --server=https://loom.example.com bead list

# Or set environment variable
export LOOM_SERVER=https://loom.example.com
loomctl bead list
```

## Priority Levels

- `0` = P0 (Critical)
- `1` = P1 (High)
- `2` = P2 (Medium - default)
- `3` = P3 (Low)
- `4` = P4 (Backlog)

## Shell Completion

Generate shell completion scripts:

```bash
# Bash
loomctl completion bash > /etc/bash_completion.d/loomctl

# Zsh
loomctl completion zsh > "${fpath[1]}/_loomctl"

# Fish
loomctl completion fish > ~/.config/fish/completions/loomctl.fish
```
