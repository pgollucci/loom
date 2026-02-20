package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/pkg/messages"
)

// PDABus combines publishing and subscription interfaces needed by the PDA orchestrator.
type PDABus interface {
	PublishTask(ctx context.Context, projectID string, task *messages.TaskMessage) error
	PublishTaskForRole(ctx context.Context, projectID, role string, task *messages.TaskMessage) error
	PublishPlan(ctx context.Context, projectID string, plan *messages.PlanMessage) error
	PublishEvent(ctx context.Context, eventType string, event *messages.EventMessage) error
	SubscribeResults(handler func(*messages.ResultMessage)) error
}

// PDAOrchestrator implements the Plan/Document/Act cycle.
// For each incoming work item it:
//  1. Plans: Consults an LLM to produce a structured plan (roles, sequence, steps).
//  2. Documents: Writes the plan as context and creates sub-beads.
//  3. Acts: Dispatches sub-beads to specialized agent services, tracks results,
//     and automatically gates code changes through review and QA before closing.
type PDAOrchestrator struct {
	bus           PDABus
	planner       Planner
	beadCreator   BeadCreator
	beadUpdater   BeadUpdater

	activePlans   map[string]*ActivePlan // planID -> plan
	mu            sync.RWMutex
	cancel        context.CancelFunc
}

// Planner generates structured plans from a bead description using an LLM.
type Planner interface {
	GeneratePlan(ctx context.Context, req PlanRequest) (*messages.PlanData, error)
}

// BeadCreator can create sub-beads for plan steps
type BeadCreator interface {
	CreateBead(projectID, title, description, beadType string, priority int, tags []string, parentID string) (string, error)
}

// BeadUpdater can update beads with context or status changes
type BeadUpdater interface {
	UpdateBead(id string, updates map[string]interface{}) error
}

// PlanRequest is the input to the planner
type PlanRequest struct {
	ProjectID   string
	BeadID      string
	Title       string
	Description string
	Context     map[string]interface{}
}

// ActivePlan tracks a plan that is being executed
type ActivePlan struct {
	PlanID        string
	ProjectID     string
	SourceBeadID  string
	Plan          messages.PlanData
	StepBeads     map[string]string // stepID -> beadID
	StepStatus    map[string]string // stepID -> "pending", "in_progress", "completed", "failed"
	CreatedAt     time.Time
	CorrelationID string
}

// NewPDAOrchestrator creates a new Plan/Document/Act orchestrator
func NewPDAOrchestrator(bus PDABus, planner Planner, beadCreator BeadCreator, beadUpdater BeadUpdater) *PDAOrchestrator {
	return &PDAOrchestrator{
		bus:         bus,
		planner:     planner,
		beadCreator: beadCreator,
		beadUpdater: beadUpdater,
		activePlans: make(map[string]*ActivePlan),
	}
}

// Start begins listening for results and driving the PDA cycle.
func (o *PDAOrchestrator) Start(ctx context.Context) error {
	ctx, o.cancel = context.WithCancel(ctx)

	// Subscribe to results to drive the Act cycle
	if err := o.bus.SubscribeResults(func(result *messages.ResultMessage) {
		o.handleResult(ctx, result)
	}); err != nil {
		return fmt.Errorf("failed to subscribe to results: %w", err)
	}

	log.Printf("[PDA] Orchestrator started")
	return nil
}

// ExecutePDA runs the full Plan/Document/Act cycle for a bead.
func (o *PDAOrchestrator) ExecutePDA(ctx context.Context, req PlanRequest) error {
	// Phase 1: PLAN
	log.Printf("[PDA] Planning for bead %s: %s", req.BeadID, req.Title)
	planData, err := o.planner.GeneratePlan(ctx, req)
	if err != nil {
		return fmt.Errorf("planning failed: %w", err)
	}

	planID := uuid.New().String()
	correlationID := uuid.New().String()

	activePlan := &ActivePlan{
		PlanID:        planID,
		ProjectID:     req.ProjectID,
		SourceBeadID:  req.BeadID,
		Plan:          *planData,
		StepBeads:     make(map[string]string),
		StepStatus:    make(map[string]string),
		CreatedAt:     time.Now(),
		CorrelationID: correlationID,
	}

	// Phase 2: DOCUMENT -- create sub-beads for each plan step
	log.Printf("[PDA] Documenting plan %s with %d steps", planID, len(planData.Steps))
	for i := range planData.Steps {
		step := &planData.Steps[i]
		step.Status = "pending"
		activePlan.StepStatus[step.StepID] = "pending"

		beadType := "task"
		if step.Action == "review" {
			beadType = "review"
		} else if step.Action == "test" {
			beadType = "test"
		}

		tags := []string{"pda", "plan:" + planID, "role:" + step.Role, "action:" + step.Action}
		beadID, err := o.beadCreator.CreateBead(
			req.ProjectID,
			fmt.Sprintf("[%s] %s", step.Role, step.Description),
			step.Description,
			beadType,
			planData.Priority,
			tags,
			req.BeadID,
		)
		if err != nil {
			log.Printf("[PDA] Failed to create bead for step %s: %v", step.StepID, err)
			continue
		}

		activePlan.StepBeads[step.StepID] = beadID
		log.Printf("[PDA] Created bead %s for step %s (role=%s, action=%s)", beadID, step.StepID, step.Role, step.Action)
	}

	o.mu.Lock()
	o.activePlans[planID] = activePlan
	o.mu.Unlock()

	// Document the plan on the source bead
	if err := o.beadUpdater.UpdateBead(req.BeadID, map[string]interface{}{
		"context": map[string]string{
			"pda_plan_id":    planID,
			"pda_status":     "in_progress",
			"pda_steps":      fmt.Sprintf("%d", len(planData.Steps)),
			"correlation_id": correlationID,
		},
	}); err != nil {
		log.Printf("[PDA] Warning: Failed to update source bead %s with plan: %v", req.BeadID, err)
	}

	// Publish plan to NATS
	planMsg := messages.NewPlanCreated(req.ProjectID, req.BeadID, planID, "pda-orchestrator", *planData, correlationID)
	if err := o.bus.PublishPlan(ctx, req.ProjectID, planMsg); err != nil {
		log.Printf("[PDA] Warning: Failed to publish plan to NATS: %v", err)
	}

	// Phase 3: ACT -- dispatch the first ready steps
	o.dispatchReadySteps(ctx, activePlan)

	return nil
}

// dispatchReadySteps finds plan steps whose dependencies are met and dispatches them.
func (o *PDAOrchestrator) dispatchReadySteps(ctx context.Context, plan *ActivePlan) {
	for _, step := range plan.Plan.Steps {
		if plan.StepStatus[step.StepID] != "pending" {
			continue
		}

		// Check dependencies
		allDepsComplete := true
		for _, dep := range step.DependsOn {
			if plan.StepStatus[dep] != "completed" {
				allDepsComplete = false
				break
			}
		}

		if !allDepsComplete {
			continue
		}

		beadID, ok := plan.StepBeads[step.StepID]
		if !ok {
			continue
		}

		// Dispatch to the appropriate role via NATS
		taskMsg := messages.TaskAssigned(
			plan.ProjectID,
			beadID,
			"", // Let the role-specific consumer pick it up
			messages.TaskData{
				Title:       step.Description,
				Description: step.Description,
				Priority:    plan.Plan.Priority,
				Type:        step.Action,
				Context:     step.Context,
			},
			plan.CorrelationID,
		)

		var err error
		if step.Role != "" {
			err = o.bus.PublishTaskForRole(ctx, plan.ProjectID, step.Role, taskMsg)
		} else {
			err = o.bus.PublishTask(ctx, plan.ProjectID, taskMsg)
		}

		if err != nil {
			log.Printf("[PDA] Failed to dispatch step %s: %v", step.StepID, err)
			continue
		}

		plan.StepStatus[step.StepID] = "in_progress"
		log.Printf("[PDA] Dispatched step %s (role=%s) as bead %s", step.StepID, step.Role, beadID)
	}
}

// handleResult processes a completed task and drives the plan forward.
func (o *PDAOrchestrator) handleResult(ctx context.Context, result *messages.ResultMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Find which plan this result belongs to
	for _, plan := range o.activePlans {
		for stepID, beadID := range plan.StepBeads {
			if beadID != result.BeadID {
				continue
			}

			switch result.Result.Status {
			case "success":
				plan.StepStatus[stepID] = "completed"
				log.Printf("[PDA] Step %s completed for plan %s", stepID, plan.PlanID)

				// Find the completed step to check if it produced code changes
				for _, step := range plan.Plan.Steps {
					if step.StepID == stepID && step.Action == "implement" {
						o.createReviewGate(ctx, plan, stepID, result)
					}
				}

			case "failure":
				plan.StepStatus[stepID] = "failed"
				log.Printf("[PDA] Step %s failed for plan %s: %s", stepID, plan.PlanID, result.Result.Error)
			}

			// Check if the plan is complete
			if o.isPlanComplete(plan) {
				o.completePlan(ctx, plan)
				return
			}

			// Dispatch any newly unblocked steps
			o.dispatchReadySteps(ctx, plan)
			return
		}
	}
}

// createReviewGate automatically creates a review step after code changes.
func (o *PDAOrchestrator) createReviewGate(ctx context.Context, plan *ActivePlan, afterStepID string, codeResult *messages.ResultMessage) {
	reviewStepID := "review-after-" + afterStepID
	if _, exists := plan.StepStatus[reviewStepID]; exists {
		return
	}

	tags := []string{"pda", "plan:" + plan.PlanID, "role:reviewer", "action:review", "auto-gate"}
	beadID, err := o.beadCreator.CreateBead(
		plan.ProjectID,
		fmt.Sprintf("[reviewer] Review changes from step %s", afterStepID),
		fmt.Sprintf("Review the code changes. Commits: %v", codeResult.Result.Commits),
		"review",
		plan.Plan.Priority,
		tags,
		plan.SourceBeadID,
	)
	if err != nil {
		log.Printf("[PDA] Failed to create review gate bead: %v", err)
		return
	}

	plan.StepBeads[reviewStepID] = beadID
	plan.StepStatus[reviewStepID] = "pending"

	// Also create a QA gate that depends on the review
	qaStepID := "qa-after-" + reviewStepID
	qaTags := []string{"pda", "plan:" + plan.PlanID, "role:qa", "action:test", "auto-gate"}
	qaBeadID, err := o.beadCreator.CreateBead(
		plan.ProjectID,
		fmt.Sprintf("[qa] Test changes from step %s", afterStepID),
		fmt.Sprintf("Build and test the codebase after code changes. Commits: %v", codeResult.Result.Commits),
		"test",
		plan.Plan.Priority,
		qaTags,
		plan.SourceBeadID,
	)
	if err != nil {
		log.Printf("[PDA] Failed to create QA gate bead: %v", err)
	} else {
		plan.StepBeads[qaStepID] = qaBeadID
		plan.StepStatus[qaStepID] = "pending"
		// QA depends on review passing
		plan.Plan.Steps = append(plan.Plan.Steps, messages.PlanStep{
			StepID:    qaStepID,
			Role:      "qa",
			Action:    "test",
			DependsOn: []string{reviewStepID},
			Status:    "pending",
		})
	}

	// Add the review step to the plan
	plan.Plan.Steps = append(plan.Plan.Steps, messages.PlanStep{
		StepID:    reviewStepID,
		Role:      "reviewer",
		Action:    "review",
		DependsOn: []string{afterStepID},
		Status:    "pending",
	})

	// Dispatch review immediately since its dependency (afterStepID) just completed
	reviewTask := messages.TaskAssigned(
		plan.ProjectID,
		beadID,
		"",
		messages.TaskData{
			Title:       fmt.Sprintf("Review changes from step %s", afterStepID),
			Description: fmt.Sprintf("Review the code changes. Commits: %v", codeResult.Result.Commits),
			Priority:    plan.Plan.Priority,
			Type:        "review",
			Context: map[string]interface{}{
				"commits":      codeResult.Result.Commits,
				"artifacts":    codeResult.Result.Artifacts,
				"source_step":  afterStepID,
			},
		},
		plan.CorrelationID,
	)

	if err := o.bus.PublishTaskForRole(ctx, plan.ProjectID, "reviewer", reviewTask); err != nil {
		log.Printf("[PDA] Failed to dispatch review gate: %v", err)
	} else {
		plan.StepStatus[reviewStepID] = "in_progress"
		log.Printf("[PDA] Auto-created review gate %s -> bead %s", reviewStepID, beadID)
	}
}

// isPlanComplete checks if all plan steps are completed (or failed).
func (o *PDAOrchestrator) isPlanComplete(plan *ActivePlan) bool {
	for _, status := range plan.StepStatus {
		if status == "pending" || status == "in_progress" {
			return false
		}
	}
	return true
}

// completePlan marks the source bead as done and cleans up.
func (o *PDAOrchestrator) completePlan(ctx context.Context, plan *ActivePlan) {
	allSuccess := true
	for _, status := range plan.StepStatus {
		if status == "failed" {
			allSuccess = false
			break
		}
	}

	pdaStatus := "completed"
	if !allSuccess {
		pdaStatus = "completed_with_failures"
	}

	if err := o.beadUpdater.UpdateBead(plan.SourceBeadID, map[string]interface{}{
		"context": map[string]string{
			"pda_status": pdaStatus,
		},
	}); err != nil {
		log.Printf("[PDA] Warning: Failed to update source bead %s: %v", plan.SourceBeadID, err)
	}

	// Publish completion event
	completionEvent := &messages.EventMessage{
		Type:      "pda.plan.completed",
		Source:    "pda-orchestrator",
		ProjectID: plan.ProjectID,
		EntityID:  plan.PlanID,
		Event: messages.EventData{
			Action:   "completed",
			Category: "pda",
			Data: map[string]interface{}{
				"plan_id":  plan.PlanID,
				"status":   pdaStatus,
				"bead_id":  plan.SourceBeadID,
			},
		},
		CorrelationID: plan.CorrelationID,
		Timestamp:     time.Now(),
	}
	if err := o.bus.PublishEvent(ctx, "pda.plan.completed", completionEvent); err != nil {
		log.Printf("[PDA] Warning: Failed to publish plan completion event: %v", err)
	}

	delete(o.activePlans, plan.PlanID)
	log.Printf("[PDA] Plan %s %s (source bead: %s)", plan.PlanID, pdaStatus, plan.SourceBeadID)
}

// ActivePlanCount returns the number of active plans
func (o *PDAOrchestrator) ActivePlanCount() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.activePlans)
}

// Close shuts down the orchestrator
func (o *PDAOrchestrator) Close() {
	if o.cancel != nil {
		o.cancel()
	}
	log.Printf("[PDA] Orchestrator stopped")
}
