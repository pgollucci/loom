package audit

import (
	"fmt"
	"regexp"
	"strings"
)

type Finding struct {
	Type     FindingType `json:"type"`     // build_error, test_failure, lint_error, warning
	Severity Severity    `json:"severity"` // error, warning, info
	Source   string      `json:"source"`   // "go build", "go test", "golangci-lint"
	File     string      `json:"file"`
	Line     int         `json:"line"`
	Column   int         `json:"column,omitempty"`
	Message  string      `json:"message"`
	Rule     string      `json:"rule,omitempty"` // for linter rules
}

// FindingType categorizes the type of finding
type FindingType string

const (
	FindingTypeBuildError  FindingType = "build_error"
	FindingTypeTestFailure FindingType = "test_failure"
	FindingTypeLintError   FindingType = "lint_error"
	FindingTypeWarning     FindingType = "warning"
)

// Severity indicates how serious the finding is
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Result contains the parsed results from all audit commands
type Result struct {
	Findings    []Finding `json:"findings"`
	BuildPassed bool      `json:"build_passed"`
	TestPassed  bool      `json:"test_passed"`
	LintPassed  bool      `json:"lint_passed"`
	Summary     string    `json:"summary"`
}

// Parser parses command output into structured findings
type Parser struct{}

// NewParser creates a new audit parser
func NewParser() *Parser {
	return &Parser{}
}

// ParseGoBuild parses `go build` output
func (p *Parser) ParseGoBuild(output string) []Finding {
	var findings []Finding

	// Pattern: /path/to/file.go:line:col: error message
	// Example: /home/jkh/Src/loom/internal/foo.go:42:10: undefined: Bar
	re := regexp.MustCompile(`([^:]+):(\d+):(\d+)?:?\s*(.+)`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches != nil {
			findings = append(findings, Finding{
				Type:     FindingTypeBuildError,
				Severity: SeverityError,
				Source:   "go build",
				File:     matches[1],
				Line:     atoiSafe(matches[2]),
				Column:   atoiSafe(matches[3]),
				Message:  strings.TrimSpace(matches[4]),
			})
		} else if strings.Contains(line, "error") || strings.Contains(line, "undefined") ||
			strings.Contains(line, "cannot use") || strings.Contains(line, "cannot find") ||
			strings.Contains(line, "no package") || strings.Contains(line, "import cycle") {
			// Catch-all for build errors that don't match the pattern
			findings = append(findings, Finding{
				Type:     FindingTypeBuildError,
				Severity: SeverityError,
				Source:   "go build",
				Message:  line,
			})
		}
	}

	return findings
}

// ParseGoTest parses `go test` output
func (p *Parser) ParseGoTest(output string) []Finding {
	var findings []Finding

	lines := strings.Split(output, "\n")

	// Check for test failures
	// Pattern: --- FAIL: TestName (0.00s)
	// Followed by:    file_test.go:123: error message
	failRe := regexp.MustCompile(`^--- FAIL: (\w+)`)
	fileRe := regexp.MustCompile(`^\s*([^:]+):(\d+):\s*(.+)`)

	var currentTest string
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if matches := failRe.FindStringSubmatch(line); matches != nil {
			currentTest = matches[1]
			continue
		}

		if currentTest != "" {
			if matches := fileRe.FindStringSubmatch(line); matches != nil {
				findings = append(findings, Finding{
					Type:     FindingTypeTestFailure,
					Severity: SeverityError,
					Source:   "go test",
					File:     matches[1],
					Line:     atoiSafe(matches[2]),
					Message:  strings.TrimSpace(matches[3]),
					Rule:     currentTest,
				})
				currentTest = ""
			}
		}

		// Check for panics in test output
		if strings.Contains(line, "panic:") || strings.Contains(line, "FAIL") {
			if !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "PASS") {
				findings = append(findings, Finding{
					Type:     FindingTypeTestFailure,
					Severity: SeverityError,
					Source:   "go test",
					Message:  line,
				})
			}
		}
	}

	// Check for coverage failures
	if strings.Contains(output, "FAIL") && strings.Contains(output, "coverage") {
		findings = append(findings, Finding{
			Type:     FindingTypeTestFailure,
			Severity: SeverityError,
			Source:   "go test",
			Message:  "Test coverage check failed",
		})
	}

	return findings
}

// ParseGoLint parses golangci-lint output
func (p *Parser) ParseGoLint(output string) []Finding {
	var findings []Finding

	// golangci-lint has multiple output formats
	// JSON: {"from":"file.go:10:5","severity":"error","message":"error message","source":"rule-name"}
	// Plain: file.go:10:5: error message (rule-name)

	// Try JSON first
	if strings.HasPrefix(strings.TrimSpace(output), "[") {
		// For now, fall through to plain parsing
		// Could add JSON parsing here if needed
	}

	// Plain text pattern: path/to/file.go:line:col: message (rule)
	re := regexp.MustCompile(`([^:]+):(\d+):(\d+)?:?\s*(.+?)(?:\s+\(([^)]+)\))?$`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip summary lines
		if strings.HasPrefix(line, "==") || strings.HasPrefix(line, "--") ||
			strings.HasPrefix(line, "戈") || strings.Contains(line, "complete") ||
			strings.Contains(line, "error index") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches != nil {
			severity := SeverityWarning
			if strings.Contains(matches[4], "error") || strings.Contains(matches[4], "undefined") ||
				strings.Contains(matches[4], "cannot") {
				severity = SeverityError
			}

			findings = append(findings, Finding{
				Type:     FindingTypeLintError,
				Severity: severity,
				Source:   "golangci-lint",
				File:     matches[1],
				Line:     atoiSafe(matches[2]),
				Column:   atoiSafe(matches[3]),
				Message:  strings.TrimSpace(matches[4]),
				Rule:     matches[5], // rule name in parentheses
			})
		} else if strings.Contains(line, "error") || strings.Contains(line, "warning") {
			// Catch-all for linter errors
			findings = append(findings, Finding{
				Type:     FindingTypeLintError,
				Severity: SeverityWarning,
				Source:   "golangci-lint",
				Message:  line,
			})
		}
	}

	return findings
}

// Parse parses command output based on source type
func (p *Parser) Parse(source, output string) []Finding {
	switch source {
	case "go build":
		return p.ParseGoBuild(output)
	case "go test":
		return p.ParseGoTest(output)
	case "golangci-lint":
		return p.ParseGoLint(output)
	default:
		return nil
	}
}

// NewResult creates a new audit result from findings
func (p *Parser) NewResult(findings []Finding) *Result {
	buildErrors := 0
	testFailures := 0
	lintErrors := 0

	for _, f := range findings {
		switch f.Type {
		case FindingTypeBuildError:
			buildErrors++
		case FindingTypeTestFailure:
			testFailures++
		case FindingTypeLintError:
			lintErrors++
		}
	}

	summary := "Audit complete"
	if buildErrors > 0 {
		summary = summary + fmt.Sprintf(", %d build errors", buildErrors)
	}
	if testFailures > 0 {
		summary = summary + fmt.Sprintf(", %d test failures", testFailures)
	}
	if lintErrors > 0 {
		summary = summary + fmt.Sprintf(", %d lint errors", lintErrors)
	}
	if len(findings) == 0 {
		summary = "All checks passed"
	}

	return &Result{
		Findings:    findings,
		BuildPassed: buildErrors == 0,
		TestPassed:  testFailures == 0,
		LintPassed:  lintErrors == 0,
		Summary:     summary,
	}
}

func atoiSafe(s string) int {
	if s == "" {
		return 0
	}
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
