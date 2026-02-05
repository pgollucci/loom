package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectFramework_Go(t *testing.T) {
	tmpDir := t.TempDir()

	// Create go.mod
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", framework)
	}
}

func TestDetectFramework_GoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .go file
	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", framework)
	}
}

func TestDetectFramework_Npm(t *testing.T) {
	tmpDir := t.TempDir()

	// Create package.json
	pkgPath := filepath.Join(tmpDir, "package.json")
	if err := os.WriteFile(pkgPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "npm" {
		t.Errorf("Expected framework 'npm', got '%s'", framework)
	}
}

func TestDetectFramework_Make(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Makefile
	makefile := filepath.Join(tmpDir, "Makefile")
	if err := os.WriteFile(makefile, []byte("all:\n\techo test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "make" {
		t.Errorf("Expected framework 'make', got '%s'", framework)
	}
}

func TestDetectFramework_Cargo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Cargo.toml
	cargoPath := filepath.Join(tmpDir, "Cargo.toml")
	if err := os.WriteFile(cargoPath, []byte("[package]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "cargo" {
		t.Errorf("Expected framework 'cargo', got '%s'", framework)
	}
}

func TestDetectFramework_Maven(t *testing.T) {
	tmpDir := t.TempDir()

	// Create pom.xml
	pomPath := filepath.Join(tmpDir, "pom.xml")
	if err := os.WriteFile(pomPath, []byte("<project></project>\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "maven" {
		t.Errorf("Expected framework 'maven', got '%s'", framework)
	}
}

func TestDetectFramework_Gradle(t *testing.T) {
	tmpDir := t.TempDir()

	// Create build.gradle
	gradlePath := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradlePath, []byte("apply plugin: 'java'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)
	framework, err := runner.DetectFramework(tmpDir)

	if err != nil {
		t.Errorf("DetectFramework() error = %v", err)
	}
	if framework != "gradle" {
		t.Errorf("Expected framework 'gradle', got '%s'", framework)
	}
}

func TestDetectFramework_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	runner := NewBuildRunner(tmpDir)
	_, err := runner.DetectFramework(tmpDir)

	if err == nil {
		t.Error("Expected error for unknown framework, got nil")
	}
}

func TestBuildCommand_Go(t *testing.T) {
	runner := NewBuildRunner(".")

	cmd, err := runner.BuildCommand("go", ".", "", "")
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	expected := []string{"go", "build", "./..."}
	if !stringSliceEqual(cmd, expected) {
		t.Errorf("Expected command %v, got %v", expected, cmd)
	}
}

func TestBuildCommand_GoWithTarget(t *testing.T) {
	runner := NewBuildRunner(".")

	cmd, err := runner.BuildCommand("go", ".", "myapp", "")
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	expected := []string{"go", "build", "-o", "myapp", "./..."}
	if !stringSliceEqual(cmd, expected) {
		t.Errorf("Expected command %v, got %v", expected, cmd)
	}
}

func TestBuildCommand_Npm(t *testing.T) {
	runner := NewBuildRunner(".")

	cmd, err := runner.BuildCommand("npm", ".", "", "")
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	expected := []string{"npm", "run", "build"}
	if !stringSliceEqual(cmd, expected) {
		t.Errorf("Expected command %v, got %v", expected, cmd)
	}
}

func TestBuildCommand_Make(t *testing.T) {
	runner := NewBuildRunner(".")

	cmd, err := runner.BuildCommand("make", ".", "", "")
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	expected := []string{"make"}
	if !stringSliceEqual(cmd, expected) {
		t.Errorf("Expected command %v, got %v", expected, cmd)
	}
}

func TestBuildCommand_CustomCommand(t *testing.T) {
	runner := NewBuildRunner(".")

	cmd, err := runner.BuildCommand("go", ".", "", "go build -v ./cmd/app")
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	expected := []string{"go", "build", "-v", "./cmd/app"}
	if !stringSliceEqual(cmd, expected) {
		t.Errorf("Expected command %v, got %v", expected, cmd)
	}
}

func TestBuildCommand_UnsupportedFramework(t *testing.T) {
	runner := NewBuildRunner(".")

	_, err := runner.BuildCommand("unknown", ".", "", "")
	if err == nil {
		t.Error("Expected error for unsupported framework, got nil")
	}
}

func TestParseGoOutput_Success(t *testing.T) {
	runner := NewBuildRunner(".")
	output := ""

	result, err := runner.parseGoOutput(output, 0)
	if err != nil {
		t.Fatalf("parseGoOutput() error = %v", err)
	}

	if result.Framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", result.Framework)
	}
	if !result.Success {
		t.Error("Expected success to be true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Expected 0 errors, got %d", len(result.Errors))
	}
}

func TestParseGoOutput_WithErrors(t *testing.T) {
	runner := NewBuildRunner(".")
	output := `internal/foo/bar.go:10:2: undefined: someFunc
internal/baz/qux.go:25:5: syntax error: unexpected newline`

	result, err := runner.parseGoOutput(output, 1)
	if err != nil {
		t.Fatalf("parseGoOutput() error = %v", err)
	}

	if result.Success {
		t.Error("Expected success to be false")
	}
	if len(result.Errors) != 2 {
		t.Fatalf("Expected 2 errors, got %d", len(result.Errors))
	}

	// Check first error
	if result.Errors[0].File != "internal/foo/bar.go" {
		t.Errorf("Expected file 'internal/foo/bar.go', got '%s'", result.Errors[0].File)
	}
	if result.Errors[0].Line != 10 {
		t.Errorf("Expected line 10, got %d", result.Errors[0].Line)
	}
	if result.Errors[0].Column != 2 {
		t.Errorf("Expected column 2, got %d", result.Errors[0].Column)
	}
	if !strings.Contains(result.Errors[0].Message, "undefined") {
		t.Errorf("Expected message to contain 'undefined', got '%s'", result.Errors[0].Message)
	}
}

func TestParseNpmOutput_Success(t *testing.T) {
	runner := NewBuildRunner(".")
	output := "Built successfully\nwebpack compiled with 0 errors"

	result, err := runner.parseNpmOutput(output, 0)
	if err != nil {
		t.Fatalf("parseNpmOutput() error = %v", err)
	}

	if !result.Success {
		t.Error("Expected success to be true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Expected 0 errors, got %d", len(result.Errors))
	}
}

func TestParseNpmOutput_WithErrors(t *testing.T) {
	runner := NewBuildRunner(".")
	output := `ERROR in ./src/app.js
Module not found: Error: Can't resolve 'foo'
ERROR in ./src/utils.js
Syntax error: Unexpected token`

	result, err := runner.parseNpmOutput(output, 1)
	if err != nil {
		t.Fatalf("parseNpmOutput() error = %v", err)
	}

	if result.Success {
		t.Error("Expected success to be false")
	}
	if len(result.Errors) != 2 {
		t.Fatalf("Expected 2 errors, got %d", len(result.Errors))
	}
}

func TestParseMakeOutput_Success(t *testing.T) {
	runner := NewBuildRunner(".")
	output := "gcc -o app main.c\nBuild complete"

	result, err := runner.parseMakeOutput(output, 0)
	if err != nil {
		t.Fatalf("parseMakeOutput() error = %v", err)
	}

	if !result.Success {
		t.Error("Expected success to be true")
	}
}

func TestParseMakeOutput_WithErrors(t *testing.T) {
	runner := NewBuildRunner(".")
	output := `main.c:10:5: error: 'foo' undeclared
utils.c:25:3: warning: implicit declaration of function 'bar'`

	result, err := runner.parseMakeOutput(output, 1)
	if err != nil {
		t.Fatalf("parseMakeOutput() error = %v", err)
	}

	if result.Success {
		t.Error("Expected success to be false")
	}
	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(result.Errors))
	}
	if len(result.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(result.Warnings))
	}
}

func TestParseCargoOutput_Success(t *testing.T) {
	runner := NewBuildRunner(".")
	output := "   Compiling myapp v0.1.0\n    Finished dev [unoptimized + debuginfo] target(s)"

	result, err := runner.parseCargoOutput(output, 0)
	if err != nil {
		t.Fatalf("parseCargoOutput() error = %v", err)
	}

	if !result.Success {
		t.Error("Expected success to be true")
	}
}

func TestParseCargoOutput_WithErrors(t *testing.T) {
	runner := NewBuildRunner(".")
	output := `error[E0425]: cannot find value 'x' in this scope
  --> src/main.rs:10:5
warning: unused variable: 'y'
  --> src/lib.rs:25:9`

	result, err := runner.parseCargoOutput(output, 1)
	if err != nil {
		t.Fatalf("parseCargoOutput() error = %v", err)
	}

	if result.Success {
		t.Error("Expected success to be false")
	}
	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(result.Errors))
	}
	if len(result.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(result.Warnings))
	}

	// Check error details
	if result.Errors[0].File != "src/main.rs" {
		t.Errorf("Expected file 'src/main.rs', got '%s'", result.Errors[0].File)
	}
	if result.Errors[0].Line != 10 {
		t.Errorf("Expected line 10, got %d", result.Errors[0].Line)
	}
}

func TestRun_DefaultTimeout(t *testing.T) {
	runner := NewBuildRunner(".")

	req := BuildRequest{
		ProjectPath: ".",
		Framework:   "go",
		// No timeout specified
	}

	// This should use DefaultBuildTimeout
	ctx := context.Background()
	result, err := runner.Run(ctx, req)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
}

func TestRun_MaxTimeout(t *testing.T) {
	runner := NewBuildRunner(".")

	req := BuildRequest{
		ProjectPath: ".",
		Framework:   "go",
		Timeout:     MaxBuildTimeout + time.Minute, // Exceeds max
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// Timeout should be clamped to MaxBuildTimeout
	if result.TimedOut && result.Duration < MaxBuildTimeout {
		t.Error("Build timed out before MaxBuildTimeout")
	}
}

// Integration test - only runs if go is available
func TestRun_IntegrationGoBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	// Create a simple Go module
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainGoPath := filepath.Join(tmpDir, "main.go")
	mainGoContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	if err := os.WriteFile(mainGoPath, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)

	req := BuildRequest{
		ProjectPath: tmpDir,
		Framework:   "go",
		Timeout:     30 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Framework != "go" {
		t.Errorf("Expected framework 'go', got '%s'", result.Framework)
	}
	if !result.Success {
		t.Errorf("Expected successful build, got failure: %s", result.RawOutput)
	}
	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
	if len(result.Errors) > 0 {
		t.Errorf("Expected no errors, got %d", len(result.Errors))
	}
}

// Integration test - build with errors
func TestRun_IntegrationGoBuildWithErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	// Create a simple Go module
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create Go file with error
	mainGoPath := filepath.Join(tmpDir, "main.go")
	mainGoContent := `package main

func main() {
	undefinedFunc() // This will cause a compile error
}
`
	if err := os.WriteFile(mainGoPath, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewBuildRunner(tmpDir)

	req := BuildRequest{
		ProjectPath: tmpDir,
		Framework:   "go",
		Timeout:     30 * time.Second,
	}

	ctx := context.Background()
	result, err := runner.Run(ctx, req)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Success {
		t.Error("Expected build to fail, but it succeeded")
	}
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code")
	}
	if len(result.Errors) == 0 {
		t.Error("Expected at least one error to be parsed")
	}

	// Check that error was parsed correctly
	foundUndefined := false
	for _, err := range result.Errors {
		if strings.Contains(err.Message, "undefined") || strings.Contains(err.Message, "undeclared") {
			foundUndefined = true
			if err.File == "" {
				t.Error("Expected error to have file information")
			}
			if err.Line == 0 {
				t.Error("Expected error to have line number")
			}
		}
	}
	if !foundUndefined {
		t.Errorf("Expected to find undefined/undeclared error, got: %+v", result.Errors)
	}
}

// Helper function to compare string slices
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
