package dispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jordanhubbard/loom/pkg/models"
)

// ActionRecord represents a single action taken by an agent
type ActionRecord struct {
	Timestamp   time.Time              `json:"timestamp"`
	AgentID     string                 `json:"agent_id"`
	ActionType  string                 `json:"action_type"`  // e.g., "read_file", "run_tests", "edit_file"
	ActionData  map[string]interface{} `json:"action_data"`  // Specific details
	ResultHash  string                 `json:"result_hash"`  // Hash of action result
	ProgressKey string                 `json:"progress_key"` // Key identifying the action pattern
}

// ProgressMetrics tracks progress indicators for a bead
type ProgressMetrics struct {
	FilesRead        int       `json:"files_read"`
	FilesModified    int       `json:"files_modified"`
	TestsRun         int       `json:"tests_run"`
	CommandsExecuted int       `json:"commands_executed"`
	LastProgress     time.Time `json:"last_progress"`
}

// LoopDetector detects stuck loops vs. productive investigation
type LoopDetector struct {
	repeatThreshold int // Number of identical action sequences before flagging as loop
}

// NewLoopDetector creates a new loop detector with default settings
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{
		repeatThreshold: 3, // Flag as loop after 3 identical sequences
	}
}

// SetRepeatThreshold configures how many repeats before flagging as a loop
func (ld *LoopDetector) SetRepeatThreshold(threshold int) {
	if threshold < 2 {
		threshold = 2
	}
	ld.repeatThreshold = threshold
}

// RecordAction adds an action to the bead's dispatch history
func (ld *LoopDetector) RecordAction(bead *models.Bead, action ActionRecord) error {
	if bead.Context == nil {
		bead.Context = make(map[string]string)
	}

	// Generate a progress key for this action type
	action.ProgressKey = ld.generateProgressKey(action)

	// Get existing action history
	history, err := ld.getActionHistory(bead)
	if err != nil {
		log.Printf("[LoopDetector] Failed to parse action history for bead %s: %v", bead.ID, err)
		history = []ActionRecord{}
	}

	// Append new action
	history = append(history, action)

	// Keep only recent history (last 50 actions)
	if len(history) > 50 {
		history = history[len(history)-50:]
	}

	// Store back in bead context
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal action history: %w", err)
	}
	bead.Context["action_history"] = string(historyJSON)

	// Update progress metrics
	ld.updateProgressMetrics(bead, action)

	return nil
}

// IsStuckInLoop checks if the bead is stuck in a non-productive loop
func (ld *LoopDetector) IsStuckInLoop(bead *models.Bead) (bool, string) {
	// CRITICAL: Check for repeated infrastructure errors FIRST
	// These are hard failures that will never succeed
	if stuck, reason := ld.checkRepeatedErrors(bead); stuck {
		return true, reason
	}

	history, err := ld.getActionHistory(bead)
	if err != nil || len(history) < ld.repeatThreshold*2 {
		// Not enough history to detect a loop
		return false, ""
	}

	// Check for progress in recent history
	if ld.hasRecentProgress(bead) {
		// Making progress, not stuck
		return false, ""
	}

	// Look for repeated action patterns
	pattern, count := ld.findRepeatedPattern(history)
	if count >= ld.repeatThreshold {
		reason := fmt.Sprintf("Repeated action pattern %d times without progress: %s", count, pattern)
		return true, reason
	}

	return false, ""
}

// checkRepeatedErrors detects repeated infrastructure errors
// This catches authentication failures, provider errors, and other hard failures
func (ld *LoopDetector) checkRepeatedErrors(bead *models.Bead) (bool, string) {
	if bead.Context == nil {
		return false, ""
	}

	// Get dispatch count
	dispatchCount := 0
	if countStr := bead.Context["dispatch_count"]; countStr != "" {
		fmt.Sscanf(countStr, "%d", &dispatchCount)
	}

	// Need at least 5 attempts to detect error pattern
	if dispatchCount < 5 {
		return false, ""
	}

	// Get last error
	lastError := bead.Context["last_run_error"]
	if lastError == "" {
		return false, ""
	}

	// Get error history from context
	errorHistory := ld.getErrorHistory(bead)

	// Add current error to history
	errorHistory = append(errorHistory, ErrorRecord{
		Timestamp: time.Now(),
		Error:     lastError,
		Dispatch:  dispatchCount,
	})

	// Keep last 20 errors
	if len(errorHistory) > 20 {
		errorHistory = errorHistory[len(errorHistory)-20:]
	}

	// Save error history
	ld.saveErrorHistory(bead, errorHistory)

	// Check for repeated error patterns
	if len(errorHistory) < 5 {
		return false, ""
	}

	// Check recent errors (last 10)
	recentErrors := errorHistory
	if len(recentErrors) > 10 {
		recentErrors = recentErrors[len(recentErrors)-10:]
	}

	// Detect specific error patterns
	authErrors := 0
	providerErrors := 0
	rateLimitErrors := 0
	sameErrorCount := 0
	lastErrorPattern := ""

	for _, errRec := range recentErrors {
		// Authentication errors (401, 403)
		if contains(errRec.Error, "401") || contains(errRec.Error, "Authentication") ||
			contains(errRec.Error, "403") || contains(errRec.Error, "Forbidden") ||
			contains(errRec.Error, "No api key") {
			authErrors++
		}

		// Rate limiting (429)
		if contains(errRec.Error, "429") || contains(errRec.Error, "rate limit") ||
			contains(errRec.Error, "Rate limit") {
			rateLimitErrors++
		}

		// Provider/infrastructure errors (500, 502, 503, 504)
		if contains(errRec.Error, "500") || contains(errRec.Error, "502") ||
			contains(errRec.Error, "503") || contains(errRec.Error, "504") ||
			contains(errRec.Error, "Internal Server Error") ||
			contains(errRec.Error, "Bad Gateway") ||
			contains(errRec.Error, "Service Unavailable") {
			providerErrors++
		}

		// Count identical errors
		if errRec.Error == lastErrorPattern {
			sameErrorCount++
		} else {
			lastErrorPattern = errRec.Error
			sameErrorCount = 1
		}
	}

	// Hard failure conditions
	if authErrors >= 3 {
		return true, fmt.Sprintf("Repeated authentication errors (%d attempts) - provider credentials invalid or missing", authErrors)
	}

	if providerErrors >= 5 {
		return true, fmt.Sprintf("Repeated provider errors (%d attempts) - provider unavailable or unhealthy", providerErrors)
	}

	if rateLimitErrors >= 5 {
		return true, fmt.Sprintf("Repeated rate limit errors (%d attempts) - exhausted provider quota", rateLimitErrors)
	}

	if sameErrorCount >= 5 {
		return true, fmt.Sprintf("Identical error repeated %d times - error pattern: %.100s", sameErrorCount, lastErrorPattern)
	}

	return false, ""
}

// ErrorRecord tracks an error occurrence
type ErrorRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error"`
	Dispatch  int       `json:"dispatch"`
}

// getErrorHistory retrieves error history from bead context
func (ld *LoopDetector) getErrorHistory(bead *models.Bead) []ErrorRecord {
	if bead.Context == nil {
		return []ErrorRecord{}
	}

	historyJSON := bead.Context["error_history"]
	if historyJSON == "" {
		return []ErrorRecord{}
	}

	var history []ErrorRecord
	if err := json.Unmarshal([]byte(historyJSON), &history); err != nil { log.Printf("[LoopDetector] Failed to unmarshal error_history for bead: %v", err) }
		log.Printf("[LoopDetector] Failed to unmarshal error_history for bead: %v", err)
		return []ErrorRecord{}
	}

	return history
}

// saveErrorHistory saves error history to bead context
func (ld *LoopDetector) saveErrorHistory(bead *models.Bead, history []ErrorRecord) {
	if bead.Context == nil {
		bead.Context = make(map[string]string)
	}

	historyJSON, err := json.Marshal(history)
	if err == nil {
		bead.Context["error_history"] = string(historyJSON)
	}
}

// contains checks if a string contains a substring (case-insensitive check would be better)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

// findSubstring does a simple substring search
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getActionHistory retrieves the action history from bead context
func (ld *LoopDetector) getActionHistory(bead *models.Bead) ([]ActionRecord, error) {
	if bead.Context == nil {
		return []ActionRecord{}, nil
	}

	historyJSON := bead.Context["action_history"]
	if historyJSON == "" {
		return []ActionRecord{}, nil
	}

	var history []ActionRecord
	if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
		return nil, err
	}

	return history, nil
}

// generateProgressKey creates a key that identifies the action pattern
func (ld *LoopDetector) generateProgressKey(action ActionRecord) string {
	// Create a signature for this action type and key data
	// This allows us to detect when the same action is repeated
	keyData := fmt.Sprintf("%s:%v", action.ActionType, action.ActionData)

	// For file operations, include the file path
	if filePath, ok := action.ActionData["file_path"].(string); ok {
		keyData = fmt.Sprintf("%s:%s", action.ActionType, filePath)
	}

	// For commands, include the command
	if command, ok := action.ActionData["command"].(string); ok {
		keyData = fmt.Sprintf("%s:%s", action.ActionType, command)
	}

	// Hash to keep it short
	hash := sha256.Sum256([]byte(keyData))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes (16 hex chars)
}

// findRepeatedPattern looks for repeated action sequences
func (ld *LoopDetector) findRepeatedPattern(history []ActionRecord) (string, int) {
	if len(history) < ld.repeatThreshold {
		return "", 0
	}

	// Look at recent history (last 15 actions)
	recent := history
	if len(recent) > 15 {
		recent = recent[len(recent)-15:]
	}

	// Count consecutive identical progress keys
	patternCounts := make(map[string]int)
	var lastKey string
	consecutiveCount := 0

	for _, action := range recent {
		if action.ProgressKey == lastKey {
			consecutiveCount++
		} else {
			if consecutiveCount >= ld.repeatThreshold {
				patternCounts[lastKey] = consecutiveCount
			}
			lastKey = action.ProgressKey
			consecutiveCount = 1
		}
	}

	// Check last sequence
	if consecutiveCount >= ld.repeatThreshold {
		patternCounts[lastKey] = consecutiveCount
	}

	// Find the pattern with highest repeat count
	maxCount := 0
	maxPattern := ""
	for pattern, count := range patternCounts {
		if count > maxCount {
			maxCount = count
			maxPattern = pattern
		}
	}

	return maxPattern, maxCount
}

// hasRecentProgress checks if there has been any progress recently
func (ld *LoopDetector) hasRecentProgress(bead *models.Bead) bool {
	if bead.Context == nil {
		return false
	}

	metricsJSON := bead.Context["progress_metrics"]
	if metricsJSON == "" {
		return false
	}

	var metrics ProgressMetrics
	if err := json.Unmarshal([]byte(metricsJSON), &metrics); err != nil {
		log.Printf("[LoopDetector] Failed to unmarshal progress_metrics for bead: %v", err)
		return false
	}

	// Consider it progress only if metrics have increased recently (within last 5 minutes)
	// The LastProgress timestamp indicates when the last meaningful action was taken
	if metrics.LastProgress.IsZero() {
		return false
	}

	timeSinceProgress := time.Since(metrics.LastProgress)
	return timeSinceProgress < 5*time.Minute
}

// updateProgressMetrics updates progress tracking based on action
func (ld *LoopDetector) updateProgressMetrics(bead *models.Bead, action ActionRecord) {
	if bead.Context == nil {
		bead.Context = make(map[string]string)
	}

	// Get existing metrics
	var metrics ProgressMetrics
	if metricsJSON := bead.Context["progress_metrics"]; metricsJSON != "" {
		_ = json.Unmarshal([]byte(metricsJSON), &metrics)
	}

	// Update metrics based on action type.
	// Only mutations and completions count as real progress.
	// Read-only exploration actions (read_file, glob, grep) do NOT advance
	// LastProgress — repeating them endlessly is the definition of being stuck.
	progressMade := false
	switch action.ActionType {
	case "read_file", "glob", "grep", "search_text", "read_tree":
		// Read-only: track for stats but do NOT update LastProgress
		metrics.FilesRead++
	case "edit_file", "write_file", "create_file":
		metrics.FilesModified++
		progressMade = true
	case "run_tests", "test":
		metrics.TestsRun++
		progressMade = true
	case "bash", "execute", "run_command":
		metrics.CommandsExecuted++
		progressMade = true
	case "git_commit", "git_push", "done", "close_bead":
		progressMade = true
	}

	if progressMade {
		metrics.LastProgress = time.Now()
	}

	// Store updated metrics
	metricsJSON, err := json.Marshal(metrics)
	if err == nil {
		bead.Context["progress_metrics"] = string(metricsJSON)
	}
}

// GetProgressSummary returns a human-readable progress summary
func (ld *LoopDetector) GetProgressSummary(bead *models.Bead) string {
	if bead.Context == nil {
		return "No progress data"
	}

	metricsJSON := bead.Context["progress_metrics"]
	if metricsJSON == "" {
		return "No progress data"
	}

	var metrics ProgressMetrics
	if err := json.Unmarshal([]byte(metricsJSON), &metrics); err != nil {
		return "Invalid progress data"
	}

	timeSince := "never"
	if !metrics.LastProgress.IsZero() {
		timeSince = time.Since(metrics.LastProgress).Round(time.Second).String() + " ago"
	}

	return fmt.Sprintf("Files read: %d, modified: %d, tests: %d, commands: %d (last: %s)",
		metrics.FilesRead, metrics.FilesModified, metrics.TestsRun,
		metrics.CommandsExecuted, timeSince)
}

// ResetProgress clears progress tracking for a bead
func (ld *LoopDetector) ResetProgress(bead *models.Bead) {
	if bead.Context != nil {
		delete(bead.Context, "action_history")
		delete(bead.Context, "progress_metrics")
	}
}

// GetAgentCommitRange returns the first and last commit SHAs for the current
// agent's dispatch cycle by reading the dispatch_count and commit metadata
// from bead context.
func (ld *LoopDetector) GetAgentCommitRange(bead *models.Bead) (firstSHA, lastSHA string, count int) {
	if bead == nil || bead.Context == nil {
		return "", "", 0
	}

	// Try to get commit range from context (set by dispatch pipeline)
	firstSHA = bead.Context["agent_first_commit_sha"]
	lastSHA = bead.Context["agent_last_commit_sha"]

	if countStr := bead.Context["agent_commit_count"]; countStr != "" {
		_, _ = fmt.Sscanf(countStr, "%d", &count)
	}

	return firstSHA, lastSHA, count
}

// GetActionHistoryJSON returns the action history as JSON string
func (ld *LoopDetector) GetActionHistoryJSON(bead *models.Bead) string {
	history, err := ld.getActionHistory(bead)
	if err != nil || len(history) == 0 {
		return "[]"
	}

	historyJSON, err := json.Marshal(history)
	if err != nil {
		return "[]"
	}

	return string(historyJSON)
}

// SuggestNextSteps analyzes the situation and suggests actions for the CEO/human
func (ld *LoopDetector) SuggestNextSteps(bead *models.Bead, loopReason string) []string {
	suggestions := []string{}

	history, err := ld.getActionHistory(bead)
	if err != nil || len(history) == 0 {
		suggestions = append(suggestions, "Review bead description and requirements")
		suggestions = append(suggestions, "Provide more specific context or constraints")
		return suggestions
	}

	// Analyze what actions were taken
	actionTypes := make(map[string]int)
	uniqueFiles := make(map[string]bool)

	for _, action := range history {
		actionTypes[action.ActionType]++

		if filePath, ok := action.ActionData["file_path"].(string); ok {
			uniqueFiles[filePath] = true
		}
	}

	// Determine what's missing or problematic
	hasReads := actionTypes["read_file"] > 0 || actionTypes["glob"] > 0 || actionTypes["grep"] > 0
	hasEdits := actionTypes["edit_file"] > 0 || actionTypes["write_file"] > 0
	hasTests := actionTypes["run_tests"] > 0 || actionTypes["test"] > 0
	hasCommands := actionTypes["bash"] > 0 || actionTypes["execute"] > 0

	// Generate suggestions based on what was attempted
	if !hasReads {
		suggestions = append(suggestions, "Agent hasn't explored the codebase - provide file locations or entry points")
	} else if len(uniqueFiles) == 1 {
		suggestions = append(suggestions, "Agent focused on single file - suggest additional files to examine")
	}

	if hasReads && !hasEdits {
		suggestions = append(suggestions, "Agent read files but made no changes - clarify what needs to be modified")
	}

	if hasEdits && !hasTests {
		suggestions = append(suggestions, "Agent made changes but didn't run tests - specify test commands or verification steps")
	}

	if !hasCommands {
		suggestions = append(suggestions, "Agent didn't run any commands - provide build/test/debug commands")
	}

	if len(suggestions) == 0 {
		// Agent tried many things but still stuck
		suggestions = append(suggestions, "Agent attempted multiple approaches - problem may require domain expertise")
		suggestions = append(suggestions, "Review error messages and test failures for missing dependencies or configuration")
		suggestions = append(suggestions, "Consider if the task description is clear and achievable")
	}

	// Always add general suggestions
	suggestions = append(suggestions, "Provide specific examples or reference implementations")
	suggestions = append(suggestions, "Break down the task into smaller, more focused subtasks")

	return suggestions
}
