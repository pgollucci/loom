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

// IsProgressStagnant detects if the agent is looping without making meaningful progress, including handling meta-remediation loops.
// Returns true if the agent appears stuck, along with a reason.
func (pt *ProgressTracker) IsProgressStagnant(iteration int, actionTypeCount map[string]int) (bool, string) {
	// Idempotent actions can be detected early — directory listings never
	// change within a single agent run, so repeating scope/tree is always
	// a sign of a stuck agent regardless of how many iterations have passed.
	if treeCount := actionTypeCount["read_tree"]; treeCount > 5 {
		return true, fmt.Sprintf("repeated read_tree action %d times (directory listings don't change)", treeCount)
	}

	// Not enough iterations to judge the remaining heuristics yet
	if iteration < 15 {
		return false, ""
	}

	// Check 1: No files written after significant iterations
	// Raised threshold from 20 to 35 — diagnostic and audit beads are
	// legitimately read-only for many iterations before taking action.
	if iteration > 35 && len(pt.filesWritten) == 0 {
		return true, "no files modified after 35+ iterations"
	}

	// Check 2: Build/test status not improving
	if iteration > 25 && pt.buildStatus == "fail" {
		// Count how many build attempts
		buildAttempts := actionTypeCount["build"]
		if buildAttempts > 5 {
			return true, fmt.Sprintf("build failing after %d attempts", buildAttempts)
		}
	}

	// Check 3: Excessive reading without writing (analysis paralysis)
	if len(pt.filesRead) > 15 && len(pt.filesWritten) == 0 {
		return true, fmt.Sprintf("read %d files but wrote none", len(pt.filesRead))
	}

	// Check 4: Repeated test failures without fixes
	if pt.testStatus == "fail" && actionTypeCount["test"] > 5 && len(pt.filesWritten) < 2 {
		return true, "tests failing repeatedly without attempting fixes"
	}

	// Check 5: Same action type dominating (likely searching/reading same thing)
	// Uses canonical action type names (post-ParseSimpleJSON), not simple-mode verbs.
	for actionType, count := range actionTypeCount {
		if count > 15 && (actionType == "search_text" || actionType == "read_file" ||
			actionType == "read_code" || actionType == "run_command") {
			return true, fmt.Sprintf("repeated %s action %d times", actionType, count)
		}
	}

	return false, ""
}

// GetProgressMetrics returns metrics about agent progress for remediation analysis
func (pt *ProgressTracker) GetProgressMetrics() map[string]interface{} {
	return map[string]interface{}{
		"files_read_count":    len(pt.filesRead),
		"files_written_count": len(pt.filesWritten),
		"build_status":        pt.buildStatus,
		"test_status":         pt.testStatus,
		"committed":           pt.committed,
		"pushed":              pt.pushed,
		"error_count":         pt.errorCount,
		"beads_created":       pt.beadsCreated,
		"beads_closed":        pt.beadsClosed,
		"files_read":          keys(pt.filesRead),
		"files_written":       keys(pt.filesWritten),
	}
}

func keys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}
