// Package taskexecutor provides a direct bead execution engine that bypasses
// Temporal, NATS, and the WorkerPool. It spawns goroutines per project that
// claim beads from the bead manager and run them through worker.ExecuteTaskWithLoop.
package taskexecutor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/actions"
	"github.com/jordanhubbard/loom/internal/beads"
	"github.com/jordanhubbard/loom/internal/database"
	"github.com/jordanhubbard/loom/internal/dispatch"
	"github.com/jordanhubbard/loom/internal/project"
	"github.com/jordanhubbard/loom/internal/provider"
	"github.com/jordanhubbard/loom/internal/worker"
	"github.com/jordanhubbard/loom/pkg/models"
)

const (
	defaultNumWorkers = 3
	// maxIdleRounds: after this many consecutive nil-claim rounds (each 5s),
	// a worker goroutine exits. 36 × 5s = 3 minutes of idleness.
	maxIdleRounds = 36
	// watcherInterval: how often the watcher checks for new work when idle.
	watcherInterval = 30 * time.Second
	// gitFetchInterval: how often the watcher does a git fetch to detect
	// beads pushed from external sources.
	gitFetchInterval = 5 * time.Minute
	// zombieBeadThreshold: if an in_progress bead with an ephemeral exec-*
	// assignment has not been updated in this long, its executor goroutine
	// is considered dead and the bead is reclaimed.
	zombieBeadThreshold = 30 * time.Minute
	// providerErrorBackoff: how long a worker pauses after a provider error
	// (502, 429, context canceled) before claiming the next bead. Prevents
	// hot-spin loops that exhaust the tokenhub rate limit (60 RPS / IP).
	providerErrorBackoff = 3 * time.Second
)

// projectState tracks per-project executor state.
type projectState struct {
	activeWorkers  int
	watcherRunning bool
	// wakeCh is sent on to immediately unblock a sleeping watcher.
	wakeCh chan struct{}
}

// Executor is the direct bead execution engine.
type Executor struct {
	providerRegistry *provider.Registry
	beadManager      *beads.Manager
	actionRouter     *actions.Router
	projectManager   *project.Manager
	db               *database.Database
	lessonsProvider  worker.LessonsProvider
	numWorkers       int
	projectStates    map[string]*projectState
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
		projectStates:    make(map[string]*projectState),
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

// Start ensures the watcher is running and spawns workers for projectID.
// Safe to call multiple times; spawns workers only when none are active.
func (e *Executor) Start(ctx context.Context, projectID string) {
	e.mu.Lock()
	state := e.getOrCreateState(projectID)
	n := e.numWorkers

	// Start the long-lived watcher if not already running
	if !state.watcherRunning {
		state.watcherRunning = true
		e.mu.Unlock()
		go e.watcherLoop(ctx, projectID)
	} else {
		e.mu.Unlock()
	}

	// Spawn workers up to numWorkers
	e.mu.Lock()
	toSpawn := n - state.activeWorkers
	state.activeWorkers += toSpawn
	e.mu.Unlock()

	if toSpawn > 0 {
		log.Printf("[TaskExecutor] Spawning %d worker(s) for project %s", toSpawn, projectID)
		for i := 0; i < toSpawn; i++ {
			go e.workerLoop(ctx, projectID)
		}
	}
}

// WakeProject signals that new work may be available, spawning workers if idle.
func (e *Executor) WakeProject(projectID string) {
	e.mu.Lock()
	state := e.getOrCreateState(projectID)
	ch := state.wakeCh
	e.mu.Unlock()

	// Non-blocking send: watcher may already be awake
	select {
	case ch <- struct{}{}:
	default:
	}
}

// getOrCreateState returns the projectState for projectID, creating it if needed.
// Caller must hold e.mu.
func (e *Executor) getOrCreateState(projectID string) *projectState {
	if s, ok := e.projectStates[projectID]; ok {
		return s
	}
	s := &projectState{
		wakeCh: make(chan struct{}, 1),
	}
	e.projectStates[projectID] = s
	return s
}

// workerLoop claims and executes beads. Exits after maxIdleRounds of no work.
func (e *Executor) workerLoop(ctx context.Context, projectID string) {
	workerID := fmt.Sprintf("exec-%s-%s", projectID, uuid.New().String()[:8])
	log.Printf("[TaskExecutor] Worker %s started for project %s", workerID, projectID)

	idleRounds := 0
	defer func() {
		e.mu.Lock()
		if s, ok := e.projectStates[projectID]; ok {
			s.activeWorkers--
			if s.activeWorkers < 0 {
				s.activeWorkers = 0
			}
		}
		e.mu.Unlock()
		log.Printf("[TaskExecutor] Worker %s exiting (idle=%d)", workerID, idleRounds)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		bead := e.claimNextBead(ctx, projectID, workerID)
		if bead == nil {
			idleRounds++
			if idleRounds >= maxIdleRounds {
				log.Printf("[TaskExecutor] Worker %s idle for %ds, going to sleep",
					workerID, idleRounds*5)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		idleRounds = 0
		log.Printf("[TaskExecutor] Worker %s claimed bead %s (%s)", workerID, bead.ID, bead.Title)
		if needsBackoff := e.executeBead(ctx, bead, workerID); needsBackoff {
			// Provider error (502, 429, context canceled): pause before
			// claiming the next bead to avoid hammering tokenhub rate limits.
			select {
			case <-ctx.Done():
				return
			case <-time.After(providerErrorBackoff):
			}
		}
	}
}

// watcherLoop runs forever for a project. It wakes workers when new beads arrive,
// either from the API (via WakeProject) or from a periodic git fetch.
func (e *Executor) watcherLoop(ctx context.Context, projectID string) {
	log.Printf("[TaskExecutor] Watcher started for project %s", projectID)
	defer log.Printf("[TaskExecutor] Watcher stopped for project %s", projectID)

	ticker := time.NewTicker(watcherInterval)
	defer ticker.Stop()
	gitTicker := time.NewTicker(gitFetchInterval)
	defer gitTicker.Stop()

	e.mu.Lock()
	state := e.getOrCreateState(projectID)
	wakeCh := state.wakeCh
	e.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return

		case <-wakeCh:
			// Immediate wake signal from API (new bead created or beads reloaded)
			e.maybeSpawnWorkers(ctx, projectID)

		case <-ticker.C:
			// Periodic check: any ready beads?
			e.maybeSpawnWorkers(ctx, projectID)

		case <-gitTicker.C:
			// Periodic git fetch to detect beads pushed externally
			e.fetchAndReloadBeads(ctx, projectID)
			e.maybeSpawnWorkers(ctx, projectID)
		}
	}
}

// maybeSpawnWorkers spawns workers for projectID if there is work and none are active.
func (e *Executor) maybeSpawnWorkers(ctx context.Context, projectID string) {
	readyBeads, err := e.beadManager.GetReadyBeads(projectID)
	if err != nil || len(readyBeads) == 0 {
		return
	}

	e.mu.Lock()
	state := e.getOrCreateState(projectID)
	n := e.numWorkers
	toSpawn := n - state.activeWorkers
	if toSpawn <= 0 {
		e.mu.Unlock()
		return
	}
	state.activeWorkers += toSpawn
	e.mu.Unlock()

	log.Printf("[TaskExecutor] Waking %d worker(s) for project %s (%d ready beads)",
		toSpawn, projectID, len(readyBeads))
	for i := 0; i < toSpawn; i++ {
		go e.workerLoop(ctx, projectID)
	}
}

// fetchAndReloadBeads does a git fetch on the project's beads worktree and reloads
// if the remote has new commits.
func (e *Executor) fetchAndReloadBeads(ctx context.Context, projectID string) {
	beadsPath := e.beadManager.GetProjectBeadsPath(projectID)
	if beadsPath == "" {
		return
	}
	// The beads path is the .beads directory inside the worktree.
	// Git operations run on the parent (the worktree root).
	worktreeRoot := filepath.Dir(beadsPath)

	// Get current HEAD before fetch
	headBefore, err := os.ReadFile(filepath.Join(worktreeRoot, ".git", "HEAD"))
	if err != nil {
		// Not a git worktree or no HEAD — skip
		return
	}

	// Fetch from origin (non-blocking: if it fails we skip gracefully)
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := fmt.Sprintf("cd %q && git fetch origin 2>/dev/null", worktreeRoot)
	if err := runShell(fetchCtx, cmd); err != nil {
		return
	}

	// Check FETCH_HEAD vs current HEAD
	fetchHead, err := os.ReadFile(filepath.Join(worktreeRoot, ".git", "FETCH_HEAD"))
	if err != nil {
		return
	}
	if strings.TrimSpace(string(headBefore)) == strings.TrimSpace(strings.Fields(string(fetchHead))[0]) {
		return // No new commits
	}

	// New commits: reset to FETCH_HEAD and reload
	resetCtx, cancel2 := context.WithTimeout(ctx, 30*time.Second)
	defer cancel2()
	if err := runShell(resetCtx, fmt.Sprintf("cd %q && git reset --hard FETCH_HEAD 2>/dev/null", worktreeRoot)); err != nil {
		log.Printf("[TaskExecutor] git reset failed for project %s: %v", projectID, err)
		return
	}

	log.Printf("[TaskExecutor] New beads detected for project %s, reloading", projectID)
	e.beadManager.ClearProjectBeads(projectID)
	if err := e.beadManager.LoadBeadsFromGit(ctx, projectID, beadsPath); err != nil {
		log.Printf("[TaskExecutor] reload failed for project %s: %v", projectID, err)
	}
}

// runShell executes a shell command, returning an error on non-zero exit.
func runShell(ctx context.Context, cmd string) error {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	return c.Run()
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
		// Rescue zombie in-progress beads. Ephemeral executor IDs (exec-<project>-<uuid>)
		// are created per goroutine and die without cleanup when loom restarts or the
		// goroutine is killed. If the bead has not been updated in zombieBeadThreshold,
		// the executor is gone and we reset it back to open so it can be reclaimed.
		if b.Status == models.BeadStatusInProgress && b.AssignedTo != "" {
			if strings.HasPrefix(b.AssignedTo, "exec-") && time.Since(b.UpdatedAt) > zombieBeadThreshold {
				log.Printf("[TaskExecutor] Reclaiming zombie bead %s (stale executor %s, age %v)",
					b.ID, b.AssignedTo, time.Since(b.UpdatedAt).Round(time.Second))
				_ = e.beadManager.UpdateBead(b.ID, map[string]interface{}{
					"status":      models.BeadStatusOpen,
					"assigned_to": "",
				})
				b.Status = models.BeadStatusOpen
				b.AssignedTo = ""
			} else {
				continue
			}
		}
		// Fix inconsistent state: open bead with stale assignment — reset it
		if b.Status == models.BeadStatusOpen && b.AssignedTo != "" {
			_ = e.beadManager.UpdateBead(b.ID, map[string]interface{}{
				"assigned_to": "",
			})
			b.AssignedTo = ""
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
// executeBead runs a bead through the worker loop. Returns true if the worker
// should back off before claiming the next bead (provider errors, rate limits).
func (e *Executor) executeBead(ctx context.Context, bead *models.Bead, workerID string) (needsBackoff bool) {
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
		return true // no providers = back off
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
		e.handleBeadError(bead, err)
		return true // provider error — caller should back off
	}

	log.Printf("[TaskExecutor] Bead %s finished: %s (%d iterations)",
		bead.ID, result.TerminalReason, result.Iterations)

	if result.TerminalReason == "completed" {
		// done/close_bead action signals success — explicitly mark closed
		_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
			"status":      models.BeadStatusClosed,
			"assigned_to": "",
		})
	} else {
		// Any non-successful terminal reason: reset to open for retry
		_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
		})
	}
	return false
}

// handleBeadError records the error in bead context and detects dispatch loops.
// Context-canceled errors (from loom shutdown) are silently reset.
// Repeated provider/infra errors trigger loop detection and eventual blocking.
func (e *Executor) handleBeadError(bead *models.Bead, execErr error) {
	// Context cancellations are from loom shutdown — silently reset, no history needed.
	if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
		_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
			"status":      models.BeadStatusOpen,
			"assigned_to": "",
		})
		return
	}

	// Reload the bead to get fresh context (dispatch_count, error_history).
	fresh, loadErr := e.beadManager.GetBead(bead.ID)
	if loadErr != nil || fresh == nil {
		fresh = bead
	}
	if fresh.Context == nil {
		fresh.Context = map[string]string{}
	}

	// Increment dispatch_count.
	dc := 0
	fmt.Sscanf(fresh.Context["dispatch_count"], "%d", &dc)
	dc++
	fresh.Context["dispatch_count"] = fmt.Sprintf("%d", dc)

	// Append to error_history (capped at 20 entries).
	type errRecord struct {
		Timestamp string `json:"timestamp"`
		Error     string `json:"error"`
		Dispatch  int    `json:"dispatch"`
	}
	var history []errRecord
	if raw := fresh.Context["error_history"]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &history)
	}
	history = append(history, errRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Error:     execErr.Error(),
		Dispatch:  dc,
	})
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	histBytes, _ := json.Marshal(history)
	fresh.Context["error_history"] = string(histBytes)
	fresh.Context["last_run_error"] = execErr.Error()
	fresh.Context["last_run_at"] = time.Now().UTC().Format(time.RFC3339)

	// Run loop detection on the updated bead context.
	ld := dispatch.NewLoopDetector()
	isStuck, loopReason := ld.IsStuckInLoop(fresh)

	ctxUpdate := map[string]string{
		"dispatch_count": fresh.Context["dispatch_count"],
		"error_history":  fresh.Context["error_history"],
		"last_run_error": fresh.Context["last_run_error"],
		"last_run_at":    fresh.Context["last_run_at"],
		"loop_detected":  fmt.Sprintf("%t", isStuck),
	}
	if isStuck {
		ctxUpdate["loop_detected_reason"] = loopReason
		ctxUpdate["loop_detected_at"] = time.Now().UTC().Format(time.RFC3339)
		log.Printf("[TaskExecutor] Loop detected for bead %s: %s", bead.ID, loopReason)
	}

	_ = e.beadManager.UpdateBead(bead.ID, map[string]interface{}{
		"status":      models.BeadStatusOpen,
		"assigned_to": "",
		"context":     ctxUpdate,
	})
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
