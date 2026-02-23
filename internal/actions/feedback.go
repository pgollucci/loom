package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxFileContentLen = 32000
	maxBuildOutputLen = 4000
	maxCommandOutput  = 6000
)

// FormatResultsAsUserMessage converts action execution results into a user message
// that can be fed back to the LLM for multi-turn action loops.
func FormatResultsAsUserMessage(results []Result) string {
	if len(results) == 0 {
		return "No actions were executed."
	}

	var sb strings.Builder
	sb.WriteString("## Action Results\n\n")

	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(formatSingleResult(r))
	}

	sb.WriteString("\n\nBased on these results, what would you like to do next?")
	return sb.String()
}

func formatSingleResult(r Result) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("### %s — %s\n", r.ActionType, r.Status))

	if r.Status == "error" {
		sb.WriteString(fmt.Sprintf("**Error:** %s\n", r.Message))
		// Phase 4: Specific recovery suggestions based on error type
		writeErrorSuggestion(&sb, r)
		return sb.String()
	}

	switch r.ActionType {
	case ActionReadCode, ActionReadFile:
		formatFileRead(&sb, r)
	case ActionWriteFile:
		formatFileWrite(&sb, r)
	case ActionEditCode, ActionApplyPatch:
		formatPatchApply(&sb, r)
	case ActionBuildProject:
		formatBuildResult(&sb, r)
	case ActionRunTests:
		formatTestResult(&sb, r)
	case ActionRunLinter:
		formatLintResult(&sb, r)
	case ActionSearchText:
		formatSearchResult(&sb, r)
	case ActionReadTree:
		formatTreeResult(&sb, r)
	case ActionGitStatus:
		formatGitOutput(&sb, r, "git status")
	case ActionGitDiff:
		formatGitOutput(&sb, r, "git diff")
	case ActionGitCommit:
		formatGitCommit(&sb, r)
	case ActionGitLog:
		formatGitOutput(&sb, r, "git log")
	case ActionRunCommand:
		formatCommandResult(&sb, r)
	case ActionCloseBead:
		sb.WriteString(fmt.Sprintf("Bead closed: %s\n", r.Message))
	case ActionCreateBead:
		formatBeadCreated(&sb, r)
	case ActionDone:
		sb.WriteString("Work complete signal acknowledged.\n")
	default:
		formatDefault(&sb, r)
	}

	return sb.String()
}

func formatFileRead(sb *strings.Builder, r Result) {
	path, _ := r.Metadata["path"].(string)
	content, _ := r.Metadata["content"].(string)
	size, _ := r.Metadata["size"].(float64)

	sb.WriteString(fmt.Sprintf("**File:** `%s` (%d bytes)\n", path, int(size)))

	if len(content) > maxFileContentLen {
		content = content[:maxFileContentLen] + "\n... (truncated)"
	}
	sb.WriteString("```\n")
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")
}

func formatFileWrite(sb *strings.Builder, r Result) {
	path, _ := r.Metadata["path"].(string)
	bytesWritten, _ := r.Metadata["bytes_written"].(float64)
	sb.WriteString(fmt.Sprintf("Written %d bytes to `%s`\n", int(bytesWritten), path))
}

func formatPatchApply(sb *strings.Builder, r Result) {
	output, _ := r.Metadata["output"].(string)
	sb.WriteString("Patch applied successfully.\n")
	if output != "" {
		sb.WriteString(fmt.Sprintf("Output: %s\n", output))
	}
}

func formatBuildResult(sb *strings.Builder, r Result) {
	if r.Metadata == nil {
		sb.WriteString(r.Message + "\n")
		return
	}

	success, _ := r.Metadata["success"].(bool)
	output, _ := r.Metadata["output"].(string)
	exitCode, _ := r.Metadata["exit_code"].(float64)

	if success {
		sb.WriteString("**Build: PASSED**\n")
	} else {
		sb.WriteString(fmt.Sprintf("**Build: FAILED** (exit code %d)\n", int(exitCode)))
	}

	if output != "" {
		// Extract and truncate build output, focusing on error lines
		truncated := truncateBuildOutput(output)
		sb.WriteString("```\n")
		sb.WriteString(truncated)
		if !strings.HasSuffix(truncated, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}

	if !success {
		sb.WriteString("\nPlease fix the build errors and rebuild.\n")
	}
}

func formatTestResult(sb *strings.Builder, r Result) {
	if r.Metadata == nil {
		sb.WriteString(r.Message + "\n")
		return
	}

	success, _ := r.Metadata["success"].(bool)
	output, _ := r.Metadata["output"].(string)
	passed, _ := r.Metadata["passed"].(float64)
	failed, _ := r.Metadata["failed"].(float64)

	if success {
		sb.WriteString(fmt.Sprintf("**Tests: PASSED** (%d passed)\n", int(passed)))
	} else {
		sb.WriteString(fmt.Sprintf("**Tests: FAILED** (%d passed, %d failed)\n", int(passed), int(failed)))
	}

	if output != "" && !success {
		truncated := truncateOutput(output, maxBuildOutputLen)
		sb.WriteString("```\n")
		sb.WriteString(truncated)
		if !strings.HasSuffix(truncated, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
		sb.WriteString("\nPlease fix the failing tests.\n")
	}
}

func formatLintResult(sb *strings.Builder, r Result) {
	if r.Metadata == nil {
		sb.WriteString(r.Message + "\n")
		return
	}

	output, _ := r.Metadata["output"].(string)
	if output != "" {
		truncated := truncateOutput(output, maxBuildOutputLen)
		sb.WriteString("```\n")
		sb.WriteString(truncated)
		if !strings.HasSuffix(truncated, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	} else {
		sb.WriteString("Lint passed with no issues.\n")
	}
}

func formatSearchResult(sb *strings.Builder, r Result) {
	matches := r.Metadata["matches"]
	if matches == nil {
		sb.WriteString("No matches found.\n")
		return
	}

	b, err := json.MarshalIndent(matches, "", "  ")
	if err != nil {
		sb.WriteString(fmt.Sprintf("Matches: %v\n", matches))
		return
	}

	output := string(b)
	if len(output) > maxFileContentLen {
		output = output[:maxFileContentLen] + "\n... (truncated)"
	}
	sb.WriteString("```json\n")
	sb.WriteString(output)
	sb.WriteString("\n```\n")
}

func formatTreeResult(sb *strings.Builder, r Result) {
	entries := r.Metadata["entries"]
	if entries == nil {
		sb.WriteString("Empty directory.\n")
		return
	}

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		sb.WriteString(fmt.Sprintf("Entries: %v\n", entries))
		return
	}

	output := string(b)
	if len(output) > maxFileContentLen {
		output = output[:maxFileContentLen] + "\n... (truncated)"
	}
	sb.WriteString("```json\n")
	sb.WriteString(output)
	sb.WriteString("\n```\n")
}

func formatGitOutput(sb *strings.Builder, r Result, label string) {
	output, _ := r.Metadata["output"].(string)
	if output == "" {
		sb.WriteString(fmt.Sprintf("%s: (empty)\n", label))
		return
	}

	truncated := truncateOutput(output, maxCommandOutput)
	sb.WriteString(fmt.Sprintf("**%s:**\n```\n", label))
	sb.WriteString(truncated)
	if !strings.HasSuffix(truncated, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")
}

func formatGitCommit(sb *strings.Builder, r Result) {
	sha, _ := r.Metadata["sha"].(string)
	message, _ := r.Metadata["message"].(string)
	if sha != "" {
		sb.WriteString(fmt.Sprintf("Commit created: `%s`\n", sha))
	}
	if message != "" {
		sb.WriteString(fmt.Sprintf("Message: %s\n", message))
	}
	if sha == "" && message == "" {
		sb.WriteString(r.Message + "\n")
	}
}

func formatCommandResult(sb *strings.Builder, r Result) {
	exitCode, _ := r.Metadata["exit_code"].(float64)
	stdout, _ := r.Metadata["stdout"].(string)
	stderr, _ := r.Metadata["stderr"].(string)

	sb.WriteString(fmt.Sprintf("**Exit code:** %d\n", int(exitCode)))

	if stdout != "" {
		truncated := truncateOutput(stdout, maxCommandOutput)
		sb.WriteString("**stdout:**\n```\n")
		sb.WriteString(truncated)
		if !strings.HasSuffix(truncated, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}

	if stderr != "" {
		truncated := truncateOutput(stderr, maxCommandOutput)
		sb.WriteString("**stderr:**\n```\n")
		sb.WriteString(truncated)
		if !strings.HasSuffix(truncated, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}
}

func formatBeadCreated(sb *strings.Builder, r Result) {
	beadID, _ := r.Metadata["bead_id"].(string)
	sb.WriteString(fmt.Sprintf("Created bead: `%s`\n", beadID))
}

func formatDefault(sb *strings.Builder, r Result) {
	sb.WriteString(r.Message + "\n")
	if r.Metadata != nil {
		b, err := json.MarshalIndent(r.Metadata, "", "  ")
		if err == nil {
			output := string(b)
			if len(output) > 2000 {
				output = output[:2000] + "..."
			}
			sb.WriteString("```json\n")
			sb.WriteString(output)
			sb.WriteString("\n```\n")
		}
	}
}

// truncateBuildOutput extracts the most useful portion of build output,
// prioritizing error lines and file:line locations.
func truncateBuildOutput(output string) string {
	if len(output) <= maxBuildOutputLen {
		return output
	}

	lines := strings.Split(output, "\n")
	var errorLines []string

	// First pass: collect lines with error indicators
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") ||
			strings.Contains(lower, "fail") ||
			strings.Contains(lower, "undefined") ||
			strings.Contains(lower, "cannot") ||
			// file:line:col patterns
			(len(line) > 0 && strings.Contains(line, ":") && len(strings.Split(line, ":")) >= 3) {
			errorLines = append(errorLines, line)
		}
	}

	if len(errorLines) > 0 {
		result := strings.Join(errorLines, "\n")
		if len(result) > maxBuildOutputLen {
			return result[:maxBuildOutputLen] + "\n... (truncated)"
		}
		return result
	}

	// No error lines found, just truncate from the end
	return output[len(output)-maxBuildOutputLen:] + "\n... (showing last portion)"
}

func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}

// writeErrorSuggestion provides specific recovery hints based on the error.
func writeErrorSuggestion(sb *strings.Builder, r Result) {
	msg := strings.ToLower(r.Message)

	switch {
	case strings.Contains(msg, "not found in") && r.ActionType == ActionEditCode:
		sb.WriteString("\n**Suggestion:** The OLD text didn't match the file content. Try:\n")
		sb.WriteString("1. READ the file first to see its current content\n")
		sb.WriteString("2. Copy the exact text from the READ output\n")
		sb.WriteString("3. Include 3-5 lines of surrounding context\n")

	case strings.Contains(msg, "no such file") || strings.Contains(msg, "does not exist"):
		sb.WriteString("\n**Suggestion:** File not found. Try:\n")
		sb.WriteString("1. Use SCOPE or TREE to see available files\n")
		sb.WriteString("2. Check that the path is relative to the project root\n")
		sb.WriteString("3. Use SEARCH to find the right file name\n")

	case strings.Contains(msg, "escapes project") || strings.Contains(msg, "must be relative"):
		sb.WriteString("\n**Suggestion:** Use relative paths from the project root, e.g. 'internal/actions/router.go'\n")

	case strings.Contains(msg, "commit blocked") && strings.Contains(msg, "toolchain not found"):
		sb.WriteString("\n**REQUIRED ACTION:** Install the missing toolchain using execute_command, " +
			"then verify the build passes before retrying git_commit. " +
			"DO NOT call done until the build is working.\n")

	case strings.Contains(msg, "commit blocked") && strings.Contains(msg, "build failed"):
		sb.WriteString("\n**REQUIRED ACTION:** Fix the build errors shown above. " +
			"Use execute_command to run the build and confirm it passes. " +
			"DO NOT call done or git_commit again until the build is clean.\n")

	case strings.Contains(msg, "build") && strings.Contains(msg, "fail"):
		sb.WriteString("\n**Suggestion:** Read the error output above, fix the issue, then BUILD again.\n")

	case strings.Contains(msg, "not cloned"):
		sb.WriteString("\n**Suggestion:** The project repository is not cloned locally. This may be a configuration issue.\n")

	default:
		sb.WriteString("Consider adjusting your approach based on this error.\n")
	}
}

// FormatResultsWithContext enhances feedback with project spatial context.
// Phase 3: Every response reminds the agent where it is and what's available.
func FormatResultsWithContext(results []Result, projectRoot string) string {
	base := FormatResultsAsUserMessage(results)

	if projectRoot != "" {
		base += fmt.Sprintf("\n\n**Working directory:** `%s`\n", projectRoot)
	}

	return base
}
