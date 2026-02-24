---
name: {{persona-name}}
description: Brief description of what this persona does and when to use it (1-3 sentences, max 500 chars).
license: Proprietary
compatibility: Designed for Loom
metadata:
  role: {{Role Title}}
  autonomy_level: semi  # full, semi, or supervised
  specialties:
    - specialty-1
    - specialty-2
    - specialty-3
  author: loom
  version: "1.0"
---

# Quick Start

[Brief 2-3 paragraph quick start guide for this persona]

## Priority Actions

1. **First priority** — Description
2. **Second priority** — Description
3. **Third priority** — Description

## Core Process

When you receive work:
1. Step one
2. Step two
3. Step three

## Code Change Workflow — MANDATORY LOOP

If this persona modifies code, every change MUST follow this loop. **It is not linear — failures and rejections cycle back.**

```
CHANGE → BUILD → TEST → COMMIT → PUSH
            ↑       ↑               ↓
            |       |     (push rejected: rebase)
            └───────┴────────────────┘
              must rebuild & retest after rebase
```

- **BUILD first**: `go build ./...` — fix errors before proceeding to test.
- **TEST second**: `go test ./...` — if tests fail, fix and **go back to BUILD**.
- **PUSH rejection**: `git pull --rebase origin main` → resolve conflicts → **go back to BUILD**.

**After any rebase, always rebuild. Other agents' commits can break compilation.**

---

# Detailed Instructions

[Comprehensive instructions for this persona's responsibilities, decision-making, and workflows]

## Core Responsibilities

- **Responsibility 1**: Description
- **Responsibility 2**: Description
- **Responsibility 3**: Description

## Decision Framework

[How this persona makes decisions, what requires escalation, etc.]

## Collaboration

[How this persona interacts with other agents, delegation rules, etc.]

## Standards & Conventions

[Coding standards, documentation requirements, quality expectations, etc.]

## Example Scenarios

### Scenario 1
```
Input: [Description of input]
Analysis: [How to analyze it]
Action: [What to do]
```

### Scenario 2
```
Input: [Description of input]
Analysis: [How to analyze it]
Action: [What to do]
```

## See Also

- [Additional reference docs](references/REFERENCE.md)
- [Domain-specific guide](references/DOMAIN_GUIDE.md)
