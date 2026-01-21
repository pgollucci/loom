package temporal

import (
	"context"
	"fmt"
	"time"
)

// DSLExecutor executes parsed Temporal DSL instructions
type DSLExecutor struct {
	manager *Manager
}

// NewDSLExecutor creates a new DSL executor
func NewDSLExecutor(manager *Manager) *DSLExecutor {
	return &DSLExecutor{
		manager: manager,
	}
}

// ExecuteInstructions executes a list of temporal instructions
func (e *DSLExecutor) ExecuteInstructions(ctx context.Context, instructions []TemporalInstruction, agentID string) *TemporalDSLExecution {
	execution := &TemporalDSLExecution{
		AgentID:      agentID,
		Instructions: instructions,
		Results:      []TemporalInstructionResult{},
		ExecutedAt:   time.Now(),
	}

	startTime := time.Now()

	for _, instr := range instructions {
		// Validate instruction
		if err := ValidateInstruction(instr); err != nil {
			execution.Results = append(execution.Results, TemporalInstructionResult{
				Instruction: instr,
				Success:     false,
				Error:       err.Error(),
				ExecutedAt:  time.Now(),
			})
			continue
		}

		// Execute instruction
		result := e.executeInstruction(ctx, instr, agentID)
		execution.Results = append(execution.Results, result)
	}

	execution.TotalDuration = time.Since(startTime)
	return execution
}

// executeInstruction routes to the appropriate executor based on instruction type
func (e *DSLExecutor) executeInstruction(ctx context.Context, instr TemporalInstruction, agentID string) TemporalInstructionResult {
	result := TemporalInstructionResult{
		Instruction: instr,
		ExecutedAt:  time.Now(),
	}

	startTime := time.Now()

	switch instr.Type {
	case InstructionTypeWorkflow:
		result.Result, result.Error = e.executeWorkflow(ctx, instr)
		result.Success = result.Error == ""

	case InstructionTypeSchedule:
		result.Result, result.Error = e.executeSchedule(ctx, instr)
		result.Success = result.Error == ""

	case InstructionTypeQuery:
		result.Result, result.Error = e.executeQuery(ctx, instr)
		result.Success = result.Error == ""

	case InstructionTypeSignal:
		result.Result, result.Error = e.executeSignal(ctx, instr)
		result.Success = result.Error == ""

	case InstructionTypeActivity:
		result.Result, result.Error = e.executeActivity(ctx, instr)
		result.Success = result.Error == ""

	case InstructionTypeCancelWF:
		result.Result, result.Error = e.executeCancel(ctx, instr)
		result.Success = result.Error == ""

	case InstructionTypeListWF:
		result.Result, result.Error = e.executeList(ctx, instr)
		result.Success = result.Error == ""

	default:
		result.Error = fmt.Sprintf("unknown instruction type: %s", instr.Type)
		result.Success = false
	}

	result.Duration = time.Since(startTime)
	return result
}

// executeWorkflow schedules a workflow execution
func (e *DSLExecutor) executeWorkflow(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	opts := WorkflowOptions{
		ID:             instr.Name + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:           instr.Name,
		Input:          instr.Input,
		Timeout:        instr.Timeout,
		Retry:          instr.Retry,
		Wait:           instr.Wait,
		Priority:       instr.Priority,
		IdempotencyKey: instr.IdempotencyKey,
	}

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}

	// Schedule the workflow
	workflowID, err := e.manager.ScheduleWorkflow(ctx, opts)
	if err != nil {
		return nil, fmt.Sprintf("failed to schedule workflow: %v", err)
	}

	result := map[string]interface{}{
		"workflow_id": workflowID,
		"scheduled":   true,
		"wait":        instr.Wait,
	}

	// If WAIT is true, try to get the result
	if instr.Wait {
		// Wait for workflow execution
		waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()

		runResult, err := e.manager.GetWorkflowResult(waitCtx, workflowID)
		if err != nil {
			result["wait_error"] = err.Error()
		} else {
			result["execution_result"] = runResult
		}
	}

	return result, ""
}

// executeSchedule creates a recurring schedule for a workflow
func (e *DSLExecutor) executeSchedule(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	opts := ScheduleOptions{
		Name:     instr.Name,
		Workflow: instr.Name,
		Input:    instr.Input,
		Interval: instr.Interval,
		Timeout:  instr.Timeout,
		Retry:    instr.Retry,
	}

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}

	scheduleID, err := e.manager.CreateSchedule(ctx, opts)
	if err != nil {
		return nil, fmt.Sprintf("failed to create schedule: %v", err)
	}

	return map[string]interface{}{
		"schedule_id": scheduleID,
		"interval":    instr.Interval.String(),
		"created":     true,
	}, ""
}

// executeQuery queries a running workflow
func (e *DSLExecutor) executeQuery(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	opts := QueryOptions{
		WorkflowID: instr.WorkflowID,
		RunID:      instr.RunID,
		QueryType:  instr.QueryType,
		Args:       []interface{}{},
	}

	result, err := e.manager.QueryWorkflow(ctx, opts)
	if err != nil {
		return nil, fmt.Sprintf("failed to query workflow: %v", err)
	}

	return result, ""
}

// executeSignal sends a signal to a running workflow
func (e *DSLExecutor) executeSignal(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	opts := SignalOptions{
		WorkflowID: instr.WorkflowID,
		RunID:      instr.RunID,
		Name:       instr.SignalName,
		Data:       instr.SignalData,
	}

	err := e.manager.SignalWorkflow(ctx, opts)
	if err != nil {
		return nil, fmt.Sprintf("failed to signal workflow: %v", err)
	}

	return map[string]interface{}{
		"workflow_id": instr.WorkflowID,
		"signal_sent": true,
		"signal_name": instr.SignalName,
	}, ""
}

// executeActivity executes an activity directly
func (e *DSLExecutor) executeActivity(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	opts := ActivityOptions{
		Name:    instr.Name,
		Input:   instr.Input,
		Timeout: instr.Timeout,
		Retry:   instr.Retry,
		Wait:    true, // Activities in DSL always wait for result
	}

	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}

	result, err := e.manager.ExecuteActivity(ctx, opts)
	if err != nil {
		return nil, fmt.Sprintf("failed to execute activity: %v", err)
	}

	return result, ""
}

// executeCancel cancels a running workflow
func (e *DSLExecutor) executeCancel(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	opts := CancelOptions{
		WorkflowID: instr.WorkflowID,
		RunID:      instr.RunID,
	}

	err := e.manager.CancelWorkflow(ctx, opts)
	if err != nil {
		return nil, fmt.Sprintf("failed to cancel workflow: %v", err)
	}

	return map[string]interface{}{
		"workflow_id": instr.WorkflowID,
		"cancelled":   true,
	}, ""
}

// executeList lists workflows
func (e *DSLExecutor) executeList(ctx context.Context, instr TemporalInstruction) (interface{}, string) {
	if e.manager == nil {
		return nil, "temporal manager not initialized"
	}

	workflows, err := e.manager.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Sprintf("failed to list workflows: %v", err)
	}

	return map[string]interface{}{
		"workflows_count": len(workflows),
		"workflows":       workflows,
	}, ""
}
