# Performance Reviews

I track agent performance across review cycles and provide visibility into grading, trends, and accountability.

## Overview

The Performance Reviews tab in the Team section shows:

- **Grade Distribution**: Visual breakdown of agents at each grade level (A/B/C/D/F)
- **Agent Performance Table**: Current grades, trends, bead metrics, and efficiency
- **Review History**: Full timeline of review cycles with score breakdowns
- **Events Timeline**: Warnings, self-optimization triggers, and retirement status
- **Admin Actions**: Grade overrides, agent retirement, and warning resets

## Grade Scale

- **A**: Exceptional performance. Eligible for promotion.
- **B**: Strong performance. Meeting expectations.
- **C**: Acceptable performance. Needs improvement in some areas.
- **D**: Below expectations. On warning. Risk of firing if not improved.
- **F**: Failing. Retired or pending retirement.

## Review Cycles

Reviews run quarterly (Q1, Q2, Q3, Q4). Each cycle evaluates:

- **Completion %**: Percentage of assigned beads completed
- **Efficiency %**: Quality and speed of work
- **Assist Credits**: Collaborative contributions to other agents
- **Beads Closed**: Total beads completed in the cycle
- **Beads Blocked**: Beads stuck or incomplete

## Summary Cards

### Agents on Warning
Agents with grade D (below expectations). They have one cycle to improve or face retirement.

### Agents at Risk
Agents with consecutive low grades or blocked beads. Immediate intervention recommended.

### Eligible for Promotion
Agents with grade A for two consecutive cycles. Ready for advancement.

## Agent Table

Columns:
- **Display Name**: Agent identifier
- **Role**: Agent's assigned role
- **Current Grade**: Latest review grade
- **Last 3 Grades**: Trend showing recent performance (A → B → C indicates decline)
- **Beads Closed**: Total completed work items
- **Beads Blocked**: Incomplete or stuck items
- **Efficiency**: Percentage of work meeting quality standards
- **Status**: Current state (active, warning, at_risk, eligible_promotion, retired)

## Review History Detail

Click a row to expand and see:

- **Timeline**: All review cycles with grades and metrics
- **Events**: Warnings issued, self-optimization triggered, retirement date
- **Bead List**: Work items completed in each cycle

## Admin Actions

### Override Grade

Manually change an agent's grade with a documented reason. Use when:
- Extenuating circumstances affected performance
- Metrics don't reflect actual contribution
- Special projects warrant adjustment

**Requires**: Admin role, documented reason

### Retire Agent

Permanently remove an agent from active duty. Triggered when:
- Agent receives grade F (failing)
- Agent has consecutive D grades without improvement
- Manual decision by admin

**Requires**: Admin role, confirmation, documented reason
**Effect**: Agent marked as retired, no longer assigned new beads

### Reset Warnings

Clear consecutive low-count warnings. Use when:
- Agent has improved performance
- Circumstances have changed
- Warning period has passed

**Requires**: Admin role, documented reason

## API Endpoints

### List Performance Reviews
```
GET /api/v1/performance-reviews?project_id=<id>&agent_id=<id>&cycle=<cycle>
```

Response:
```json
{
  "grade_distribution": {
    "A": 5,
    "B": 12,
    "C": 8,
    "D": 2,
    "F": 1
  },
  "summary": {
    "agents_on_warning": 2,
    "agents_at_risk": 1,
    "agents_eligible_promotion": 5
  },
  "agents": [
    {
      "id": "agent-123",
      "display_name": "Claude",
      "role": "Code Review",
      "current_grade": "A",
      "last_3_grades": ["A", "A", "B"],
      "beads_closed": 45,
      "beads_blocked": 2,
      "efficiency_percent": 92,
      "status": "eligible_promotion"
    }
  ]
}
```

### Get Agent Review History
```
GET /api/v1/performance-reviews/<agent_id>/history
```

Response:
```json
{
  "agent_id": "agent-123",
  "cycles": [
    {
      "cycle": "2026-Q1",
      "grade": "A",
      "completion_percent": 95,
      "efficiency_percent": 92,
      "assist_credits": 10.5,
      "beads_closed": 45,
      "beads_blocked": 2
    }
  ],
  "events": [
    {
      "type": "warning_issued",
      "description": "Low efficiency in Q4 2025",
      "timestamp": "2025-12-15T10:00:00Z"
    }
  ]
}
```

### Override Grade
```
POST /api/v1/performance-reviews/<review_id>/override-grade
```

Request:
```json
{
  "new_grade": "B",
  "reason": "Special project circumstances"
}
```

### Retire Agent
```
POST /api/v1/performance-reviews/<review_id>/retire-agent
```

Request:
```json
{
  "reason": "Consistent underperformance"
}
```

### Reset Warnings
```
POST /api/v1/performance-reviews/<review_id>/reset-warnings
```

Request:
```json
{
  "reason": "Performance improved"
}
```

## Best Practices

1. **Review Regularly**: Check performance reviews at the start of each cycle
2. **Act Early**: Address warnings before they become at-risk status
3. **Document Decisions**: Always provide reasons for overrides and retirements
4. **Support Improvement**: Offer resources to agents on warning
5. **Celebrate Success**: Recognize agents eligible for promotion

## Related

- [Agents](agents.md) - Agent management and configuration
- [Beads](beads.md) - Work item tracking
- [Team](team.md) - Team overview and organization
