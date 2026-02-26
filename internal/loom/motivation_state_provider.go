package loom

import (
	"time"

	"github.com/loom-project/loom/internal/motivation"
)

// LoomStateProvider implements motivation.StateProvider for the Loom system
type LoomStateProvider struct {
	loom *Loom
}

// NewLoomStateProvider creates a new state provider backed by Loom
func NewLoomStateProvider(l *Loom) *LoomStateProvider {
	return &LoomStateProvider{loom: l}
}

// GetCurrentTime returns the current time
func (p *LoomStateProvider) GetCurrentTime() time.Time {
	return time.Now()
}

// GetBeadsWithUpcomingDeadlines returns beads with deadlines within the specified days
func (p *LoomStateProvider) GetBeadsWithUpcomingDeadlines(withinDays int) ([]motivation.BeadDeadlineInfo, error) {
	// TODO: Implement when bead deadline tracking is added
	return nil, nil
}

// GetOverdueBeads returns beads that are past their deadline
func (p *LoomStateProvider) GetOverdueBeads() ([]motivation.BeadDeadlineInfo, error) {
	// TODO: Implement when bead deadline tracking is added
	return nil, nil
}

// GetBeadsByStatus returns bead IDs with the specified status
func (p *LoomStateProvider) GetBeadsByStatus(status string) ([]string, error) {
	if p.loom.beadsManager == nil {
		return nil, nil
	}
	// Get all beads and filter by status
	beads, err := p.loom.beadsManager.List("")
	if err != nil {
		return nil, err
	}
	var result []string
	for _, b := range beads {
		if b.Status == status {
			result = append(result, b.ID)
		}
	}
	return result, nil
}

// GetMilestones returns milestones for a project
func (p *LoomStateProvider) GetMilestones(projectID string) ([]*motivation.Milestone, error) {
	// TODO: Implement when milestone tracking is added
	return nil, nil
}

// GetUpcomingMilestones returns milestones within the specified days
func (p *LoomStateProvider) GetUpcomingMilestones(withinDays int) ([]*motivation.Milestone, error) {
	// TODO: Implement when milestone tracking is added
	return nil, nil
}

// GetIdleAgents returns IDs of agents that are currently idle
func (p *LoomStateProvider) GetIdleAgents() ([]string, error) {
	if p.loom.agentManager == nil {
		return nil, nil
	}
	// Get idle agents from the worker manager
	idleAgents := p.loom.agentManager.GetIdleAgents()
	return idleAgents, nil
}

// GetAgentsByRole returns agent IDs with the specified role
func (p *LoomStateProvider) GetAgentsByRole(role string) ([]string, error) {
	if p.loom.agentManager == nil {
		return nil, nil
	}
	// Get agents by role from the worker manager
	agents := p.loom.agentManager.GetAgentsByRole(role)
	return agents, nil
}

// GetProjectIdle returns whether a project has been idle for the specified duration
func (p *LoomStateProvider) GetProjectIdle(projectID string, duration time.Duration) (bool, error) {
	if p.loom.idleDetector == nil {
		return false, nil
	}
	return p.loom.idleDetector.IsProjectIdle(projectID, duration), nil
}

// GetSystemIdle returns whether the entire system has been idle for the specified duration
func (p *LoomStateProvider) GetSystemIdle(duration time.Duration) (bool, error) {
	if p.loom.idleDetector == nil {
		return false, nil
	}
	return p.loom.idleDetector.IsSystemIdle(duration), nil
}

// GetCurrentSpending returns current spending for the period
func (p *LoomStateProvider) GetCurrentSpending(period string) (float64, error) {
	// TODO: Implement when cost tracking is added
	return 0, nil
}

// GetBudgetThreshold returns the budget threshold for a project
func (p *LoomStateProvider) GetBudgetThreshold(projectID string) (float64, error) {
	// TODO: Implement when budget tracking is added
	return 0, nil
}

// GetPendingDecisions returns IDs of pending decisions
func (p *LoomStateProvider) GetPendingDecisions() ([]string, error) {
	if p.loom.decisionManager == nil {
		return nil, nil
	}
	// Get pending decisions
	decisions := p.loom.decisionManager.GetPending()
	var result []string
	for _, d := range decisions {
		result = append(result, d.ID)
	}
	return result, nil
}

// GetUnprocessedExternalEvents returns unprocessed external events of the specified type
func (p *LoomStateProvider) GetUnprocessedExternalEvents(eventType string) ([]motivation.ExternalEvent, error) {
	// TODO: Implement when external event tracking is added
	return nil, nil
}
