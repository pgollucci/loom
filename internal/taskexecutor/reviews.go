package taskexecutor

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/internal/persona"
	"github.com/jordanhubbard/loom/pkg/models"
)

const (
	reviewInterval    = 7 * 24 * time.Hour
	fireThreshold     = 2 // Consecutive D/F cycles before termination
	minBeadsForReview = 3
)

// Priority weights: P0 work counts more than P3 work.
var priorityWeight = map[models.BeadPriority]float64{
	models.BeadPriorityP0: 4.0,
	models.BeadPriorityP1: 2.5,
	models.BeadPriorityP2: 1.5,
	models.BeadPriorityP3: 1.0,
}

// Expected iteration budgets per priority. P0 beads are inherently harder
// and get a larger iteration budget before penalizing efficiency.
var iterationBudget = map[models.BeadPriority]float64{
	models.BeadPriorityP0: 100,
	models.BeadPriorityP1: 60,
	models.BeadPriorityP2: 30,
	models.BeadPriorityP3: 15,
}

// AgentReview is the outcome of a single performance review cycle.
type AgentReview struct {
	AgentID        string    `json:"agent_id"`
	AgentName      string    `json:"agent_name"`
	PersonaName    string    `json:"persona_name"`
	Grade          string    `json:"grade"`
	WeightedScore  float64   `json:"weighted_score"`
	BeadsAttempted int       `json:"beads_attempted"`
	BeadsClosed    int       `json:"beads_closed"`
	BeadsBlocked   int       `json:"beads_blocked"`
	AssistCredits  float64   `json:"assist_credits"`
	Efficiency     float64   `json:"efficiency"`
	ReviewedAt     time.Time `json:"reviewed_at"`
	Action         string    `json:"action"`
	Breakdown      string    `json:"breakdown"` // Human-readable explanation
}

// ReviewManager runs weekly performance reviews for all agents.
type ReviewManager struct {
	agentManager   AgentManager
	beadManager    BeadManagerForReview
	personaManager *persona.Manager
	ceoEscalator   CEOEscalator
	reviews        map[string][]*AgentReview
	mu             sync.RWMutex
}

// BeadManagerForReview is the subset of the bead manager needed for reviews.
type BeadManagerForReview interface {
	ListBeads(filter map[string]interface{}) ([]*models.Bead, error)
}

func NewReviewManager(am AgentManager, bm BeadManagerForReview, pm *persona.Manager) *ReviewManager {
	return &ReviewManager{
		agentManager:   am,
		beadManager:    bm,
		personaManager: pm,
		reviews:        make(map[string][]*AgentReview),
	}
}

func (rm *ReviewManager) SetCEOEscalator(esc CEOEscalator) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.ceoEscalator = esc
}

// StartReviewLoop runs the weekly performance review cycle.
func (rm *ReviewManager) StartReviewLoop(ctx context.Context) {
	log.Printf("[Reviews] Performance review system started (interval: %v)", reviewInterval)

	select {
	case <-ctx.Done():
		return
	case <-time.After(24 * time.Hour):
	}

	rm.RunReviewCycle(ctx)

	ticker := time.NewTicker(reviewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.RunReviewCycle(ctx)
		}
	}
}

// RunReviewCycle reviews all agents and takes action on poor performers.
func (rm *ReviewManager) RunReviewCycle(ctx context.Context) {
	if rm.agentManager == nil {
		return
	}

	agents := rm.agentManager.ListAgents()
	if len(agents) == 0 {
		return
	}

	log.Printf("[Reviews] Starting performance review cycle for %d agents", len(agents))

	var reviewed, warned, optimized, fired int
	for _, ag := range agents {
		if ag == nil {
			continue
		}

		review := rm.reviewAgent(ag, agents)
		if review == nil {
			continue
		}

		rm.mu.Lock()
		rm.reviews[ag.ID] = append(rm.reviews[ag.ID], review)
		if len(rm.reviews[ag.ID]) > 12 {
			rm.reviews[ag.ID] = rm.reviews[ag.ID][len(rm.reviews[ag.ID])-12:]
		}
		rm.mu.Unlock()

		reviewed++

		switch review.Action {
		case "warning":
			warned++
			log.Printf("[Reviews] %s (%s): grade %s (score %.1f) — WARNING\n  %s",
				review.AgentName, review.PersonaName, review.Grade, review.WeightedScore, review.Breakdown)
		case "self_optimize":
			optimized++
			log.Printf("[Reviews] %s (%s): grade %s (score %.1f) — self-optimization triggered\n  %s",
				review.AgentName, review.PersonaName, review.Grade, review.WeightedScore, review.Breakdown)
			rm.triggerSelfOptimization(ctx, ag)
		case "fired":
			fired++
			log.Printf("[Reviews] %s (%s): grade %s (score %.1f) — TERMINATED\n  %s",
				review.AgentName, review.PersonaName, review.Grade, review.WeightedScore, review.Breakdown)
			rm.fireAgent(ctx, ag, review)
		default:
			log.Printf("[Reviews] %s (%s): grade %s (score %.1f)\n  %s",
				review.AgentName, review.PersonaName, review.Grade, review.WeightedScore, review.Breakdown)
		}
	}

	log.Printf("[Reviews] Review cycle complete: %d reviewed, %d warned, %d self-optimized, %d fired",
		reviewed, warned, optimized, fired)
}

// reviewAgent scores a single agent over the review window.
// The scoring system has four components:
//
//  1. Weighted completion: closed beads weighted by priority (P0=4x, P1=2.5x, P2=1.5x, P3=1x)
//  2. Block penalty: blocked beads weighted by priority (penalizes blocking high-priority work more)
//  3. Efficiency: per-bead iteration count vs budget for that priority (within budget = 1.0, over = <1.0)
//  4. Assist credits: beads where this agent was consulted, reviewed code, or participated in meetings
func (rm *ReviewManager) reviewAgent(ag *models.Agent, allAgents []*models.Agent) *AgentReview {
	allBeads, err := rm.beadManager.ListBeads(map[string]interface{}{
		"project_id": ag.ProjectID,
	})
	if err != nil {
		return nil
	}

	windowStart := time.Now().Add(-reviewInterval)

	var (
		attempted       int
		closed          int
		blocked         int
		weightedClosed  float64
		weightedBlocked float64
		efficiencySum   float64
		efficiencyCount int
		assistCredits   float64
	)

	for _, b := range allBeads {
		if b == nil || b.UpdatedAt.Before(windowStart) {
			continue
		}

		owned := isOwnedBy(b, ag)
		assisted := isAssistedBy(b, ag)

		if !owned && !assisted {
			continue
		}

		pw := priorityWeight[b.Priority]
		if pw == 0 {
			pw = 1.0
		}

		if owned {
			attempted++
			switch b.Status {
			case models.BeadStatusClosed:
				closed++
				weightedClosed += pw

				// Efficiency: how many iterations vs the budget for this priority
				var iters int
				fmt.Sscanf(b.Context["dispatch_count"], "%d", &iters)
				if iters > 0 {
					budget := iterationBudget[b.Priority]
					if budget == 0 {
						budget = 30
					}
					eff := math.Min(1.0, budget/float64(iters))
					efficiencySum += eff
					efficiencyCount++
				}

			case models.BeadStatusBlocked:
				blocked++
				weightedBlocked += pw
			}
		}

		if assisted {
			// Assist credit: 0.5x the priority weight for consultations,
			// code reviews, meeting participation
			assistCredits += pw * 0.5
		}
	}

	if attempted < minBeadsForReview {
		return nil
	}

	// Efficiency score: average across all closed beads (1.0 = within budget)
	efficiency := 1.0
	if efficiencyCount > 0 {
		efficiency = efficiencySum / float64(efficiencyCount)
	}

	// Composite score (0-100 scale):
	//   Completion component (60%): weighted closed / (weighted closed + weighted blocked + remaining)
	//   Efficiency component (20%): average efficiency * 20
	//   Assist component (20%): assist credits / attempted, capped at 20
	weightedAttempted := weightedClosed + weightedBlocked
	remainingWeight := float64(attempted-closed-blocked) * 1.0
	weightedAttempted += remainingWeight

	completionScore := 0.0
	if weightedAttempted > 0 {
		completionScore = (weightedClosed / weightedAttempted) * 60.0
	}
	efficiencyScore := efficiency * 20.0
	assistScore := 0.0
	if attempted > 0 {
		assistScore = math.Min(20.0, (assistCredits/float64(attempted))*20.0)
	}

	totalScore := completionScore + efficiencyScore + assistScore

	grade := scoreToGrade(totalScore)

	breakdown := fmt.Sprintf("attempted=%d closed=%d(wt=%.1f) blocked=%d(wt=%.1f) assists=%.1f eff=%.0f%% | completion=%.1f/60 efficiency=%.1f/20 assists=%.1f/20 → total=%.1f/100",
		attempted, closed, weightedClosed, blocked, weightedBlocked,
		assistCredits, efficiency*100,
		completionScore, efficiencyScore, assistScore, totalScore)

	review := &AgentReview{
		AgentID:        ag.ID,
		AgentName:      ag.Name,
		PersonaName:    ag.PersonaName,
		Grade:          grade,
		WeightedScore:  totalScore,
		BeadsAttempted: attempted,
		BeadsClosed:    closed,
		BeadsBlocked:   blocked,
		AssistCredits:  assistCredits,
		Efficiency:     efficiency,
		ReviewedAt:     time.Now().UTC(),
		Breakdown:      breakdown,
	}

	consecutiveLow := rm.consecutiveLowCount(ag.ID)
	switch {
	case grade == "D" || grade == "F":
		if consecutiveLow >= fireThreshold {
			review.Action = "fired"
		} else if consecutiveLow >= 1 {
			review.Action = "self_optimize"
		} else {
			review.Action = "warning"
		}
	default:
		review.Action = "none"
	}

	return review
}

func isOwnedBy(b *models.Bead, ag *models.Agent) bool {
	if b.AssignedTo == ag.ID {
		return true
	}
	if ag.Name != "" && strings.Contains(b.AssignedTo, ag.Name) {
		return true
	}
	if b.Context != nil && b.Context["last_assigned_to"] == ag.ID {
		return true
	}
	return false
}

// isAssistedBy checks whether this agent contributed to a bead they don't own:
// code review, consultation, meeting participation, or voting.
func isAssistedBy(b *models.Bead, ag *models.Agent) bool {
	if b.Context == nil {
		return false
	}
	for key, val := range b.Context {
		switch {
		case key == "reviewed_by" && containsAgent(val, ag):
			return true
		case key == "consulted_agents" && containsAgent(val, ag):
			return true
		case key == "meeting_participants" && containsAgent(val, ag):
			return true
		case key == "voters" && containsAgent(val, ag):
			return true
		}
	}
	return false
}

func containsAgent(csv string, ag *models.Agent) bool {
	if strings.Contains(csv, ag.ID) {
		return true
	}
	if ag.Name != "" && strings.Contains(csv, ag.Name) {
		return true
	}
	return false
}

// scoreToGrade converts a 0-100 composite score to a letter grade.
func scoreToGrade(score float64) string {
	switch {
	case score >= 80:
		return "A"
	case score >= 65:
		return "B"
	case score >= 45:
		return "C"
	case score >= 25:
		return "D"
	default:
		return "F"
	}
}

func (rm *ReviewManager) consecutiveLowCount(agentID string) int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	history := rm.reviews[agentID]
	count := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Grade == "D" || history[i].Grade == "F" {
			count++
		} else {
			break
		}
	}
	return count
}

// triggerSelfOptimization lets an agent rewrite its own MOTIVATION.md or
// PERSONALITY.md. SKILL.md stays fixed — capabilities don't change, but
// approach and communication style can.
func (rm *ReviewManager) triggerSelfOptimization(ctx context.Context, ag *models.Agent) {
	_ = ctx
	if rm.personaManager == nil || ag.PersonaName == "" {
		return
	}

	rm.mu.RLock()
	history := rm.reviews[ag.ID]
	rm.mu.RUnlock()

	var grades []string
	for _, r := range history {
		grades = append(grades, fmt.Sprintf("%s(%.0f)", r.Grade, r.WeightedScore))
	}

	log.Printf("[Reviews] Self-optimization triggered for %s (%s). History: %v",
		ag.Name, ag.PersonaName, grades)

	if p, err := rm.personaManager.LoadPersona(ag.PersonaName); err == nil {
		p.SelfOptimized = true
	}
}

func (rm *ReviewManager) fireAgent(ctx context.Context, ag *models.Agent, review *AgentReview) {
	_ = ctx

	reason := fmt.Sprintf("Agent %s (%s) terminated after %d consecutive poor reviews (last: %s, score: %.0f/100). %s",
		ag.Name, ag.PersonaName, fireThreshold+1, review.Grade, review.WeightedScore, review.Breakdown)

	rm.mu.RLock()
	esc := rm.ceoEscalator
	rm.mu.RUnlock()

	if esc != nil {
		_ = esc.EscalateBeadToCEO("", reason, ag.ID)
	}

	if rm.agentManager != nil {
		_ = rm.agentManager.UpdateAgentStatus(ag.ID, "fired")
	}

	log.Printf("[Reviews] FIRED: %s", reason)
}

func (rm *ReviewManager) GetReviews(agentID string) []*AgentReview {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.reviews[agentID]
}

func (rm *ReviewManager) GetAllReviews() []*AgentReview {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var latest []*AgentReview
	for _, history := range rm.reviews {
		if len(history) > 0 {
			latest = append(latest, history[len(history)-1])
		}
	}
	return latest
}
