// Package taskexecutor provides a direct bead execution engine that bypasses
// Temporal, NATS, and the WorkerPool. It spawns goroutines per project that
// claim beads from the bead manager and run them through worker.ExecuteTaskWithLoop.
package taskexecutor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/actions"
	"github.com/jordanhubbard/loom/internal/beads"
	"github.com/jordanhubbard/loom/internal/database"
	"github.com/jordanhubbard/loom/internal/project"
	"github.com/jordanhubbard/loom/internal/provider"
	"github.com/jordanhubbard/loom/internal/worker"
	"github.com/jordanhubbard/loom/pkg/models"
)

const defaultNumWorkers = 3

// Executor is the direct bead execution engine.
type Executor struct {
	providerRegistry *provider.Registry
	beadManager      *beads.Manager
	actionRouter     *actions.Router
	projectManager   *project.Manager
	db               *database.Database
	lessonsProvider  worker.LessonsProvider
	numWorkers       int
	startedProjects  map[string]struct{}
	mu               sync.Mutex
}

// New creates an Executor.
func New(
	providerRegistry *provider.Registry,
	beadManager *beads.Manager,
	actionRouter *actions.Router,
	projectManager *project.Manager,
	db *database.Database,
) *Executor {
	return &Executor{
		providerRegistry: providerRegistry,
		beadManager:      beadManager,
		actionRouter:     actionRouter,
		projectManager:   projectManager,
		db:               db,
		numWorkers:       defaultNumWorkers,
		startedProjects:  make(map[string]struct{}),
	}
}

// SetLessonsProvider wires in the lessons provider for build failure learning.
func (e *Executor) SetLessonsProvider(lp worker.LessonsProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lessonsProvider = lp
}

// SetNumWorkers sets the number of concurrent worker goroutines per project.
func (e *Executor) SetNumWorkers(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.numWorkers = n
}

// Start spawns numWorkers goroutines for projectID. Idempotent.
func (e *Executor) Start(ctx context.Context, projectID string) {
	e.mu.Lock()
	if _, ok := e.startedProjects[projectID]; ok {
		e.mu.Unlock()
		return
	}
	e.startedProjects[projectID] = struct{}{}
	n := e.numWorkers
	e.mu.Unlock()

	log.Printf("[TaskExecutor] Starting %d worker(s) for project %s", n, projectID)
	for i := 0; i < n; i++ {
		go e.workerLoop(ctx, projectID)
	}
}

// workerLoop repeatedly claims and executes beads for a project.
func (e *Executor) workerLoop(ctx context.Context, projectID string) {
	workerID := fmt.Sprintf("exec-%s-%s", projectID, uuid.New().String()[:8])
	log.Printf("[TaskExecutor] Worker %s started for project %s", workerID, projectID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[TaskExecutor] Worker %s stopping: context cancelled", workerID)
			return
		default:
		}

		bead := e.claimNextBead(ctx, projectID, workerID)
		if bead == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		log.Printf("[TaskExecutor] Worker %s claimed bead %s (%s)", workerID, bead.ID, bead.Title)
		e.executeBead(ctx, bead, workerID)
	}
}

// claimNextBead returns the next available bead for the project, or nil.
func (e *Executor) claimNextBead(ctx context.Context, projectID, workerID string) *models.Bead {
	_ = ctx // reserved for future use
	readyBeads, err := e.beadManager.GetReadyBeads(projectID)
	if err != nil {
		log.Printf("[TaskExecutor] GetReadyBeads(%s) error: %v", projectID, err)
		return nil
	}

	for _, b := range readyBeads {
		if b == nil {
			continue
		}
		// Skip decision beads — they require human input
		if b.Type == "decision" {
			continue
		}
		// Skip in-progress beads that already have an assigned agent
		if b.Status == models.BeadStatusInProgress && b.AssignedTo != "" {
			continue
		}
		// Try to claim; another worker goroutine may win the race
		if err := e.beadManager.ClaimBead(b.ID, workerID); err != nil {
			continue
		}
		return b
	}
	return nil
}

// executeBead runs the full action loop for a claimed bead.
func (e *Executor) executeBead(ctx context.Context, bead *models.Bead, workerID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[TaskExecutor] PANIC for bead %s: %v", bead.ID, r)
			_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
				"status":      models.BeadStatusOpen,
				"assigned_to": "",
			})
		}
	}()

	providers := e.providerRegistry.ListActive()
	if len(providers) == 0 {
		log.Printf("[TaskExecutor] No active providers, releasing bead %s", bead.ID)
		_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
		})
		return
	}
	prov := providers[0]

	personaName := personaForBead(bead)
	agent := &models.Agent{
		ID:          workerID,
		Name:        personaName,
		PersonaName: personaName,
		ProjectID:   bead.ProjectID,
		ProviderID:  prov.Config.ID,
		Status:      "working",
		Persona: &models.Persona{
			Name:      personaName,
			Character: personas[personaName],
		},
	}

	w := worker.NewWorker(workerID, agent, prov)
	if e.db != nil {
		w.SetDatabase(e.db)
	}

	var proj *models.Project
	if e.projectManager != nil {
		proj, _ = e.projectManager.GetProject(bead.ProjectID)
	}

	task := &worker.Task{
		ID:          fmt.Sprintf("task-%s-%d", bead.ID, time.Now().UnixNano()),
		Description: buildBeadDescription(bead),
		Context:     buildBeadContext(bead, proj),
		BeadID:      bead.ID,
		ProjectID:   bead.ProjectID,
	}

	loopConfig := &worker.LoopConfig{
		MaxIterations: 100,
		Router:        e.actionRouter,
		ActionContext: actions.ActionContext{
			AgentID:   workerID,
			BeadID:    bead.ID,
			ProjectID: bead.ProjectID,
		},
		LessonsProvider: e.lessonsProvider,
		DB:              e.db,
		TextMode:        !isFullModeCapable(prov),
		OnProgress: func() {
			_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
				"updated_at": time.Now().UTC(),
			})
		},
	}

	result, err := w.ExecuteTaskWithLoop(ctx, task, loopConfig)
	if err != nil {
		log.Printf("[TaskExecutor] ExecuteTaskWithLoop error for bead %s: %v", bead.ID, err)
		_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
		})
		return
	}

	log.Printf("[TaskExecutor] Bead %s finished: %s (%d iterations)",
		bead.ID, result.TerminalReason, result.Iterations)

	// If not closed by a done/close_bead action, reset to open for retry
	if result.TerminalReason != "completed" {
		_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
		})
	}
}

// personaForBead picks a persona name based on bead tags.
func personaForBead(bead *models.Bead) string {
	for _, tag := range bead.Tags {
		switch strings.ToLower(tag) {
		case "devops", "infra", "infrastructure":
			return "devops"
		case "review", "pr", "code-review":
			return "review"
		case "qa", "test", "testing":
			return "qa"
		case "docs", "documentation":
			return "docs"
		}
	}
	return "engineering-manager"
}

// buildBeadDescription formats a bead as a task description for the LLM.
func buildBeadDescription(bead *models.Bead) string {
	return fmt.Sprintf("Work on bead %s: %s\n\n%s", bead.ID, bead.Title, bead.Description)
}

// buildBeadContext builds the context string for a bead, including project info.
func buildBeadContext(bead *models.Bead, proj *models.Project) string {
	var sb strings.Builder

	if proj != nil {
		sb.WriteString(fmt.Sprintf("Project: %s (%s)\nBranch: %s\n", proj.Name, proj.ID, proj.Branch))
		if len(proj.Context) > 0 {
			for k, v := range proj.Context {
				sb.WriteString(fmt.Sprintf("%s: %s\n", k, v))
			}
		}
		sb.WriteString("\n")

		workDir := proj.WorkDir
		if workDir == "" {
			workDir = filepath.Join("data", "projects", proj.ID)
		}
		if agentsMD := readProjectFile(workDir, "AGENTS.md", 4000); agentsMD != "" {
			sb.WriteString("## Project Instructions (AGENTS.md)\n\n")
			sb.WriteString(agentsMD)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString(fmt.Sprintf("Bead: %s (P%d %s)\n", bead.ID, bead.Priority, bead.Type))
	if len(bead.Context) > 0 {
		for k, v := range bead.Context {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}

	sb.WriteString(`
## Instructions

You are an autonomous coding agent. Your job is to MAKE CHANGES, COMMIT, and PUSH.

WORKFLOW:
1. Locate: scope + read relevant files (iterations 1-3)
2. Change: edit or write files (iterations 4-15)
3. Verify: build and test (iterations 16-18)
4. Land: git_commit, git_push, done (iterations 19-21)

CRITICAL RULES:
- You have 100 iterations. Use them.
- ALWAYS git_commit after making changes.
- ALWAYS git_push after committing.
- Use the 'done' action when the task is complete.
`)

	return sb.String()
}

// readProjectFile reads a file from a project work directory, capped at maxLen bytes.
func readProjectFile(workDir, filename string, maxLen int) string {
	path := filepath.Join(workDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > maxLen {
		content = content[:maxLen] + "\n... (truncated)"
	}
	return content
}

// isFullModeCapable returns true for frontier/large models that support
// the full 60+ action JSON schema. Small/local models use text mode (14 actions).
func isFullModeCapable(prov *provider.RegisteredProvider) bool {
	if prov == nil || prov.Config == nil {
		return false
	}
	name := strings.ToLower(prov.Config.SelectedModel)
	if name == "" {
		name = strings.ToLower(prov.Config.Model)
	}
	for _, prefix := range []string{
		"claude", "anthropic/claude",
		"gpt-4", "gpt-5", "o1", "o3", "o4",
		"gemini-pro", "gemini-1.5", "gemini-2",
	} {
		if strings.HasPrefix(name, prefix) || strings.Contains(name, "/"+prefix) {
			return true
		}
	}
	// Large context window as proxy for frontier model capability
	return prov.Config.ContextWindow > 32768
}
