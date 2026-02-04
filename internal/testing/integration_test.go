package testing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIntegration_GoProject tests running Go tests on a real project
func TestIntegration_GoProject(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary Go project with tests
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create main.go
	mainGo := `package main

func Add(a, b int) int {
	return a + b
}

func Subtract(a, b int) int {
	return a - b
}

func main() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("Failed to create main.go: %v", err)
	}

	// Create main_test.go with passing tests
	mainTestGo := `package main

import "testing"

func TestAdd(t *testing.T) {
	result := Add(2, 3)
	if result != 5 {
		t.Errorf("Add(2, 3) = %d, want 5", result)
	}
}

func TestSubtract(t *testing.T) {
	result := Subtract(5, 3)
	if result != 2 {
		t.Errorf("Subtract(5, 3) = %d, want 2", result)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte(mainTestGo), 0644); err != nil {
		t.Fatalf("Failed to create main_test.go: %v", err)
	}

	// Run tests
	runner := NewTestRunner(tmpDir)

	req := TestRequest{
		ProjectPath: tmpDir,
		Timeout:     30 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify results
	if result.Framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", result.Framework)
	}

	if !result.Success {
		t.Errorf("Expected tests to pass, but they failed. Output: %s", result.RawOutput)
	}

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}

	if result.Summary.Passed < 2 {
		t.Errorf("Expected at least 2 passed tests, got %d", result.Summary.Passed)
	}
}

// TestIntegration_GoProject_WithFailures tests handling of failing tests
func TestIntegration_GoProject_WithFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary Go project with failing tests
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create calculator.go
	calculatorGo := `package main

func Multiply(a, b int) int {
	return a * b // Correct implementation
}

func Divide(a, b int) int {
	return a + b // Bug: should be a / b
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "calculator.go"), []byte(calculatorGo), 0644); err != nil {
		t.Fatalf("Failed to create calculator.go: %v", err)
	}

	// Create calculator_test.go with one passing and one failing test
	calculatorTestGo := `package main

import "testing"

func TestMultiply(t *testing.T) {
	result := Multiply(3, 4)
	if result != 12 {
		t.Errorf("Multiply(3, 4) = %d, want 12", result)
	}
}

func TestDivide(t *testing.T) {
	result := Divide(10, 2)
	if result != 5 {
		t.Errorf("Divide(10, 2) = %d, want 5", result)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "calculator_test.go"), []byte(calculatorTestGo), 0644); err != nil {
		t.Fatalf("Failed to create calculator_test.go: %v", err)
	}

	// Run tests
	runner := NewTestRunner(tmpDir)

	req := TestRequest{
		ProjectPath: tmpDir,
		Timeout:     30 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify results
	if result.Success {
		t.Error("Expected tests to fail, but they passed")
	}

	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for failing tests")
	}

	if result.Summary.Failed == 0 {
		t.Error("Expected at least one failed test")
	}

	if result.Summary.Passed == 0 {
		t.Error("Expected at least one passed test")
	}
}

// TestIntegration_GoProject_WithPattern tests running specific tests by pattern
func TestIntegration_GoProject_WithPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary Go project
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create math.go
	mathGo := `package main

func Add(a, b int) int { return a + b }
func Multiply(a, b int) int { return a * b }
`
	if err := os.WriteFile(filepath.Join(tmpDir, "math.go"), []byte(mathGo), 0644); err != nil {
		t.Fatalf("Failed to create math.go: %v", err)
	}

	// Create math_test.go
	mathTestGo := `package main

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("Add failed")
	}
}

func TestMultiply(t *testing.T) {
	if Multiply(2, 3) != 6 {
		t.Error("Multiply failed")
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "math_test.go"), []byte(mathTestGo), 0644); err != nil {
		t.Fatalf("Failed to create math_test.go: %v", err)
	}

	// Run only TestAdd
	runner := NewTestRunner(tmpDir)

	req := TestRequest{
		ProjectPath: tmpDir,
		TestPattern: "TestAdd",
		Timeout:     30 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify only TestAdd ran
	if !result.Success {
		t.Errorf("Expected TestAdd to pass. Output: %s", result.RawOutput)
	}

	// The output should mention TestAdd but not TestMultiply
	if !containsString(result.RawOutput, "TestAdd") {
		t.Error("Expected output to mention TestAdd")
	}
}

// TestIntegration_OutputStreaming tests real-time output streaming
func TestIntegration_OutputStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary Go project
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module testproject

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create simple_test.go
	simpleTestGo := `package main

import "testing"

func TestSimple(t *testing.T) {
	t.Log("Starting test")
	if 1+1 != 2 {
		t.Error("Math is broken")
	}
	t.Log("Test complete")
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "simple_test.go"), []byte(simpleTestGo), 0644); err != nil {
		t.Fatalf("Failed to create simple_test.go: %v", err)
	}

	// Create runner with output streamer
	runner := NewTestRunner(tmpDir)
	streamer := &mockOutputStreamer{}
	runner.SetOutputStreamer(streamer)

	req := TestRequest{
		ProjectPath: tmpDir,
		Timeout:     30 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify streaming worked
	if len(streamer.lines) == 0 {
		t.Error("Expected output to be streamed, but got no lines")
	}

	if !result.Success {
		t.Errorf("Expected test to pass. Output: %s", result.RawOutput)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
