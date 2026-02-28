package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/internal/observability"
	"github.com/jordanhubbard/loom/pkg/config"
	"github.com/jordanhubbard/loom/pkg/models"
	"gopkg.in/yaml.v3"
)

// Manager integrates with the bd (beads) CLI tool and git-centric storage
type Manager struct {
	bdPath            string
	beadsPath         string
	backend           string // "sqlite", "dolt", or "yaml"
	mu                sync.RWMutex
	beads             map[string]*models.Bead
	beadFiles         map[string]string
	workGraph         *models.WorkGraph
	nextID            int               // For generating IDs when bd CLI is not available
	projectPrefixes   map[string]string // Project ID -> bead prefix (e.g., "loom" -> "bd")
	projectNextIDs    map[string]int    // Per-project next ID counter
	projectBeadsPaths map[string]string // Project ID -> beads worktree path (avoids last-writer-wins)

	// Git-centric storage fields (per-project)
	gitConfigs map[string]*GitConfig  // Project ID -> git configuration
	gitMu      sync.Mutex             // Protects gitLocks map
	gitLocks   map[string]*sync.Mutex // Per-project mutex to serialize git operations
}

// GitConfig stores git storage configuration for a project
type GitConfig struct {
	WorktreeManager interface{} // GitWorktreeManager interface
	BeadsBranch     string      // Branch name for beads (e.g., "beads-sync")
	UseGitStorage   bool        // Enable git commit/push for beads
	GitAuthMethod   string      // Auth method: "token", "ssh", "none"
	GitRepo         string      // Remote repo URL (needed for token auth)
}

// NewManager creates a new beads manager
func NewManager(bdPath string) *Manager {
	return &Manager{
		bdPath:    bdPath,
		beadsPath: ".beads",
		backend:   "sqlite", // Default to sqlite for simpler setup
		beads:     make(map[string]*models.Bead),
		beadFiles: make(map[string]string),
		workGraph: &models.WorkGraph{
			Beads:     make(map[string]*models.Bead),
			Edges:     []models.Edge{},
			UpdatedAt: time.Now(),
		},
		nextID:            1,
		projectPrefixes:   make(map[string]string),
		projectNextIDs:    make(map[string]int),
		projectBeadsPaths: make(map[string]string),
		gitConfigs:        make(map[string]*GitConfig),
		gitLocks:          make(map[string]*sync.Mutex),
	}
}

// projectGitLock returns (and lazily creates) the per-project mutex that
// serializes git operations on a given beads worktree.
func (m *Manager) projectGitLock(projectID string) *sync.Mutex {
	m.gitMu.Lock()
	defer m.gitMu.Unlock()
	if mu, ok := m.gitLocks[projectID]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	m.gitLocks[projectID] = mu
	return mu
}

// SetGitStorage configures git-centric bead storage for a project
func (m *Manager) SetGitStorage(projectID string, worktreeManager interface{}, beadsBranch string, enabled bool, authMethod, gitRepo string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gitConfigs[projectID] = &GitConfig{
		WorktreeManager: worktreeManager,
		BeadsBranch:     beadsBranch,
		UseGitStorage:   enabled,
		GitAuthMethod:   authMethod,
		GitRepo:         gitRepo,
	}
}

// buildGitAuthEnv returns environment variables needed for authenticated git operations.
func (m *Manager) buildGitAuthEnv(cfg *GitConfig) []string {
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	if cfg.GitAuthMethod != "token" {
		return env
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GITLAB_TOKEN")
	}
	if token == "" {
		return env
	}

	env = append(env,
		"GIT_ASKPASS=/usr/local/bin/git-askpass-helper",
		fmt.Sprintf("GIT_TOKEN=%s", token),
		fmt.Sprintf("GIT_REPO=%s", cfg.GitRepo),
	)
	return env
}

// SetBackend sets the beads backend type ("sqlite" or "dolt")
func (m *Manager) SetBackend(backend string) {
	m.backend = backend
}

// buildBDCommand constructs a bd command with the correct --db flag for sqlite backend
func (m *Manager) buildBDCommand(args ...string) *exec.Cmd {
	// For sqlite backend, explicitly pass --db flag to avoid dolt auto-discovery
	if m.backend == "sqlite" && m.beadsPath != "" {
		dbPath := filepath.Join(m.beadsPath, "beads.db")
		finalArgs := append([]string{"--db", dbPath}, args...)
		return exec.Command(m.bdPath, finalArgs...)
	}
	return exec.Command(m.bdPath, args...)
}

// Reset clears cached beads and work graph state.
// ClearProjectBeads removes all in-memory beads for a specific project.
// Call LoadBeadsFromFilesystem or LoadBeadsFromGit afterward to reload from disk.
func (m *Manager) ClearProjectBeads(projectID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, b := range m.beads {
		if b.ProjectID == projectID {
			delete(m.beads, id)
			delete(m.beadFiles, id)
			delete(m.workGraph.Beads, id)
		}
	}
}

func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beads = make(map[string]*models.Bead)
	m.beadFiles = make(map[string]string)
	m.workGraph = &models.WorkGraph{Beads: make(map[string]*models.Bead), Edges: []models.Edge{}, UpdatedAt: time.Now()}
	m.nextID = 1
	m.projectPrefixes = make(map[string]string)
	m.projectNextIDs = make(map[string]int)
	m.projectBeadsPaths = make(map[string]string)
}

// SetBeadsPath sets the global fallback path to the beads directory
func (m *Manager) SetBeadsPath(path string) {
	m.beadsPath = path
}

// SetProjectBeadsPath sets the beads path for a specific project (beads worktree).
// This prevents the last-writer-wins problem when multiple projects are loaded.
func (m *Manager) SetProjectBeadsPath(projectID, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectBeadsPaths[projectID] = path
}

// GetProjectBeadsPath returns the beads path for a specific project,
// falling back to the global beadsPath if none is set for that project.
func (m *Manager) GetProjectBeadsPath(projectID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if path, ok := m.projectBeadsPaths[projectID]; ok && path != "" {
		return path
	}
	return m.beadsPath
}

// SetProjectPrefix sets the bead ID prefix for a project
func (m *Manager) SetProjectPrefix(projectID, prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectPrefixes[projectID] = prefix
	if _, exists := m.projectNextIDs[projectID]; !exists {
		m.projectNextIDs[projectID] = 1
	}
}

// GetProjectPrefix returns the prefix for a project, defaulting to "bd" if not set
func (m *Manager) GetProjectPrefix(projectID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if prefix, ok := m.projectPrefixes[projectID]; ok && prefix != "" {
		return prefix
	}
	return "bd" // Default prefix
}

// LoadProjectPrefixFromConfig reads the issue-prefix from a project's .beads/config.yaml
func (m *Manager) LoadProjectPrefixFromConfig(projectID, beadsPath string) error {
	configPath := filepath.Join(beadsPath, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Config doesn't exist, use default
		}
		return fmt.Errorf("failed to read beads config: %w", err)
	}

	var config struct {
		IssuePrefix string `yaml:"issue-prefix"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse beads config: %w", err)
	}

	if config.IssuePrefix != "" {
		m.SetProjectPrefix(projectID, config.IssuePrefix)
	}
	return nil
}

// CreateBead creates a new bead using bd CLI or filesystem fallback
func (m *Manager) CreateBead(title, description string, priority models.BeadPriority, beadType, projectID string) (*models.Bead, error) {
	m.mu.Lock()

	var beadID string
	var bead *models.Bead

	// Get project-specific prefix
	prefix := "bd" // default
	if p, ok := m.projectPrefixes[projectID]; ok && p != "" {
		prefix = p
	}

	usedBD := false
	// Try bd CLI first if available
	if m.bdPath != "" {
		args := []string{"create", title, "-p", fmt.Sprintf("%d", priority)}

		if description != "" {
			args = append(args, "-d", description)
		}

		cmd := exec.Command(m.bdPath, args...)
		if dir := beadsRootDir(m.beadsPath); dir != "" {
			cmd.Dir = dir
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			outStr := strings.TrimSpace(string(output))
			// Auto-initialize if bd reports uninitialized database
			if strings.Contains(outStr, "database not initialized") || strings.Contains(outStr, "issue_prefix config is missing") {
				if initErr := m.tryAutoInitBD(prefix); initErr != nil {
					log.Printf("[Beads] bd auto-init failed: %v", initErr)
				} else {
					// Retry the create after init
					cmd2 := exec.Command(m.bdPath, args...)
					if dir := beadsRootDir(m.beadsPath); dir != "" {
						cmd2.Dir = dir
					}
					output, err = cmd2.CombinedOutput()
					if err != nil {
						log.Printf("[Beads] bd create failed after auto-init, falling back to filesystem: %v: %s", err, strings.TrimSpace(string(output)))
					}
				}
			} else {
				log.Printf("[Beads] bd create failed, falling back to filesystem: %v: %s", err, outStr)
			}
		}
		if err == nil {
			outputStr := string(output)
			beadID = m.extractBeadIDWithPrefix(outputStr, prefix)
			if beadID == "" {
				beadID = m.extractBeadID(outputStr)
			}
			if beadID == "" {
				log.Printf("[Beads] bd create did not return bead id, falling back to filesystem: %s", strings.TrimSpace(outputStr))
			} else {
				usedBD = true
			}
		}
	}

	// Fallback to filesystem-based bead creation
	if beadID == "" {
		// Get or initialize project-specific counter
		nextID := m.projectNextIDs[projectID]
		if nextID == 0 {
			nextID = 1
		}

		// Generate a new ID with project prefix
		beadID = fmt.Sprintf("%s-%03d", prefix, nextID)
		nextID++

		// Check for existing beads to avoid ID collision
		for {
			if _, exists := m.beads[beadID]; !exists {
				break
			}
			beadID = fmt.Sprintf("%s-%03d", prefix, nextID)
			nextID++
		}

		m.projectNextIDs[projectID] = nextID
	}

	// Create internal bead representation
	bead = &models.Bead{
		ID:          beadID,
		Type:        beadType,
		Title:       title,
		Description: description,
		Status:      models.BeadStatusOpen,
		Priority:    priority,
		ProjectID:   projectID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.beads[beadID] = bead
	m.workGraph.Beads[beadID] = bead
	m.workGraph.UpdatedAt = time.Now()

	// Release lock before I/O operations
	m.mu.Unlock()

	// Save to filesystem only when not using bd CLI
	if !usedBD {
		if err := m.SaveBeadToFilesystem(bead, m.GetProjectBeadsPath(bead.ProjectID)); err != nil {
			// Log error but don't fail - the bead is in memory
			fmt.Fprintf(os.Stderr, "Warning: failed to save bead to filesystem: %v\n", err)
		}
	}

	return bead, nil
}

// GetBead retrieves a bead by ID
func (m *Manager) GetBead(id string) (*models.Bead, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bead, ok := m.beads[id]
	if !ok {
		// Try to fetch from bd
		return m.fetchBeadFromBD(id)
	}

	return bead, nil
}

// ListBeads returns all beads, optionally filtered
func (m *Manager) ListBeads(filters map[string]interface{}) ([]*models.Bead, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// In a full implementation, this would call bd with appropriate filters
	// For now, return from cache with basic filtering

	beads := make([]*models.Bead, 0, len(m.beads))

	for _, bead := range m.beads {
		if m.matchesFilters(bead, filters) {
			beads = append(beads, bead)
		}
	}

	return beads, nil
}

// UpdateBead updates a bead
func (m *Manager) UpdateBead(id string, updates map[string]interface{}) error {
	// Update in-memory state with write lock
	m.mu.Lock()

	bead, ok := m.beads[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("bead not found %s: %w", id, ErrBeadNotFound)
	}

	previousAssigned := bead.AssignedTo
	assignedUpdated := false

	// Apply updates
	if status, ok := updates["status"].(models.BeadStatus); ok {
		bead.Status = status
		// Set closed_at timestamp if closing
		if status == models.BeadStatusClosed && bead.ClosedAt == nil {
			now := time.Now()
			bead.ClosedAt = &now
		}
		if status != models.BeadStatusClosed {
			bead.ClosedAt = nil
		}
		// When resetting to open, clear any stale assignment unless explicitly overridden
		if status == models.BeadStatusOpen {
			if _, hasAssigned := updates["assigned_to"]; !hasAssigned {
				bead.AssignedTo = ""
			}
		}
	}
	if priority, ok := updates["priority"].(models.BeadPriority); ok {
		bead.Priority = priority
	}
	if title, ok := updates["title"].(string); ok {
		bead.Title = title
	}
	if beadType, ok := updates["type"].(string); ok {
		bead.Type = beadType
	}
	if projectID, ok := updates["project_id"].(string); ok {
		bead.ProjectID = projectID
	}
	if assignedTo, ok := updates["assigned_to"].(string); ok {
		bead.AssignedTo = assignedTo
		assignedUpdated = true
	}
	if description, ok := updates["description"].(string); ok {
		bead.Description = description
	}
	if parent, ok := updates["parent"].(string); ok {
		bead.Parent = parent
	}
	if tags, ok := updates["tags"].([]string); ok {
		bead.Tags = tags
	}
	if blockedBy, ok := updates["blocked_by"].([]string); ok {
		bead.BlockedBy = blockedBy
	}
	if blocks, ok := updates["blocks"].([]string); ok {
		bead.Blocks = blocks
	}
	if relatedTo, ok := updates["related_to"].([]string); ok {
		bead.RelatedTo = relatedTo
	}
	if children, ok := updates["children"].([]string); ok {
		bead.Children = children
	}
	if ctxUpdates, ok := updates["context"].(map[string]string); ok {
		if bead.Context == nil {
			bead.Context = make(map[string]string)
		}
		for k, v := range ctxUpdates {
			bead.Context[k] = v
		}
	}

	bead.UpdatedAt = time.Now()
	m.workGraph.UpdatedAt = time.Now()

	if assignedUpdated && previousAssigned != bead.AssignedTo {
		observability.Info("bead.assignment_updated", map[string]interface{}{
			"bead_id":              bead.ID,
			"project_id":           bead.ProjectID,
			"assigned_to":          bead.AssignedTo,
			"previous_assigned_to": previousAssigned,
		})
	}

	// Release lock before expensive I/O operations
	// SaveBeadToGit has its own locking for safe concurrent access
	m.mu.Unlock()

	// Save to filesystem and git (without holding the main lock)
	if err := m.SaveBeadToGit(context.Background(), bead, m.GetProjectBeadsPath(bead.ProjectID)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save bead to git: %v\n", err)
	}

	return nil
}

// ClaimBead assigns a bead to an agent
func (m *Manager) ClaimBead(beadID, agentID string) error {
	// Update in-memory state with write lock
	m.mu.Lock()

	bead, ok := m.beads[beadID]
	if !ok {
		m.mu.Unlock()
		err := fmt.Errorf("bead not found %s: %w", beadID, ErrBeadNotFound)
		observability.Error("bead.claim", map[string]interface{}{
			"agent_id": agentID,
			"bead_id":  beadID,
		}, err)
		return err
	}

	if bead.AssignedTo != "" && bead.AssignedTo != agentID {
		m.mu.Unlock()
		err := fmt.Errorf("bead already claimed by agent %s: %w", bead.AssignedTo, ErrBeadAlreadyClaimed)
		observability.Error("bead.claim", map[string]interface{}{
			"agent_id":    agentID,
			"bead_id":     beadID,
			"assigned_to": bead.AssignedTo,
			"project_id":  bead.ProjectID,
		}, err)
		return err
	}

	bead.AssignedTo = agentID
	bead.Status = models.BeadStatusInProgress
	bead.UpdatedAt = time.Now()

	observability.Info("bead.claim", map[string]interface{}{
		"agent_id":   agentID,
		"bead_id":    bead.ID,
		"project_id": bead.ProjectID,
		"status":     "claimed",
	})

	// Release lock before I/O operations
	m.mu.Unlock()

	if err := m.SaveBeadToFilesystem(bead, m.GetProjectBeadsPath(bead.ProjectID)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save bead to filesystem: %v\n", err)
	}

	return nil
}

// ReassignBead forcibly reassigns a bead to a new agent, overriding any
// existing assignment. The caller (dispatcher) must have already validated
// that the previous agent is no longer actively working on this bead.
func (m *Manager) ReassignBead(beadID, newAgentID, previousAgentID string) error {
	m.mu.Lock()

	bead, ok := m.beads[beadID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("bead not found %s: %w", beadID, ErrBeadNotFound)
	}

	oldAgent := bead.AssignedTo
	bead.AssignedTo = newAgentID
	bead.Status = models.BeadStatusInProgress
	bead.UpdatedAt = time.Now()

	observability.Info("bead.reassign", map[string]interface{}{
		"bead_id":      bead.ID,
		"project_id":   bead.ProjectID,
		"old_agent_id": oldAgent,
		"new_agent_id": newAgentID,
		"expected_old": previousAgentID,
	})

	m.mu.Unlock()

	if err := m.SaveBeadToFilesystem(bead, m.GetProjectBeadsPath(bead.ProjectID)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save bead to filesystem: %v\n", err)
	}

	return nil
}

// AddDependency adds a dependency between beads
func (m *Manager) AddDependency(childID, parentID, relationship string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	child, ok := m.beads[childID]
	if !ok {
		return fmt.Errorf("child bead not found %s: %w", childID, ErrBeadNotFound)
	}

	parent, ok := m.beads[parentID]
	if !ok {
		return fmt.Errorf("parent bead not found %s: %w", parentID, ErrBeadNotFound)
	}

	// Update bead relationships
	switch relationship {
	case "blocks":
		child.BlockedBy = append(child.BlockedBy, parentID)
		parent.Blocks = append(parent.Blocks, childID)
		if child.Status == models.BeadStatusInProgress {
			child.Status = models.BeadStatusBlocked
		}
	case "parent":
		child.Parent = parentID
		parent.Children = append(parent.Children, childID)
	case "related":
		child.RelatedTo = append(child.RelatedTo, parentID)
		parent.RelatedTo = append(parent.RelatedTo, childID)
	default:
		return fmt.Errorf("unknown relationship: %s", relationship)
	}

	// Update work graph
	m.workGraph.Edges = append(m.workGraph.Edges, models.Edge{
		From:         childID,
		To:           parentID,
		Relationship: relationship,
	})
	m.workGraph.UpdatedAt = time.Now()

	return nil
}

// GetReadyBeads returns beads with no open blockers
func (m *Manager) GetReadyBeads(projectID string) ([]*models.Bead, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ready := make([]*models.Bead, 0)

	for _, bead := range m.beads {
		if projectID != "" && bead.ProjectID != projectID {
			continue
		}

		if bead.Status != models.BeadStatusOpen && bead.Status != models.BeadStatusInProgress {
			continue
		}

		// Check if all blockers are resolved.
		// Blockers not in the cache are treated as resolved (they're closed
		// beads that were excluded from the active-only load).
		allResolved := true
		for _, blockerID := range bead.BlockedBy {
			blocker, ok := m.beads[blockerID]
			if ok && blocker.Status != models.BeadStatusClosed {
				allResolved = false
				break
			}
		}

		if allResolved {
			ready = append(ready, bead)
		}
	}

	return ready, nil
}

// UnblockBead removes a blocking dependency
func (m *Manager) UnblockBead(beadID, blockerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	bead, ok := m.beads[beadID]
	if !ok {
		return fmt.Errorf("bead not found %s: %w", beadID, ErrBeadNotFound)
	}

	// Remove blocker
	for i, id := range bead.BlockedBy {
		if id == blockerID {
			bead.BlockedBy = append(bead.BlockedBy[:i], bead.BlockedBy[i+1:]...)
			break
		}
	}

	// If no more blockers, unblock
	if len(bead.BlockedBy) == 0 && bead.Status == models.BeadStatusBlocked {
		bead.Status = models.BeadStatusOpen
	}

	bead.UpdatedAt = time.Now()
	m.workGraph.UpdatedAt = time.Now()

	return nil
}

// GetWorkGraph returns the current work graph
func (m *Manager) GetWorkGraph(projectID string) (*models.WorkGraph, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if projectID == "" {
		return m.workGraph, nil
	}

	// Filter by project
	filteredGraph := &models.WorkGraph{
		Beads:     make(map[string]*models.Bead),
		Edges:     []models.Edge{},
		UpdatedAt: m.workGraph.UpdatedAt,
	}

	for id, bead := range m.workGraph.Beads {
		if bead.ProjectID == projectID {
			filteredGraph.Beads[id] = bead
		}
	}

	for _, edge := range m.workGraph.Edges {
		if _, ok := filteredGraph.Beads[edge.From]; ok {
			if _, ok := filteredGraph.Beads[edge.To]; ok {
				filteredGraph.Edges = append(filteredGraph.Edges, edge)
			}
		}
	}

	return filteredGraph, nil
}

// Helper functions

func (m *Manager) extractBeadID(output string) string {
	fields := strings.Fields(output)
	for _, field := range fields {
		cleaned := strings.Trim(field, ",.:;[](){}")
		if beadIDPattern.MatchString(cleaned) {
			return cleaned
		}
	}
	return ""
}

func (m *Manager) extractBeadIDWithPrefix(output, prefix string) string {
	// Look for pattern like "<prefix>-xxxxx" or "Created <prefix>-xxxxx"
	parts := strings.Fields(output)
	prefixDash := prefix + "-"
	for _, part := range parts {
		if strings.HasPrefix(part, prefixDash) {
			return part
		}
	}
	return ""
}

// tryAutoInitBD attempts to initialize the bd database when it's detected as uninitialized.
// Must be called with m.mu held.
func (m *Manager) tryAutoInitBD(prefix string) error {
	if m.bdPath == "" {
		return fmt.Errorf("bd path not set")
	}

	args := []string{"init", "--prefix", prefix}
	cmd := exec.Command(m.bdPath, args...)
	if dir := beadsRootDir(m.beadsPath); dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	if err != nil {
		// "already initialized" is fine
		if strings.Contains(outStr, "already initialized") {
			return nil
		}
		return fmt.Errorf("bd init --prefix %s failed: %w: %s", prefix, err, outStr)
	}
	log.Printf("[Beads] Auto-initialized bd database with prefix %q", prefix)
	return nil
}

var beadIDPattern = regexp.MustCompile(`(?i)\b[a-z0-9]{2,8}-[a-z0-9]+\b`)

func beadsRootDir(beadsPath string) string {
	if beadsPath == "" {
		return ""
	}

	cleaned := filepath.Clean(beadsPath)
	if filepath.Base(cleaned) == "beads" {
		cleaned = filepath.Dir(cleaned)
	}
	if filepath.Base(cleaned) == ".beads" {
		return filepath.Dir(cleaned)
	}
	return filepath.Dir(cleaned)
}

func (m *Manager) fetchBeadFromBD(id string) (*models.Bead, error) {
	// Execute: bd show <id> --json
	cmd := exec.Command(m.bdPath, "show", id, "--json")
	if dir := beadsRootDir(m.beadsPath); dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bead: %w", err)
	}

	var bead models.Bead
	if err := json.Unmarshal(output, &bead); err != nil {
		return nil, fmt.Errorf("failed to parse bead: %w", err)
	}

	return &bead, nil
}

func (m *Manager) matchesFilters(bead *models.Bead, filters map[string]interface{}) bool {
	if projectID, ok := filters["project_id"].(string); ok {
		if bead.ProjectID != projectID {
			return false
		}
	}

	if status, ok := filters["status"].(models.BeadStatus); ok {
		if bead.Status != status {
			return false
		}
	}

	if beadType, ok := filters["type"].(string); ok {
		if bead.Type != beadType {
			return false
		}
	}

	if assignedTo, ok := filters["assigned_to"]; ok {
		switch value := assignedTo.(type) {
		case string:
			if bead.AssignedTo != value {
				return false
			}
		case []string:
			match := false
			for _, candidate := range value {
				if bead.AssignedTo == candidate {
					match = true
					break
				}
			}
			if !match {
				return false
			}
		}
	}

	return true
}

// RefreshBeads reloads the bead cache from the bd CLI / Dolt database.
// Call periodically to pick up beads created after startup.
func (m *Manager) RefreshBeads(projectID, beadsPath string) error {
	return m.LoadBeadsFromFilesystem(projectID, beadsPath)
}

// LoadBeadsFromFilesystem loads beads using bd CLI when available, with YAML fallback.
func (m *Manager) LoadBeadsFromFilesystem(projectID, beadsPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Skip bd CLI for yaml backend - it would try Dolt which may not be running.
	// For yaml backend, YAML files on disk are the authoritative source.
	if m.bdPath != "" && m.backend != "yaml" {
		if err := m.loadBeadsFromBD(projectID, beadsPath); err == nil {
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "Warning: failed to load beads via bd CLI: %v\n", err)
		}
	}

	beadsDir := filepath.Join(beadsPath, "beads")

	// Check if directory exists
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return nil // No beads directory, skip silently
	}

	// Read all YAML files in beads directory
	entries, err := os.ReadDir(beadsDir)
	if err != nil {
		return fmt.Errorf("failed to read beads directory: %w", err)
	}

	loadedCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		beadPath := filepath.Join(beadsDir, entry.Name())
		data, err := os.ReadFile(beadPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read bead file %s: %v\n", entry.Name(), err)
			continue // Skip files we can't read
		}

		var bead models.Bead
		if err := yaml.Unmarshal(data, &bead); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse bead file %s: %v\n", entry.Name(), err)
			continue // Skip invalid YAML
		}

		// Add to internal cache
		if bead.ProjectID == "" && projectID != "" {
			bead.ProjectID = projectID
		}
		m.beads[bead.ID] = &bead
		m.workGraph.Beads[bead.ID] = &bead
		m.beadFiles[bead.ID] = beadPath
		loadedCount++
	}

	if loadedCount > 0 {
		fmt.Fprintf(os.Stderr, "Loaded %d bead(s) from %s\n", loadedCount, beadsDir)
	}

	m.workGraph.UpdatedAt = time.Now()
	return nil
}

type bdIssue struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	IssueType   string     `json:"issue_type"`
	Assignee    string     `json:"assignee"`
	Labels      []string   `json:"labels"`
	Parent      string     `json:"parent"`
	Children    []string   `json:"children"`
	BlockedBy   []string   `json:"blocked_by"`
	Blocks      []string   `json:"blocks"`
	RelatedTo   []string   `json:"related_to"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
}

func (m *Manager) loadBeadsFromBD(projectID, beadsPath string) error {
	// Only load non-closed beads to avoid loading thousands of historical entries.
	// Closed beads are not needed for dispatch, routing, or stuck detection.
	var allOutput []byte
	dir := beadsRootDir(beadsPath)
	failCount := 0
	for _, status := range []string{"open", "in_progress", "blocked"} {
		cmd := m.buildBDCommand("list", "--json", "--limit", "0", "--allow-stale", "--status="+status)
		if dir != "" {
			cmd.Dir = dir
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[BeadManager] bd list --status=%s failed: %v: %s", status, err, strings.TrimSpace(string(output)))
			failCount++
			continue
		}
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" || trimmed == "[]" {
			continue
		}
		// Strip array brackets and accumulate entries
		if idx := strings.Index(trimmed, "["); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
		if idx := strings.LastIndex(trimmed, "]"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			continue
		}
		if len(allOutput) > 0 {
			allOutput = append(allOutput, ',')
		}
		allOutput = append(allOutput, []byte(trimmed)...)
	}

	// If all bd commands failed, report error so YAML fallback can run.
	if failCount == 3 {
		return fmt.Errorf("all bd list commands failed")
	}

	combined := "[" + string(allOutput) + "]"

	var issues []bdIssue
	if err := json.Unmarshal([]byte(combined), &issues); err != nil {
		return fmt.Errorf("failed to parse bd list output: %w", err)
	}

	for _, issue := range issues {
		beadType := issue.IssueType
		if beadType == "" {
			beadType = "task"
		}

		// If this bead was already loaded by a different project, keep the
		// original project assignment. This prevents a shared Dolt database
		// from clobbering project IDs on every refresh cycle.
		existingProjectID := projectID
		if existing, ok := m.beads[issue.ID]; ok && existing.ProjectID != "" && existing.ProjectID != projectID {
			existingProjectID = existing.ProjectID
		}

		bead := &models.Bead{
			ID:          issue.ID,
			Type:        beadType,
			Title:       issue.Title,
			Description: issue.Description,
			Status:      models.BeadStatus(issue.Status),
			Priority:    models.BeadPriority(issue.Priority),
			ProjectID:   existingProjectID,
			AssignedTo:  issue.Assignee,
			BlockedBy:   issue.BlockedBy,
			Blocks:      issue.Blocks,
			RelatedTo:   issue.RelatedTo,
			Parent:      issue.Parent,
			Children:    issue.Children,
			Tags:        issue.Labels,
			CreatedAt:   issue.CreatedAt,
			UpdatedAt:   issue.UpdatedAt,
			ClosedAt:    issue.ClosedAt,
		}

		m.beads[bead.ID] = bead
		m.workGraph.Beads[bead.ID] = bead
	}

	m.workGraph.UpdatedAt = time.Now()
	return nil
}

// SaveBeadToFilesystem saves a bead to the filesystem
func (m *Manager) SaveBeadToFilesystem(bead *models.Bead, beadsPath string) error {
	beadsDir := filepath.Join(beadsPath, "beads")

	// Ensure directory exists
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		return fmt.Errorf("failed to create beads directory: %w", err)
	}

	// Preserve existing bead file path if we loaded it from disk, to avoid creating duplicates.
	// Use read lock to safely access beadFiles map
	m.mu.RLock()
	beadPath, exists := m.beadFiles[bead.ID]
	m.mu.RUnlock()

	if !exists || beadPath == "" {
		// Generate filename from bead ID and title
		filename := fmt.Sprintf("%s-%s.yaml", bead.ID, sanitizeFilename(bead.Title))
		beadPath = filepath.Join(beadsDir, filename)

		// Use write lock to update beadFiles map
		m.mu.Lock()
		m.beadFiles[bead.ID] = beadPath
		m.mu.Unlock()
	}

	// Marshal to YAML
	data, err := yaml.Marshal(bead)
	if err != nil {
		return fmt.Errorf("failed to marshal bead: %w", err)
	}

	// Write to file
	if err := os.WriteFile(beadPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write bead file: %w", err)
	}

	return nil
}

// LoadBeadsFromGit syncs beads branch from git and loads YAMLs
func (m *Manager) LoadBeadsFromGit(ctx context.Context, projectID, beadsPath string) error {
	// Get git configuration for this project
	m.mu.RLock()
	gitConfig, ok := m.gitConfigs[projectID]
	m.mu.RUnlock()

	if !ok || gitConfig == nil || !gitConfig.UseGitStorage || gitConfig.WorktreeManager == nil {
		// Fallback to filesystem-only loading
		return m.LoadBeadsFromFilesystem(projectID, beadsPath)
	}

	// Pull latest beads from remote using worktree manager
	// We use type assertion to call the sync method
	type syncer interface {
		SyncBeadsBranch(string) error
	}
	if wt, ok := gitConfig.WorktreeManager.(syncer); ok {
		if err := wt.SyncBeadsBranch(projectID); err != nil {
			log.Printf("Warning: git pull failed for project %s, using local beads: %v", projectID, err)
		}
	}

	// Load from filesystem (now synced with git)
	return m.LoadBeadsFromFilesystem(projectID, beadsPath)
}

// SaveBeadToGit commits bead to git and pushes to remote
func (m *Manager) SaveBeadToGit(ctx context.Context, bead *models.Bead, beadsPath string) error {
	// First save to filesystem (YAML)
	if err := m.SaveBeadToFilesystem(bead, beadsPath); err != nil {
		return fmt.Errorf("failed to save bead YAML: %w", err)
	}

	// Get git configuration for this bead's project
	m.mu.RLock()
	gitConfig, ok := m.gitConfigs[bead.ProjectID]
	m.mu.RUnlock()

	if !ok || gitConfig == nil || !gitConfig.UseGitStorage || gitConfig.WorktreeManager == nil {
		return nil // No git operations if disabled or not configured for this project
	}

	// Serialize all git operations for this project to prevent concurrent
	// git processes from leaving index.lock files.
	gitLock := m.projectGitLock(bead.ProjectID)
	gitLock.Lock()
	defer gitLock.Unlock()

	// Get beads worktree path
	type pathGetter interface {
		GetWorktreePath(string, string) string
	}
	var beadsWorktree string
	if wt, ok := gitConfig.WorktreeManager.(pathGetter); ok {
		beadsWorktree = wt.GetWorktreePath(bead.ProjectID, "beads")
	} else {
		return fmt.Errorf("worktree manager does not support GetWorktreePath")
	}

	// Derive the relative path from the actual saved file location.
	// Beads may have been loaded from the main worktree (m.beadFiles points there),
	// so we cannot assume the file is already in the beads worktree.
	m.mu.RLock()
	actualPath := m.beadFiles[bead.ID]
	m.mu.RUnlock()

	var beadFile string
	if rel, err := filepath.Rel(beadsWorktree, actualPath); err == nil && !strings.HasPrefix(rel, "..") {
		// File is already inside the beads worktree — use its relative path.
		beadFile = rel
	} else {
		// File is outside the beads worktree (e.g., loaded from main worktree).
		// Write a copy into the beads worktree so it can be staged.
		beadFile = filepath.Join(".beads", "beads", filepath.Base(actualPath))
		dest := filepath.Join(beadsWorktree, beadFile)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("failed to create beads dir in worktree: %w", err)
		}
		data, err := os.ReadFile(actualPath)
		if err != nil {
			return fmt.Errorf("failed to read bead file for copy: %w", err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return fmt.Errorf("failed to copy bead to beads worktree: %w", err)
		}
		// Update m.beadFiles to point to the beads worktree going forward.
		m.mu.Lock()
		m.beadFiles[bead.ID] = dest
		m.mu.Unlock()
	}

	// Auto-recover from a stuck rebase before doing anything else.
	rebaseMerge := filepath.Join(beadsWorktree, ".git", "rebase-merge")
	rebaseApply := filepath.Join(beadsWorktree, ".git", "rebase-apply")
	if _, err := os.Stat(rebaseMerge); err == nil {
		log.Printf("[BeadsGit] Detected stuck rebase-merge in %s, aborting", beadsWorktree)
		abort := exec.Command("git", "rebase", "--abort")
		abort.Dir = beadsWorktree
		_ = abort.Run()
	} else if _, err := os.Stat(rebaseApply); err == nil {
		log.Printf("[BeadsGit] Detected stuck rebase-apply in %s, aborting", beadsWorktree)
		abort := exec.Command("git", "rebase", "--abort")
		abort.Dir = beadsWorktree
		_ = abort.Run()
	}

	// Stage the bead file
	addCmd := exec.Command("git", "add", beadFile)
	addCmd.Dir = beadsWorktree
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %s - %w", output, err)
	}

	// Commit with descriptive message
	message := fmt.Sprintf("Update bead %s: %s\n\nStatus: %s\nAgent: %s\nPriority: %v",
		bead.ID, bead.Title, bead.Status, bead.AssignedTo, bead.Priority)
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = beadsWorktree
	if output, err := commitCmd.CombinedOutput(); err != nil {
		// Check if it's "nothing to commit" (not an error)
		if !strings.Contains(string(output), "nothing to commit") {
			return fmt.Errorf("git commit failed: %s - %w", output, err)
		}
		// Nothing to commit is OK, just return
		return nil
	}

	// Build auth environment once for all git commands
	authEnv := m.buildGitAuthEnv(gitConfig)

	// Push to remote with retry logic for conflicts
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		pushCmd := exec.Command("git", "push")
		pushCmd.Dir = beadsWorktree
		pushCmd.Env = authEnv
		output, err := pushCmd.CombinedOutput()
		if err == nil {
			return nil
		}

		if strings.Contains(string(output), "rejected") || strings.Contains(string(output), "non-fast-forward") {
			log.Printf("[BeadsGit] Push conflict detected (attempt %d/%d), rebasing...", i+1, maxRetries)

			pullCmd := exec.Command("git", "pull", "--rebase")
			pullCmd.Dir = beadsWorktree
			pullCmd.Env = authEnv
			pullOutput, pullErr := pullCmd.CombinedOutput()
			if pullErr != nil {
				// Rebase conflict — abort and retry cleanly
				log.Printf("[BeadsGit] Rebase failed: %s, aborting", pullOutput)
				abortCmd := exec.Command("git", "rebase", "--abort")
				abortCmd.Dir = beadsWorktree
				_ = abortCmd.Run()
			}

			continue
		}

		return fmt.Errorf("git push failed: %s - %w", output, err)
	}

	return fmt.Errorf("git push failed after %d retries due to conflicts", maxRetries)
}

// SyncFederation syncs with all enabled federation peers.
func (m *Manager) SyncFederation(ctx context.Context, cfg *config.BeadsFederationConfig) error {
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	var lastErr error
	for _, peer := range cfg.Peers {
		if !peer.Enabled {
			continue
		}
		if err := m.syncWithPeer(ctx, peer, cfg.SyncStrategy); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: federation sync with peer %s failed: %v\n", peer.Name, err)
			lastErr = err
		}
	}
	return lastErr
}

// syncWithPeer runs `bd federation sync --peer <name> [--strategy <s>]`.
func (m *Manager) syncWithPeer(ctx context.Context, peer config.FederationPeer, strategy string) error {
	args := []string{"federation", "sync", "--peer", peer.Name}
	if strategy != "" {
		args = append(args, "--strategy", strategy)
	}

	cmd := exec.CommandContext(ctx, m.bdPath, args...)
	if dir := beadsRootDir(m.beadsPath); dir != "" {
		cmd.Dir = dir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd federation sync --peer %s failed: %w: %s", peer.Name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// FederationStatus runs `bd federation status --json` and returns the raw JSON.
func (m *Manager) FederationStatus(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, m.bdPath, "federation", "status", "--json")
	if dir := beadsRootDir(m.beadsPath); dir != "" {
		cmd.Dir = dir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("bd federation status failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// sanitizeFilename removes characters that aren't safe for filenames
func sanitizeFilename(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)
	// Replace spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	// Keep only alphanumeric and hyphens
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	// Limit length
	if result.Len() > 50 {
		return result.String()[:50]
	}
	return result.String()
}
