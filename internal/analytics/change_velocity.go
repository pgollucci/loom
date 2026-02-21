package analytics

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/internal/database"
)

// ChangeVelocityMetrics tracks the full development funnel from code changes to commits
type ChangeVelocityMetrics struct {
	ProjectID        string    `json:"project_id"`
	TimeWindow       string    `json:"time_window"`
	FilesModified    int       `json:"files_modified"`
	BuildsAttempted  int       `json:"builds_attempted"`
	BuildsSucceeded  int       `json:"builds_succeeded"`
	TestsAttempted   int       `json:"tests_attempted"`
	TestsPassed      int       `json:"tests_passed"`
	CommitsAttempted int       `json:"commits_attempted"`
	CommitsSucceeded int       `json:"commits_succeeded"`
	PushesAttempted  int       `json:"pushes_attempted"`
	PushesSucceeded  int       `json:"pushes_succeeded"`
	UncommittedFiles []string  `json:"uncommitted_files"`
	LastCommitTime   time.Time `json:"last_commit_time"`
	ChangeVelocity   float64   `json:"change_velocity"` // commits per day
	Funnel           *Funnel   `json:"funnel"`
}

// Funnel represents the conversion funnel from edits to pushes
type Funnel struct {
	Edits            int     `json:"edits"`
	Builds           int     `json:"builds"`
	Tests            int     `json:"tests"`
	Commits          int     `json:"commits"`
	Pushes           int     `json:"pushes"`
	EditToCommitRate float64 `json:"edit_to_commit_rate"` // commits/edits
	CommitToPushRate float64 `json:"commit_to_push_rate"` // pushes/commits
}

// ChangeVelocityTracker tracks change velocity metrics
type ChangeVelocityTracker struct {
	db *database.Database
}

// NewChangeVelocityTracker creates a new change velocity tracker
func NewChangeVelocityTracker(db *database.Database) *ChangeVelocityTracker {
	return &ChangeVelocityTracker{db: db}
}

// GetChangeVelocity retrieves change velocity metrics for a project
func (t *ChangeVelocityTracker) GetChangeVelocity(ctx context.Context, projectID string, timeWindow time.Duration) (*ChangeVelocityMetrics, error) {
	if t.db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	metrics := &ChangeVelocityMetrics{
		ProjectID:  projectID,
		TimeWindow: formatDuration(timeWindow),
	}

	// Query action statistics from database
	// Note: Using GetStats from analytics package would be better, but for now
	// we'll skip database queries since we don't have the action_logs table yet
	// This can be implemented once the schema is added

	// For now, return metrics based on git status only
	metrics.FilesModified = 0 // Would come from action_logs
	metrics.BuildsAttempted = 0
	metrics.BuildsSucceeded = 0
	metrics.TestsAttempted = 0
	metrics.TestsPassed = 0
	metrics.CommitsAttempted = 0
	metrics.CommitsSucceeded = 0
	metrics.PushesAttempted = 0
	metrics.PushesSucceeded = 0

	// TODO: Query from action_logs table once schema is added:
	// rows, err := t.db.db.Query(query, projectID, since)

	// Get uncommitted files from git status
	uncommittedFiles, err := getUncommittedFiles(projectID)
	if err == nil {
		metrics.UncommittedFiles = uncommittedFiles
	}

	// Get last commit time
	lastCommitTime, err := getLastCommitTime(projectID)
	if err == nil {
		metrics.LastCommitTime = lastCommitTime
	}

	// Calculate change velocity (commits per day)
	if timeWindow.Hours() > 0 {
		days := timeWindow.Hours() / 24
		metrics.ChangeVelocity = float64(metrics.CommitsSucceeded) / days
	}

	// Build funnel
	metrics.Funnel = &Funnel{
		Edits:   metrics.FilesModified,
		Builds:  metrics.BuildsSucceeded,
		Tests:   metrics.TestsPassed,
		Commits: metrics.CommitsSucceeded,
		Pushes:  metrics.PushesSucceeded,
	}

	if metrics.FilesModified > 0 {
		metrics.Funnel.EditToCommitRate = float64(metrics.CommitsSucceeded) / float64(metrics.FilesModified)
	}

	if metrics.CommitsSucceeded > 0 {
		metrics.Funnel.CommitToPushRate = float64(metrics.PushesSucceeded) / float64(metrics.CommitsSucceeded)
	}

	return metrics, nil
}

// getUncommittedFiles returns a list of uncommitted files in the project
func getUncommittedFiles(projectID string) ([]string, error) {
	// For now, assume project directory is at /app/projects/{projectID}
	// This should be configurable via project settings
	projectPath := fmt.Sprintf("/app/projects/%s", projectID)

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git status failed: %w", err)
	}

	if len(output) == 0 {
		return []string{}, nil
	}

	lines := strings.Split(string(output), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		// Format: "XY filename" where X is index status, Y is work tree status
		file := strings.TrimSpace(line[3:])
		if file != "" {
			files = append(files, file)
		}
	}

	return files, nil
}

// getLastCommitTime returns the timestamp of the last commit in the project
func getLastCommitTime(projectID string) (time.Time, error) {
	projectPath := fmt.Sprintf("/app/projects/%s", projectID)

	cmd := exec.Command("git", "log", "-1", "--format=%ct")
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return time.Time{}, fmt.Errorf("git log failed: %w", err)
	}

	timestamp := strings.TrimSpace(string(output))
	if timestamp == "" {
		return time.Time{}, fmt.Errorf("no commits found")
	}

	var unixTime int64
	_, err = fmt.Sscanf(timestamp, "%d", &unixTime)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp failed: %w", err)
	}

	return time.Unix(unixTime, 0), nil
}

// formatDuration formats a duration as a human-readable string
func formatDuration(d time.Duration) string {
	hours := d.Hours()
	if hours < 1 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if hours < 24 {
		return fmt.Sprintf("%dh", int(hours))
	}
	days := int(hours / 24)
	if days == 1 {
		return "24h"
	}
	return fmt.Sprintf("%dd", days)
}
