package dispatch

import (
	"testing"

	"github.com/jordanhubbard/loom/pkg/models"
)

func TestInferAgentRole_AllRoles(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		agentRole string
		beadType  string
		want      string
	}{
		{"coder", "task", "coder"},
		{"senior-coder", "task", "coder"},
		{"software-engineer", "bug", "coder"},
		{"developer", "feature", "coder"},
		{"programmer", "task", "coder"},
		{"reviewer", "review", "reviewer"},
		{"code-reviewer", "task", "reviewer"},
		{"qa", "test", "qa"},
		{"quality-assurance", "task", "qa"},
		{"tester", "task", "qa"},
		{"test-engineer", "task", "coder"}, // "engineer" in name -> coder
		{"pm", "task", "pm"},
		{"product-manager", "task", "pm"},
		{"project-manager", "task", "pm"},
		{"architect", "task", "architect"},
		{"cto", "decision", "architect"},
		// Fallback to bead type
		{"engineering-manager", "bug", "coder"},
		{"engineering-manager", "feature", "coder"},
		{"engineering-manager", "task", "coder"},
		// No match
		{"ceo", "decision", ""},
	}

	for _, tc := range tests {
		ag := &models.Agent{Role: tc.agentRole}
		bead := &models.Bead{Type: tc.beadType}
		got := d.inferAgentRole(ag, bead)
		if got != tc.want {
			t.Errorf("inferAgentRole(%q, %q) = %q, want %q", tc.agentRole, tc.beadType, got, tc.want)
		}
	}
}

func TestInferAgentRole_NilBead(t *testing.T) {
	d := &Dispatcher{}
	// "engineering-manager" contains "engineer" -> maps to "coder"
	ag := &models.Agent{Role: "engineering-manager"}
	got := d.inferAgentRole(ag, nil)
	if got != "coder" {
		t.Errorf("expected coder for EM (contains 'engineer'), got %q", got)
	}

	// CEO has no role match and nil bead -> empty
	agCEO := &models.Agent{Role: "ceo"}
	got = d.inferAgentRole(agCEO, nil)
	if got != "" {
		t.Errorf("expected empty role for CEO with nil bead, got %q", got)
	}
}

func TestNormalizeRoleName_Extended(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  CTO  ", "cto"},
		{"Engineering Manager", "engineering-manager"},
		{"senior_developer", "senior-developer"},
		{"path/to/coder", "coder"},
		{"architect (senior)", "architect"},
		{"foo--bar", "foo-bar"},
		{"-leading-trailing-", "leading-trailing"},
		{"deep/nested/path/reviewer", "reviewer"},
	}

	for _, tc := range tests {
		got := normalizeRoleName(tc.input)
		if got != tc.want {
			t.Errorf("normalizeRoleName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
