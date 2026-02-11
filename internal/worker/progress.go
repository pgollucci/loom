package worker

import (
	"fmt"
	"strings"

	"github.com/jordanhubbard/loom/internal/actions"
)

// ProgressTracker accumulates state across action loop iterations so the LLM
// can see a compact progress summary even when older conversation messages
// have been truncated away.
type ProgressTracker struct {
	maxIterations int
	filesRead     map[string]bool
	filesWritten  map[string]bool
	buildStatus   string // "", "pass", "fail"
	testStatus    string // "", "pass", "fail"
	committed     bool
	pushed        bool
	errorCount    int
	beadsCreated  int
	beadsClosed   int
}

// NewProgressTracker creates a tracker for a loop with the given max iterations.
func NewProgressTracker(maxIterations int) *ProgressTracker {
	return &ProgressTracker{
		maxIterations: maxIterations,
		filesRead:     make(map[string]bool),
		filesWritten:  make(map[string]bool),
	}
}

// Update processes one iteration's action results and updates tracked state.
func (pt *ProgressTracker) Update(iteration int, results []actions.Result) {
	for _, r := range results {
		switch r.ActionType {
		case actions.ActionReadCode, actions.ActionReadFile:
			if path, _ := r.Metadata["path"].(string); path != "" {
				pt.filesRead[path] = true
			}
		case actions.ActionWriteFile:
			if path, _ := r.Metadata["path"].(string); path != "" {
				pt.filesWritten[path] = true
			}
		case actions.ActionEditCode, actions.ActionApplyPatch:
			if path, _ := r.Metadata["path"].(string); path != "" {
				pt.filesWritten[path] = true
			}
		case actions.ActionBuildProject:
			if r.Status == "error" || (r.Metadata != nil && r.Metadata["success"] == false) {
				pt.buildStatus = "fail"
			} else {
				pt.buildStatus = "pass"
			}
		case actions.ActionRunTests:
			if r.Status == "error" || (r.Metadata != nil && r.Metadata["success"] == false) {
				pt.testStatus = "fail"
			} else {
				pt.testStatus = "pass"
			}
		case actions.ActionGitCommit:
			if r.Status != "error" {
				pt.committed = true
			}
		case actions.ActionGitPush:
			if r.Status != "error" {
				pt.pushed = true
			}
		case actions.ActionCreateBead:
			if r.Status != "error" {
				pt.beadsCreated++
			}
		case actions.ActionCloseBead:
			if r.Status != "error" {
				pt.beadsClosed++
			}
		}
		if r.Status == "error" {
			pt.errorCount++
		}
	}
}

// Summary returns a compact markdown block summarizing progress so far.
// Designed to survive context truncation by being prepended to every feedback.
func (pt *ProgressTracker) Summary(iteration int) string {
	var sb strings.Builder
	sb.WriteString("## Your Progress\n")
	fmt.Fprintf(&sb, "Iteration %d/%d", iteration, pt.maxIterations)

	var items []string
	if len(pt.filesRead) > 0 {
		items = append(items, fmt.Sprintf("read %d files", len(pt.filesRead)))
	}
	if len(pt.filesWritten) > 0 {
		items = append(items, fmt.Sprintf("wrote %d files", len(pt.filesWritten)))
	}
	if pt.buildStatus != "" {
		items = append(items, fmt.Sprintf("build: %s", pt.buildStatus))
	}
	if pt.testStatus != "" {
		items = append(items, fmt.Sprintf("tests: %s", pt.testStatus))
	}
	if pt.committed {
		items = append(items, "committed")
	}
	if pt.pushed {
		items = append(items, "pushed")
	}
	if pt.beadsCreated > 0 {
		items = append(items, fmt.Sprintf("%d beads created", pt.beadsCreated))
	}
	if pt.beadsClosed > 0 {
		items = append(items, fmt.Sprintf("%d beads closed", pt.beadsClosed))
	}
	if pt.errorCount > 0 {
		items = append(items, fmt.Sprintf("%d errors", pt.errorCount))
	}

	if len(items) > 0 {
		sb.WriteString(" | ")
		sb.WriteString(strings.Join(items, ", "))
	}

	sb.WriteString("\n\n")

	// List written files so the model doesn't re-write them
	if len(pt.filesWritten) > 0 {
		sb.WriteString("Files modified: ")
		files := make([]string, 0, len(pt.filesWritten))
		for f := range pt.filesWritten {
			files = append(files, f)
		}
		sb.WriteString(strings.Join(files, ", "))
		sb.WriteString("\n\n")
	}

	return sb.String()
}
