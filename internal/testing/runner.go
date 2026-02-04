package testing

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TestStatus represents the status of a test case
type TestStatus string

const (
	TestPass TestStatus = "pass"
	TestFail TestStatus = "fail"
	TestSkip TestStatus = "skip"
)

// TestCase represents a single test result
type TestCase struct {
	Name       string        `json:"name"`        // Test name/identifier
	Package    string        `json:"package"`     // Package/file path
	Status     TestStatus    `json:"status"`      // pass/fail/skip
	Duration   time.Duration `json:"duration"`    // Individual test time
	Output     string        `json:"output"`      // Test-specific output
	Error      string        `json:"error"`       // Error message if failed
	StackTrace string        `json:"stack_trace"` // Stack trace if available
}

// TestSummary provides aggregate statistics
type TestSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// TestResult contains the complete test execution result
type TestResult struct {
	Framework string        `json:"framework"`  // "go", "jest", "pytest", etc.
	Success   bool          `json:"success"`    // Overall pass/fail
	Duration  time.Duration `json:"duration"`   // Total execution time
	Tests     []TestCase    `json:"tests"`      // Individual test results
	Summary   TestSummary   `json:"summary"`    // Aggregate statistics
	RawOutput string        `json:"raw_output"` // Full command output
	ExitCode  int           `json:"exit_code"`  // Process exit code
	TimedOut  bool          `json:"timed_out"`  // Whether execution timed out
	Error     string        `json:"error"`      // Error message if execution failed
}

// TestRequest defines parameters for test execution
type TestRequest struct {
	ProjectPath  string            // Absolute path to project
	TestCommand  string            // Optional: override test command
	Framework    string            // Optional: specify framework (auto-detect if empty)
	TestPattern  string            // Optional: run specific tests (e.g., "TestFoo*")
	Environment  map[string]string // Environment variables
	Timeout      time.Duration     // Max execution time
	StreamOutput bool              // Whether to stream output in real-time
}

// OutputStreamer provides real-time test output
type OutputStreamer interface {
	Write(line string) error
	Close() error
}

const (
	// DefaultTestTimeout is the default maximum test execution time
	DefaultTestTimeout = 10 * time.Minute
	// MaxTestTimeout is the absolute maximum allowed timeout
	MaxTestTimeout = 30 * time.Minute
)

// TestRunner executes tests and parses results
type TestRunner struct {
	workDir  string
	streamer OutputStreamer
}

// NewTestRunner creates a new TestRunner instance
func NewTestRunner(workDir string) *TestRunner {
	return &TestRunner{
		workDir: workDir,
	}
}

// SetOutputStreamer sets the output streamer for real-time test output
func (r *TestRunner) SetOutputStreamer(streamer OutputStreamer) {
	r.streamer = streamer
}

// Run executes tests and returns structured results
func (r *TestRunner) Run(ctx context.Context, req TestRequest) (*TestResult, error) {
	// Validate request
	if req.ProjectPath == "" {
		req.ProjectPath = r.workDir
	}

	// Validate timeout
	if req.Timeout == 0 {
		req.Timeout = DefaultTestTimeout
	} else if req.Timeout > MaxTestTimeout {
		req.Timeout = MaxTestTimeout
	}

	// Auto-detect framework if not specified
	framework := req.Framework
	if framework == "" {
		detected, err := r.DetectFramework(req.ProjectPath)
		if err != nil {
			return nil, fmt.Errorf("failed to detect test framework: %w", err)
		}
		framework = detected
	}

	// Build test command
	cmdArgs, err := r.BuildCommand(framework, req.ProjectPath, req.TestPattern, req.TestCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to build test command: %w", err)
	}

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	// Execute tests
	startTime := time.Now()
	output, exitCode, timedOut, err := r.executeCommand(timeoutCtx, cmdArgs, req.ProjectPath, req.Environment)
	duration := time.Since(startTime)

	// If execution failed completely, return error result
	if err != nil && !timedOut {
		return &TestResult{
			Framework: framework,
			Success:   false,
			Duration:  duration,
			RawOutput: output,
			ExitCode:  exitCode,
			TimedOut:  false,
			Error:     err.Error(),
		}, nil
	}

	// Parse output based on framework
	result, err := r.parseOutput(framework, output, exitCode)
	if err != nil {
		// If parsing fails, return a basic result with raw output
		return &TestResult{
			Framework: framework,
			Success:   exitCode == 0,
			Duration:  duration,
			RawOutput: output,
			ExitCode:  exitCode,
			TimedOut:  timedOut,
			Error:     fmt.Sprintf("failed to parse output: %v", err),
		}, nil
	}

	// Update result with execution details
	result.Duration = duration
	result.TimedOut = timedOut

	return result, nil
}

// DetectFramework auto-detects the test framework based on project structure
func (r *TestRunner) DetectFramework(projectPath string) (string, error) {
	// Check for Go
	if r.fileExists(filepath.Join(projectPath, "go.mod")) {
		return "go", nil
	}

	// Check for test files
	matches, _ := filepath.Glob(filepath.Join(projectPath, "*_test.go"))
	if len(matches) > 0 {
		return "go", nil
	}

	// Check for Node.js/Jest
	packageJSON := filepath.Join(projectPath, "package.json")
	if r.fileExists(packageJSON) {
		// Read package.json to check for jest
		data, err := os.ReadFile(packageJSON)
		if err == nil && strings.Contains(string(data), "jest") {
			return "jest", nil
		}
		// Default to npm test for Node.js projects
		return "npm", nil
	}

	// Check for Python/pytest
	if r.fileExists(filepath.Join(projectPath, "pytest.ini")) ||
		r.fileExists(filepath.Join(projectPath, "pyproject.toml")) ||
		r.fileExists(filepath.Join(projectPath, "setup.cfg")) {
		return "pytest", nil
	}

	// Check for Python test files
	matches, _ = filepath.Glob(filepath.Join(projectPath, "test_*.py"))
	if len(matches) > 0 {
		return "pytest", nil
	}
	matches, _ = filepath.Glob(filepath.Join(projectPath, "tests", "*.py"))
	if len(matches) > 0 {
		return "pytest", nil
	}

	return "", fmt.Errorf("could not detect test framework in %s", projectPath)
}

// BuildCommand constructs the test command based on framework
func (r *TestRunner) BuildCommand(framework, projectPath, pattern, customCommand string) ([]string, error) {
	// Use custom command if provided
	if customCommand != "" {
		return strings.Fields(customCommand), nil
	}

	switch framework {
	case "go":
		cmd := []string{"go", "test", "-json"}
		if pattern != "" {
			cmd = append(cmd, "-run", pattern)
		}
		cmd = append(cmd, "./...")
		return cmd, nil

	case "jest":
		cmd := []string{"npm", "test", "--", "--json"}
		if pattern != "" {
			cmd = append(cmd, "-t", pattern)
		}
		return cmd, nil

	case "npm":
		return []string{"npm", "test"}, nil

	case "pytest":
		cmd := []string{"pytest", "--json-report", "--json-report-file=/dev/stdout"}
		if pattern != "" {
			cmd = append(cmd, "-k", pattern)
		}
		return cmd, nil

	default:
		return nil, fmt.Errorf("unsupported framework: %s", framework)
	}
}

// executeCommand runs the test command and captures output
func (r *TestRunner) executeCommand(ctx context.Context, cmdArgs []string, workDir string, env map[string]string) (output string, exitCode int, timedOut bool, err error) {
	if len(cmdArgs) == 0 {
		return "", 1, false, fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = workDir

	// Set environment variables
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture combined output
	outputBytes, err := cmd.CombinedOutput()
	output = string(outputBytes)

	// Stream output if streamer is configured
	if r.streamer != nil {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			_ = r.streamer.Write(line)
		}
	}

	// Check for timeout first (before checking exit error)
	if ctx.Err() == context.DeadlineExceeded {
		// Timeout occurred
		return output, 124, true, nil
	}

	// Get exit code
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Other execution error
			return output, 1, false, err
		}
	}

	return output, exitCode, false, nil
}

// parseOutput parses test output based on framework
func (r *TestRunner) parseOutput(framework, output string, exitCode int) (*TestResult, error) {
	switch framework {
	case "go":
		return r.parseGoTestOutput(output, exitCode)
	case "jest":
		return r.parseJestOutput(output, exitCode)
	case "npm":
		return r.parseGenericOutput(output, exitCode, "npm")
	case "pytest":
		return r.parsePytestOutput(output, exitCode)
	default:
		return r.parseGenericOutput(output, exitCode, framework)
	}
}

// parseGoTestOutput parses Go test JSON output
func (r *TestRunner) parseGoTestOutput(output string, exitCode int) (*TestResult, error) {
	// For now, we'll implement a basic parser
	// A full implementation will be in internal/testing/parsers/go.go
	result := &TestResult{
		Framework: "go",
		Success:   exitCode == 0,
		RawOutput: output,
		ExitCode:  exitCode,
		Tests:     []TestCase{},
		Summary:   TestSummary{},
	}

	// Count pass/fail from output
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "PASS") {
			result.Summary.Passed++
			result.Summary.Total++
		} else if strings.Contains(line, "FAIL") {
			result.Summary.Failed++
			result.Summary.Total++
		} else if strings.Contains(line, "SKIP") {
			result.Summary.Skipped++
			result.Summary.Total++
		}
	}

	return result, nil
}

// parseJestOutput parses Jest JSON output
func (r *TestRunner) parseJestOutput(output string, exitCode int) (*TestResult, error) {
	// Placeholder implementation
	result := &TestResult{
		Framework: "jest",
		Success:   exitCode == 0,
		RawOutput: output,
		ExitCode:  exitCode,
		Tests:     []TestCase{},
		Summary:   TestSummary{},
	}
	return result, nil
}

// parsePytestOutput parses pytest JSON output
func (r *TestRunner) parsePytestOutput(output string, exitCode int) (*TestResult, error) {
	// Placeholder implementation
	result := &TestResult{
		Framework: "pytest",
		Success:   exitCode == 0,
		RawOutput: output,
		ExitCode:  exitCode,
		Tests:     []TestCase{},
		Summary:   TestSummary{},
	}
	return result, nil
}

// parseGenericOutput provides fallback parsing for unknown frameworks
func (r *TestRunner) parseGenericOutput(output string, exitCode int, framework string) (*TestResult, error) {
	result := &TestResult{
		Framework: framework,
		Success:   exitCode == 0,
		RawOutput: output,
		ExitCode:  exitCode,
		Tests:     []TestCase{},
		Summary:   TestSummary{},
	}

	// Basic pattern matching
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "passed") || strings.Contains(lower, "ok") {
			result.Summary.Passed++
		}
		if strings.Contains(lower, "failed") || strings.Contains(lower, "error") {
			result.Summary.Failed++
		}
	}

	result.Summary.Total = result.Summary.Passed + result.Summary.Failed

	return result, nil
}

// fileExists checks if a file exists
func (r *TestRunner) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
