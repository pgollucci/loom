package testing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockOutputStreamer implements OutputStreamer for testing
type mockOutputStreamer struct {
	lines []string
}

func (m *mockOutputStreamer) Write(line string) error {
	m.lines = append(m.lines, line)
	return nil
}

func (m *mockOutputStreamer) Close() error {
	return nil
}

func TestNewTestRunner(t *testing.T) {
	runner := NewTestRunner("/tmp/test")
	if runner == nil {
		t.Fatal("Expected TestRunner instance, got nil")
	}
	if runner.workDir != "/tmp/test" {
		t.Errorf("Expected workDir /tmp/test, got %s", runner.workDir)
	}
}

func TestTestRunner_SetOutputStreamer(t *testing.T) {
	runner := NewTestRunner("/tmp/test")
	streamer := &mockOutputStreamer{}

	runner.SetOutputStreamer(streamer)
	if runner.streamer == nil {
		t.Error("Expected streamer to be set")
	}
}

func TestTestRunner_DetectFramework_Go(t *testing.T) {
	// Create temporary directory with go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	runner := NewTestRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)
	if err != nil {
		t.Fatalf("DetectFramework failed: %v", err)
	}

	if framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", framework)
	}
}

func TestTestRunner_DetectFramework_GoTestFiles(t *testing.T) {
	// Create temporary directory with test files
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "foo_test.go")
	if err := os.WriteFile(testFilePath, []byte("package foo"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	runner := NewTestRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)
	if err != nil {
		t.Fatalf("DetectFramework failed: %v", err)
	}

	if framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", framework)
	}
}

func TestTestRunner_DetectFramework_Jest(t *testing.T) {
	// Create temporary directory with package.json containing jest
	tmpDir := t.TempDir()
	packageJSONPath := filepath.Join(tmpDir, "package.json")
	packageJSON := `{
		"name": "test",
		"devDependencies": {
			"jest": "^29.0.0"
		}
	}`
	if err := os.WriteFile(packageJSONPath, []byte(packageJSON), 0644); err != nil {
		t.Fatalf("Failed to create package.json: %v", err)
	}

	runner := NewTestRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)
	if err != nil {
		t.Fatalf("DetectFramework failed: %v", err)
	}

	if framework != "jest" {
		t.Errorf("Expected framework 'jest', got '%s'", framework)
	}
}

func TestTestRunner_DetectFramework_NPM(t *testing.T) {
	// Create temporary directory with package.json without jest
	tmpDir := t.TempDir()
	packageJSONPath := filepath.Join(tmpDir, "package.json")
	packageJSON := `{"name": "test"}`
	if err := os.WriteFile(packageJSONPath, []byte(packageJSON), 0644); err != nil {
		t.Fatalf("Failed to create package.json: %v", err)
	}

	runner := NewTestRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)
	if err != nil {
		t.Fatalf("DetectFramework failed: %v", err)
	}

	if framework != "npm" {
		t.Errorf("Expected framework 'npm', got '%s'", framework)
	}
}

func TestTestRunner_DetectFramework_Pytest(t *testing.T) {
	// Create temporary directory with pytest.ini
	tmpDir := t.TempDir()
	pytestIniPath := filepath.Join(tmpDir, "pytest.ini")
	if err := os.WriteFile(pytestIniPath, []byte("[pytest]"), 0644); err != nil {
		t.Fatalf("Failed to create pytest.ini: %v", err)
	}

	runner := NewTestRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)
	if err != nil {
		t.Fatalf("DetectFramework failed: %v", err)
	}

	if framework != "pytest" {
		t.Errorf("Expected framework 'pytest', got '%s'", framework)
	}
}

func TestTestRunner_DetectFramework_PytestFiles(t *testing.T) {
	// Create temporary directory with test_*.py files
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "test_foo.py")
	if err := os.WriteFile(testFilePath, []byte("def test_foo(): pass"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	runner := NewTestRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)
	if err != nil {
		t.Fatalf("DetectFramework failed: %v", err)
	}

	if framework != "pytest" {
		t.Errorf("Expected framework 'pytest', got '%s'", framework)
	}
}

func TestTestRunner_DetectFramework_Unknown(t *testing.T) {
	// Create temporary directory with no recognizable test setup
	tmpDir := t.TempDir()

	runner := NewTestRunner(tmpDir)
	_, err := runner.DetectFramework(tmpDir)
	if err == nil {
		t.Error("Expected error for unknown framework, got nil")
	}

	if !strings.Contains(err.Error(), "could not detect test framework") {
		t.Errorf("Expected 'could not detect' error, got: %v", err)
	}
}

func TestTestRunner_BuildCommand_Go(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	tests := []struct {
		name        string
		pattern     string
		expected    []string
	}{
		{
			name:     "No pattern",
			pattern:  "",
			expected: []string{"go", "test", "-json", "./..."},
		},
		{
			name:     "With pattern",
			pattern:  "TestFoo",
			expected: []string{"go", "test", "-json", "-run", "TestFoo", "./..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := runner.BuildCommand("go", "/tmp/test", tt.pattern, "")
			if err != nil {
				t.Fatalf("BuildCommand failed: %v", err)
			}

			if len(cmd) != len(tt.expected) {
				t.Errorf("Expected command length %d, got %d", len(tt.expected), len(cmd))
			}

			for i, arg := range tt.expected {
				if i >= len(cmd) || cmd[i] != arg {
					t.Errorf("Expected arg[%d] = %s, got %s", i, arg, cmd[i])
				}
			}
		})
	}
}

func TestTestRunner_BuildCommand_Jest(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	tests := []struct {
		name        string
		pattern     string
		expected    []string
	}{
		{
			name:     "No pattern",
			pattern:  "",
			expected: []string{"npm", "test", "--", "--json"},
		},
		{
			name:     "With pattern",
			pattern:  "should work",
			expected: []string{"npm", "test", "--", "--json", "-t", "should work"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := runner.BuildCommand("jest", "/tmp/test", tt.pattern, "")
			if err != nil {
				t.Fatalf("BuildCommand failed: %v", err)
			}

			if len(cmd) != len(tt.expected) {
				t.Errorf("Expected command length %d, got %d", len(tt.expected), len(cmd))
			}

			for i, arg := range tt.expected {
				if i >= len(cmd) || cmd[i] != arg {
					t.Errorf("Expected arg[%d] = %s, got %s", i, arg, cmd[i])
				}
			}
		})
	}
}

func TestTestRunner_BuildCommand_Pytest(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	cmd, err := runner.BuildCommand("pytest", "/tmp/test", "", "")
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	if cmd[0] != "pytest" {
		t.Errorf("Expected first arg 'pytest', got '%s'", cmd[0])
	}

	if !contains(cmd, "--json-report") {
		t.Error("Expected command to contain --json-report")
	}
}

func TestTestRunner_BuildCommand_CustomCommand(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	custom := "make test"
	cmd, err := runner.BuildCommand("go", "/tmp/test", "", custom)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	expected := []string{"make", "test"}
	if len(cmd) != len(expected) {
		t.Errorf("Expected command length %d, got %d", len(expected), len(cmd))
	}

	for i, arg := range expected {
		if cmd[i] != arg {
			t.Errorf("Expected arg[%d] = %s, got %s", i, arg, cmd[i])
		}
	}
}

func TestTestRunner_BuildCommand_UnsupportedFramework(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	_, err := runner.BuildCommand("unknown", "/tmp/test", "", "")
	if err == nil {
		t.Error("Expected error for unsupported framework, got nil")
	}

	if !strings.Contains(err.Error(), "unsupported framework") {
		t.Errorf("Expected 'unsupported framework' error, got: %v", err)
	}
}

func TestTestRunner_Run_BasicExecution(t *testing.T) {
	// This test requires echo command (available on Unix systems)
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("Skipping test: /bin/echo not available")
	}

	tmpDir := t.TempDir()
	runner := NewTestRunner(tmpDir)

	req := TestRequest{
		ProjectPath:  tmpDir,
		TestCommand:  "echo test output",
		Framework:    "generic",
		Timeout:      5 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}

	if !strings.Contains(result.RawOutput, "test output") {
		t.Errorf("Expected output to contain 'test output', got: %s", result.RawOutput)
	}
}

func TestTestRunner_Run_Timeout(t *testing.T) {
	// This test requires sleep command
	if _, err := os.Stat("/bin/sleep"); err != nil {
		t.Skip("Skipping test: /bin/sleep not available")
	}

	tmpDir := t.TempDir()
	runner := NewTestRunner(tmpDir)

	req := TestRequest{
		ProjectPath:  tmpDir,
		TestCommand:  "sleep 5",
		Framework:    "generic",
		Timeout:      500 * time.Millisecond,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !result.TimedOut {
		t.Errorf("Expected test to timeout, but it didn't. Exit code: %d, Duration: %v", result.ExitCode, result.Duration)
	}

	// Verify the test didn't complete (should have been killed)
	if result.Duration >= 5*time.Second {
		t.Error("Test should have been killed before completing 5 seconds")
	}
}

func TestTestRunner_Run_WithStreamer(t *testing.T) {
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("Skipping test: /bin/echo not available")
	}

	tmpDir := t.TempDir()
	runner := NewTestRunner(tmpDir)

	streamer := &mockOutputStreamer{}
	runner.SetOutputStreamer(streamer)

	req := TestRequest{
		ProjectPath:  tmpDir,
		TestCommand:  "echo line1; echo line2",
		Framework:    "generic",
		Timeout:      5 * time.Second,
	}

	ctx := context.Background()
	_, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(streamer.lines) == 0 {
		t.Error("Expected streamer to receive output lines")
	}
}

func TestTestRunner_Run_DefaultTimeout(t *testing.T) {
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("Skipping test: /bin/echo not available")
	}

	tmpDir := t.TempDir()
	runner := NewTestRunner(tmpDir)

	req := TestRequest{
		ProjectPath:  tmpDir,
		TestCommand:  "echo test",
		Framework:    "generic",
		// Timeout not specified - should use default
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify default timeout was applied (should not timeout for quick command)
	if result.TimedOut {
		t.Error("Command should not have timed out with default timeout")
	}
}

func TestTestRunner_Run_MaxTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	runner := NewTestRunner(tmpDir)

	// Request timeout beyond maximum
	req := TestRequest{
		ProjectPath:  tmpDir,
		TestCommand:  "echo test",
		Framework:    "generic",
		Timeout:      MaxTestTimeout + time.Hour,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify the test completed (wasn't waiting for the excessive timeout)
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

func TestTestRunner_ParseGoTestOutput(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	output := `PASS
ok  	github.com/user/pkg	0.123s
FAIL
FAIL	github.com/user/other	0.456s
`

	result, err := runner.parseGoTestOutput(output, 1)
	if err != nil {
		t.Fatalf("parseGoTestOutput failed: %v", err)
	}

	if result.Framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", result.Framework)
	}

	if result.Success {
		t.Error("Expected success=false for exit code 1")
	}

	if result.Summary.Total == 0 {
		t.Error("Expected some tests to be counted")
	}
}

func TestTestRunner_ParseGenericOutput(t *testing.T) {
	runner := NewTestRunner("/tmp/test")

	output := `Running tests...
Test 1: PASSED
Test 2: FAILED
Test 3: PASSED
All tests complete.
`

	result, err := runner.parseGenericOutput(output, 1, "custom")
	if err != nil {
		t.Fatalf("parseGenericOutput failed: %v", err)
	}

	if result.Framework != "custom" {
		t.Errorf("Expected framework 'custom', got '%s'", result.Framework)
	}

	if result.Summary.Passed == 0 {
		t.Error("Expected some passed tests to be counted")
	}

	if result.Summary.Failed == 0 {
		t.Error("Expected some failed tests to be counted")
	}
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
