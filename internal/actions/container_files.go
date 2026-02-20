package actions

import (
	"context"
	"fmt"
	"log"

	"github.com/jordanhubbard/loom/internal/containers"
)

// ContainerOrchestrator is the subset of containers.Orchestrator the Router needs.
type ContainerOrchestrator interface {
	GetProjectAgent(projectID string) (*containers.ProjectAgentClient, error)
}

// orchestratorAdapter wraps the real Orchestrator to satisfy ContainerOrchestrator.
type orchestratorAdapter struct {
	orch interface {
		GetAgent(projectID string) (containers.AgentClient, error)
	}
}

func (a *orchestratorAdapter) GetProjectAgent(projectID string) (*containers.ProjectAgentClient, error) {
	agent, err := a.orch.GetAgent(projectID)
	if err != nil {
		return nil, err
	}
	pac, ok := agent.(*containers.ProjectAgentClient)
	if !ok {
		return nil, fmt.Errorf("agent for project %s is not a ProjectAgentClient", projectID)
	}
	return pac, nil
}

// NewContainerOrchAdapter creates a ContainerOrchestrator from a containers.Orchestrator.
func NewContainerOrchAdapter(orch interface {
	GetAgent(projectID string) (containers.AgentClient, error)
}) ContainerOrchestrator {
	return &orchestratorAdapter{orch: orch}
}

// getContainerAgentRaw returns the ProjectAgentClient for a project if it uses
// containers, or nil if it doesn't. Does NOT run environment initialisation.
func (r *Router) getContainerAgentRaw(projectID string) *containers.ProjectAgentClient {
	if r.ContainerOrch == nil || r.Projects == nil || projectID == "" {
		return nil
	}
	project, err := r.Projects.GetProject(projectID)
	if err != nil || project == nil || !project.UseContainer {
		return nil
	}
	agent, err := r.ContainerOrch.GetProjectAgent(projectID)
	if err != nil {
		log.Printf("[Router] Container agent unavailable for %s: %v", projectID, err)
		return nil
	}
	return agent
}

// GetContainerAgent returns the ProjectAgentClient for a project and ensures
// its environment has been initialised (dependencies installed). Every
// container interaction — file, git, shell — should go through this method
// so the LLM-driven env bootstrap runs exactly once per project.
func (r *Router) GetContainerAgent(projectID string) *containers.ProjectAgentClient {
	agent := r.getContainerAgentRaw(projectID)
	if agent == nil {
		return nil
	}
	if r.BuildEnv != nil {
		if err := r.BuildEnv.EnsureReady(context.Background(), projectID, agent); err != nil {
			log.Printf("[Router] env init for %s failed (non-fatal): %v", projectID, err)
		}
	}
	return agent
}

// GetReadyContainerAgent is like GetContainerAgent but accepts a context for
// cancellation propagation. Prefer this variant inside action handlers.
func (r *Router) GetReadyContainerAgent(ctx context.Context, projectID string) *containers.ProjectAgentClient {
	agent := r.getContainerAgentRaw(projectID)
	if agent == nil {
		return nil
	}
	if r.BuildEnv != nil {
		if err := r.BuildEnv.EnsureReady(ctx, projectID, agent); err != nil {
			log.Printf("[Router] env init for %s failed (non-fatal): %v", projectID, err)
		}
	}
	return agent
}

// containerWriteFile writes a file via the project container.
func (r *Router) containerWriteFile(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	res, err := agent.WriteFile(ctx, action.Path, action.Content)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "file written",
		Metadata: map[string]interface{}{
			"path":          res.Path,
			"bytes_written": res.BytesWritten,
		},
	}
}

// containerReadFile reads a file via the project container.
func (r *Router) containerReadFile(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	res, err := agent.ReadFile(ctx, action.Path)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "file read",
		Metadata: map[string]interface{}{
			"path":    res.Path,
			"content": res.Content,
			"size":    res.Size,
		},
	}
}

// containerReadTree returns a directory listing via the project container.
func (r *Router) containerReadTree(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	path := action.Path
	if path == "" {
		path = "."
	}
	res, err := agent.ReadTree(ctx, path, 4)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "tree read",
		Metadata: map[string]interface{}{
			"path":    res.Path,
			"entries": res.Entries,
			"count":   res.Count,
		},
	}
}

// containerSearchText searches files via the project container.
func (r *Router) containerSearchText(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	res, err := agent.SearchFiles(ctx, action.Query, "", 100)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "search completed",
		Metadata: map[string]interface{}{
			"pattern": res.Pattern,
			"output":  res.Output,
		},
	}
}

// containerGitCommit commits changes via the project container.
func (r *Router) containerGitCommit(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}

	message := action.CommitMessage
	if message == "" {
		message = fmt.Sprintf("feat: Update from bead %s\n\nBead: %s\nAgent: %s",
			actx.BeadID, actx.BeadID, actx.AgentID)
	}

	res, err := agent.GitCommit(ctx, message, action.Files)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "commit created",
		Metadata: map[string]interface{}{
			"commit_sha": res.CommitSHA,
		},
	}
}

// containerGitPush pushes via the project container.
func (r *Router) containerGitPush(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	res, err := agent.GitPush(ctx, action.Branch, false)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "push completed",
		Metadata: map[string]interface{}{
			"success": res.Success,
			"output":  res.Output,
		},
	}
}

// containerGitStatus returns git status via the project container.
func (r *Router) containerGitStatus(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	res, err := agent.GitStatus(ctx)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "git status",
		Metadata: map[string]interface{}{
			"output": res.Status,
		},
	}
}

// containerGitDiff returns git diff via the project container.
func (r *Router) containerGitDiff(ctx context.Context, actx ActionContext, action Action) Result {
	agent := r.getContainerAgentRaw(actx.ProjectID)
	if agent == nil {
		return Result{ActionType: action.Type, Status: "error", Message: "container agent not available"}
	}
	res, err := agent.GitDiff(ctx)
	if err != nil {
		return Result{ActionType: action.Type, Status: "error", Message: err.Error()}
	}
	return Result{
		ActionType: action.Type,
		Status:     "executed",
		Message:    "git diff",
		Metadata: map[string]interface{}{
			"output": res.Unstaged + res.Staged,
		},
	}
}
