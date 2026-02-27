// Package cimon periodically checks GitHub Actions CI/CD status for all
// projects and files P0 beads when workflows are failing on the default branch.
// It runs as a lightweight goroutine alongside the task executor.
package cimon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/internal/github"
	"github.com/jordanhubbard/loom/pkg/models"
)

// ProjectResolver provides cross-project view needed by the runner.
type ProjectResolver interface {
	ListProjectIDs() []string
	GetProjectWorkDir(projectID string) string
	GetProject(projectID string) (*models.Project, error)
}

// BeadWriter provides bead read/write operations.
type BeadWriter interface {
	CreateBead(title, description string, priority models.BeadPriority, beadType, projectID string) (*models.Bead, error)
	GetBeadsByProject(projectID string) ([]*models.Bead, error)
}

// Runner periodically sweeps all projects, checks their GitHub Actions status,
// and files P0 beads for any failing workflows that don't already have one open.
type Runner struct {
	projects ProjectResolver
	beads    BeadWriter
	interval time.Duration
	stopCh   chan struct{}
}

// NewRunner creates a CI monitor runner. interval defaults to 30 minutes if <= 0.
func NewRunner(projects ProjectResolver, beads BeadWriter, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	return &Runner{
		projects: projects,
		beads:    beads,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start runs the CI check sweep at the configured interval until the context
// is cancelled or Stop is called. Also runs an immediate check on startup
// so failures are caught right away rather than waiting for the first tick.
func (r *Runner) Start(ctx context.Context) {
	log.Printf("[CIMon] Starting CI/CD monitor with %s interval", r.interval)

	// Immediate check on startup.
	r.sweep(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

// Stop signals the runner to exit its loop.
func (r *Runner) Stop() {
	close(r.stopCh)
}

// sweep checks every project that has a GitHub repo (explicit or derivable from git_repo URL).
func (r *Runner) sweep(ctx context.Context) {
	for _, pid := range r.projects.ListProjectIDs() {
		proj, err := r.projects.GetProject(pid)
		if err != nil || proj == nil {
			continue
		}

		repo := proj.GitHubRepo
		if repo == "" {
			repo = deriveGitHubRepo(proj.GitRepo)
		}
		if repo == "" {
			continue // not a GitHub project
		}

		workDir := proj.WorkDir
		if workDir == "" {
			workDir = fmt.Sprintf("data/projects/%s/main", pid)
		}

		token := proj.Context["github_token"] // may be empty; gh falls back to stored creds
		if err := r.checkProject(ctx, pid, repo, workDir, token); err != nil {
			log.Printf("[CIMon] project %s: %v", pid, err)
		}
	}
}

// deriveGitHubRepo extracts "owner/repo" from a GitHub HTTPS URL.
// Returns "" if the URL is not a recognizable GitHub URL.
func deriveGitHubRepo(gitURL string) string {
	if !strings.Contains(gitURL, "github.com") {
		return ""
	}
	parts := strings.SplitN(gitURL, "github.com/", 2)
	if len(parts) != 2 {
		return ""
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	repo = strings.TrimSuffix(repo, "/")
	return repo
}

// checkProject checks GitHub Actions for a single project and creates P0 beads
// for any failing workflows that don't already have an open bead.
func (r *Runner) checkProject(ctx context.Context, projectID, repo, workDir, token string) error {
	client := github.NewClient(workDir, token)

	// List the most recent failed runs across all workflows.
	runs, err := client.ListFailedWorkflowRuns(ctx, "")
	if err != nil {
		// gh errors (no credentials, network, not a git repo) are non-fatal.
		log.Printf("[CIMon] %s: gh run list failed (check GH_TOKEN / gh auth): %v", projectID, err)
		return nil
	}
	if len(runs) == 0 {
		return nil
	}

	// Load open/in-progress beads once to check for duplicates.
	existing, err := r.beads.GetBeadsByProject(projectID)
	if err != nil {
		return fmt.Errorf("list beads: %w", err)
	}
	openTitles := make(map[string]bool, len(existing))
	for _, b := range existing {
		if b.Status == models.BeadStatusOpen || b.Status == models.BeadStatusInProgress {
			openTitles[b.Title] = true
		}
	}

	// One bead per failing workflow definition (not per run). Use WorkflowName
	// as the dedup key so repeated sweeps of the same failure don't create
	// duplicate beads. A new bead is only created once the previous one closes
	// (i.e., someone fixed and re-ran the workflow successfully).
	seenWorkflows := make(map[string]bool)
	for _, run := range runs {
		wfName := run.WorkflowName
		if wfName == "" {
			wfName = run.Name // fall back to displayTitle if workflowName not populated
		}
		if seenWorkflows[wfName] {
			continue // only one bead per workflow across runs
		}
		seenWorkflows[wfName] = true

		title := fmt.Sprintf("[ci-failure] %s: %s", repo, wfName)
		if openTitles[title] {
			continue // bead already open, skip
		}

		desc := buildDescription(repo, wfName, run)
		bead, err := r.beads.CreateBead(
			title,
			desc,
			models.BeadPriorityP0,
			"bug",
			projectID,
		)
		if err != nil {
			log.Printf("[CIMon] Failed to create bead for %s/%s: %v", repo, wfName, err)
			continue
		}
		openTitles[title] = true
		log.Printf("[CIMon] Filed P0 bead %s for CI failure: %s (%s)", bead.ID, wfName, repo)
	}
	return nil
}

// buildDescription produces the bead body for a CI failure.
func buildDescription(repo, workflowName string, run github.WorkflowRun) string {
	var sb strings.Builder
	sb.WriteString("## CI/CD Pipeline Failure\n\n")
	sb.WriteString(fmt.Sprintf("**Repository**: %s\n", repo))
	sb.WriteString(fmt.Sprintf("**Workflow**: %s\n", workflowName))
	if run.Name != "" && run.Name != workflowName {
		sb.WriteString(fmt.Sprintf("**Triggered by**: %s\n", run.Name))
	}
	sb.WriteString(fmt.Sprintf("**Status**: %s / **Conclusion**: %s\n", run.Status, run.Conclusion))
	if run.URL != "" {
		sb.WriteString(fmt.Sprintf("**Run URL**: %s\n", run.URL))
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString("Investigate the failed workflow and fix the root cause.\n")
	sb.WriteString("Steps:\n")
	sb.WriteString("1. Open the run URL above and read the failing step output\n")
	sb.WriteString("2. Fix the code, tests, or configuration causing the failure\n")
	sb.WriteString("3. Push the fix and confirm the workflow goes green\n")
	sb.WriteString("4. Close this bead once CI is passing\n\n")
	sb.WriteString("*Auto-filed by the CI/CD monitor. A new bead will be filed if the failure recurs after this one is closed.*\n")
	return sb.String()
}
