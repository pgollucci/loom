package gitops

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// GitWorktreeManager manages git worktrees for project and beads branches
type GitWorktreeManager struct {
	projectsRoot string // e.g., /app/data/projects
}

// NewGitWorktreeManager creates a new worktree manager
func NewGitWorktreeManager(projectsRoot string) *GitWorktreeManager {
	return &GitWorktreeManager{projectsRoot: projectsRoot}
}

// SetupBeadsWorktree creates isolated worktree for beads branch
// This allows concurrent access to main branch (for code) and beads branch (for bead metadata)
func (m *GitWorktreeManager) SetupBeadsWorktree(projectID, mainBranch, beadsBranch string) error {
	projectDir := filepath.Join(m.projectsRoot, projectID)
	mainWorktree := filepath.Join(projectDir, "main")
	beadsWorktree := filepath.Join(projectDir, "beads")

	// Ensure main worktree exists
	if _, err := os.Stat(mainWorktree); os.IsNotExist(err) {
		return fmt.Errorf("main worktree not found: %s", mainWorktree)
	}

	// Check if beads branch exists remotely
	checkCmd := exec.Command("git", "ls-remote", "--heads", "origin", beadsBranch)
	checkCmd.Dir = mainWorktree
	output, _ := checkCmd.CombinedOutput()

	branchExists := len(output) > 0

	if !branchExists {
		// Create orphan branch for beads
		if err := m.initializeBeadsBranch(mainWorktree, beadsBranch); err != nil {
			return fmt.Errorf("failed to initialize beads branch: %w", err)
		}
	}

	// Check if worktree already exists
	worktreeExists := false
	if _, err := os.Stat(beadsWorktree); !os.IsNotExist(err) {
		// Worktree exists, verify it's tracking the right branch
		worktreeExists = true
	}

	if !worktreeExists {
		// Create worktree for beads branch
		worktreeCmd := exec.Command("git", "worktree", "add", beadsWorktree, beadsBranch)
		worktreeCmd.Dir = mainWorktree
		if output, err := worktreeCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git worktree add failed: %s - %w", output, err)
		}
	}

	// Ensure upstream tracking is configured (do this every time to handle restarts)
	// This allows git push to work without -u flag
	configRemoteCmd := exec.Command("git", "config", "branch."+beadsBranch+".remote", "origin")
	configRemoteCmd.Dir = beadsWorktree
	if err := configRemoteCmd.Run(); err != nil {
		log.Printf("Warning: failed to set remote config for %s: %v", beadsBranch, err)
	}

	configMergeCmd := exec.Command("git", "config", "branch."+beadsBranch+".merge", "refs/heads/"+beadsBranch)
	configMergeCmd.Dir = beadsWorktree
	if err := configMergeCmd.Run(); err != nil {
		log.Printf("Warning: failed to set merge config for %s: %v", beadsBranch, err)
	}

	return nil
}

// initializeBeadsBranch creates orphan branch with initial structure
// Orphan branches have no parent commits and are used to store independent data
func (m *GitWorktreeManager) initializeBeadsBranch(repoPath, beadsBranch string) error {
	// Create orphan branch
	checkoutCmd := exec.Command("git", "checkout", "--orphan", beadsBranch)
	checkoutCmd.Dir = repoPath
	if err := checkoutCmd.Run(); err != nil {
		return err
	}

	// Remove all staged files
	resetCmd := exec.Command("git", "reset", "--hard")
	resetCmd.Dir = repoPath
	resetCmd.Run()

	// Create initial .beads structure
	beadsDir := filepath.Join(repoPath, ".beads", "beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		return fmt.Errorf("failed to create beads directory: %w", err)
	}

	// Create config file
	configPath := filepath.Join(repoPath, ".beads", "config.yaml")
	configContent := fmt.Sprintf(`# Beads configuration
version: 1
sync-branch: %s
`, beadsBranch)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Create .gitignore to ensure .beads is tracked
	gitignorePath := filepath.Join(repoPath, ".beads", ".gitignore")
	gitignoreContent := `# Keep beads directory structure
!beads/
beads/.gitkeep
`
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("failed to write .gitignore: %w", err)
	}

	// Initial commit
	addCmd := exec.Command("git", "add", ".beads")
	addCmd.Dir = repoPath
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	commitCmd := exec.Command("git", "commit", "-m", "Initialize beads branch")
	commitCmd.Dir = repoPath
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	// Try to push to remote (optional - may fail if no write access)
	pushCmd := exec.Command("git", "push", "-u", "origin", beadsBranch)
	pushCmd.Dir = repoPath
	if err := pushCmd.Run(); err != nil {
		// Log warning but don't fail - we can still use local branch
		log.Printf("Warning: Failed to push beads branch to remote: %v (branch will be local-only)", err)
	}

	// Return to main branch so worktree creation can use beads-sync
	checkoutMainCmd := exec.Command("git", "checkout", "main")
	checkoutMainCmd.Dir = repoPath
	if err := checkoutMainCmd.Run(); err != nil {
		return fmt.Errorf("failed to return to main branch: %w", err)
	}

	return nil
}

// GetWorktreePath returns path to specific worktree (main or beads)
func (m *GitWorktreeManager) GetWorktreePath(projectID, worktree string) string {
	return filepath.Join(m.projectsRoot, projectID, worktree)
}

// CleanupWorktree removes worktree (for cleanup/shutdown)
func (m *GitWorktreeManager) CleanupWorktree(projectID, worktree string) error {
	projectDir := filepath.Join(m.projectsRoot, projectID, "main")
	worktreePath := filepath.Join(m.projectsRoot, projectID, worktree)

	cmd := exec.Command("git", "worktree", "remove", worktreePath)
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s - %w", output, err)
	}

	return nil
}

// SetupAgentWorktree creates an isolated worktree for a specific agent/bead
// combination, allowing multiple agents to work on the same repo simultaneously
// without stepping on each other's changes.
func (m *GitWorktreeManager) SetupAgentWorktree(projectID, beadID, baseBranch string) (string, error) {
	projectDir := filepath.Join(m.projectsRoot, projectID)
	mainWorktree := filepath.Join(projectDir, "main")

	if _, err := os.Stat(mainWorktree); os.IsNotExist(err) {
		return "", fmt.Errorf("main worktree not found: %s", mainWorktree)
	}

	branchName := fmt.Sprintf("bead/%s", beadID)
	worktreePath := filepath.Join(projectDir, "agents", beadID)

	// If worktree already exists, just return the path
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		log.Printf("[Worktree] Agent worktree already exists for bead %s: %s", beadID, worktreePath)
		return worktreePath, nil
	}

	// Ensure the agents directory exists
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create agents directory: %w", err)
	}

	// Check if branch exists
	checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
	checkCmd.Dir = mainWorktree
	if err := checkCmd.Run(); err != nil {
		// Branch doesn't exist — create it from base
		if baseBranch == "" {
			baseBranch = "main"
		}
		createCmd := exec.Command("git", "branch", branchName, baseBranch)
		createCmd.Dir = mainWorktree
		if output, err := createCmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git branch create failed: %s - %w", output, err)
		}
	}

	// Create worktree
	worktreeCmd := exec.Command("git", "worktree", "add", worktreePath, branchName)
	worktreeCmd.Dir = mainWorktree
	if output, err := worktreeCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s - %w", output, err)
	}

	log.Printf("[Worktree] Created agent worktree for bead %s at %s (branch %s)", beadID, worktreePath, branchName)
	return worktreePath, nil
}

// CleanupAgentWorktree removes an agent's worktree after the bead is completed.
func (m *GitWorktreeManager) CleanupAgentWorktree(projectID, beadID string) error {
	projectDir := filepath.Join(m.projectsRoot, projectID, "main")
	worktreePath := filepath.Join(m.projectsRoot, projectID, "agents", beadID)

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	}

	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s - %w", output, err)
	}

	log.Printf("[Worktree] Cleaned up agent worktree for bead %s", beadID)
	return nil
}

// ListAgentWorktrees returns all active agent worktrees for a project.
func (m *GitWorktreeManager) ListAgentWorktrees(projectID string) ([]string, error) {
	agentsDir := filepath.Join(m.projectsRoot, projectID, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var worktrees []string
	for _, e := range entries {
		if e.IsDir() {
			worktrees = append(worktrees, e.Name())
		}
	}
	return worktrees, nil
}

// SyncBeadsBranch pulls latest beads from remote
// This ensures the local beads worktree is up-to-date with git remote
func (m *GitWorktreeManager) SyncBeadsBranch(projectID string) error {
	beadsWorktree := m.GetWorktreePath(projectID, "beads")

	// Use fetch + merge instead of pull --rebase to avoid rebase conflicts
	// Fetch updates from remote
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = beadsWorktree
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s - %w", output, err)
	}

	// Merge with prefer-ours strategy for automatic conflict resolution
	// This ensures local changes take precedence during conflicts
	mergeCmd := exec.Command("git", "merge", "origin/beads-sync", "-X", "ours", "--no-edit")
	mergeCmd.Dir = beadsWorktree
	output, err := mergeCmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: git merge failed, will retry next sync: %s", output)
		// Don't return error - allow system to continue with local state
		return nil
	}

	return nil
}
