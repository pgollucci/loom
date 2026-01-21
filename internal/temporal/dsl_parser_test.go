package temporal

import (
	"strings"
	"testing"
	"time"
)

func TestParseTemporalDSLBasic(t *testing.T) {
	text := `Some preamble text about a task.

<temporal>
WORKFLOW: ProcessTask
  INPUT: {"task_id": "123", "priority": "high"}
  TIMEOUT: 5m
  RETRY: 3
  WAIT: true
END
</temporal>

Some follow-up text after the instruction.`

	instructions, cleaned, err := ParseTemporalDSL(text)
	if err != nil {
		t.Fatalf("ParseTemporalDSL failed: %v", err)
	}

	// Check cleaned text
	if strings.Contains(cleaned, "<temporal>") || strings.Contains(cleaned, "WORKFLOW:") {
		t.Error("Cleaned text still contains temporal DSL")
	}

	if !strings.Contains(cleaned, "Some preamble text") {
		t.Error("Cleaned text missing preamble")
	}

	// Check instructions
	if len(instructions) != 1 {
		t.Fatalf("Expected 1 instruction, got %d", len(instructions))
	}

	instr := instructions[0]
	if instr.Type != InstructionTypeWorkflow {
		t.Errorf("Expected WORKFLOW, got %s", instr.Type)
	}

	if instr.Name != "ProcessTask" {
		t.Errorf("Expected ProcessTask, got %s", instr.Name)
	}

	if instr.Timeout != 5*time.Minute {
		t.Errorf("Expected 5m timeout, got %v", instr.Timeout)
	}

	if instr.Retry != 3 {
		t.Errorf("Expected retry 3, got %d", instr.Retry)
	}

	if !instr.Wait {
		t.Error("Expected Wait=true")
	}

	if instr.Input["task_id"] != "123" {
		t.Errorf("Expected task_id=123, got %v", instr.Input["task_id"])
	}
}

func TestParseTemporalDSLMultipleInstructions(t *testing.T) {
	text := `<temporal>
WORKFLOW: GetBudgets
  INPUT: {"org_id": "acme"}
  TIMEOUT: 2m
  WAIT: true
END

SIGNAL: workflow-123
  NAME: update_budget
  DATA: {"amount": 50000}
END
</temporal>`

	instructions, _, err := ParseTemporalDSL(text)
	if err != nil {
		t.Fatalf("ParseTemporalDSL failed: %v", err)
	}

	if len(instructions) != 2 {
		t.Fatalf("Expected 2 instructions, got %d", len(instructions))
	}

	// First instruction
	if instructions[0].Type != InstructionTypeWorkflow {
		t.Errorf("Expected WORKFLOW, got %s", instructions[0].Type)
	}

	// Second instruction
	if instructions[1].Type != InstructionTypeSignal {
		t.Errorf("Expected SIGNAL, got %s", instructions[1].Type)
	}

	if instructions[1].SignalName != "update_budget" {
		t.Errorf("Expected signal name update_budget, got %s", instructions[1].SignalName)
	}
}

func TestParseTemporalDSLNoBlocks(t *testing.T) {
	text := "This is just plain text with no temporal blocks"

	instructions, cleaned, err := ParseTemporalDSL(text)
	if err != nil {
		t.Fatalf("ParseTemporalDSL failed: %v", err)
	}

	if len(instructions) != 0 {
		t.Fatalf("Expected 0 instructions, got %d", len(instructions))
	}

	if cleaned != text {
		t.Error("Cleaned text should be unchanged when no DSL blocks present")
	}
}

func TestParseTemporalDSLQuery(t *testing.T) {
	text := `<temporal>
QUERY: agent-workflow-123
  TYPE: current_status
END
</temporal>`

	instructions, _, err := ParseTemporalDSL(text)
	if err != nil {
		t.Fatalf("ParseTemporalDSL failed: %v", err)
	}

	if len(instructions) != 1 {
		t.Fatalf("Expected 1 instruction, got %d", len(instructions))
	}

	instr := instructions[0]
	if instr.Type != InstructionTypeQuery {
		t.Errorf("Expected QUERY, got %s", instr.Type)
	}

	if instr.QueryType != "current_status" {
		t.Errorf("Expected current_status, got %s", instr.QueryType)
	}
}

func TestValidateInstruction(t *testing.T) {
	tests := []struct {
		name    string
		instr   TemporalInstruction
		wantErr bool
	}{
		{
			name: "valid workflow",
			instr: TemporalInstruction{
				Type: InstructionTypeWorkflow,
				Name: "TestWorkflow",
			},
			wantErr: false,
		},
		{
			name: "workflow without name",
			instr: TemporalInstruction{
				Type: InstructionTypeWorkflow,
				Name: "",
			},
			wantErr: true,
		},
		{
			name: "valid query",
			instr: TemporalInstruction{
				Type:       InstructionTypeQuery,
				WorkflowID: "wf-123",
				QueryType:  "status",
			},
			wantErr: false,
		},
		{
			name: "query without workflow id",
			instr: TemporalInstruction{
				Type:      InstructionTypeQuery,
				QueryType: "status",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInstruction(tt.instr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInstruction() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFormatDSL(t *testing.T) {
	instr := TemporalInstruction{
		Type:    InstructionTypeWorkflow,
		Name:    "TestWorkflow",
		Timeout: 5 * time.Minute,
		Retry:   3,
		Wait:    true,
		Input: map[string]interface{}{
			"task_id": "123",
		},
	}

	formatted := FormatDSL(instr)

	if !strings.Contains(formatted, "<temporal>") {
		t.Error("Formatted DSL missing opening tag")
	}

	if !strings.Contains(formatted, "WORKFLOW:") {
		t.Error("Formatted DSL missing WORKFLOW instruction")
	}

	if !strings.Contains(formatted, "TestWorkflow") {
		t.Error("Formatted DSL missing workflow name")
	}

	if !strings.Contains(formatted, "TIMEOUT:") {
		t.Error("Formatted DSL missing TIMEOUT")
	}

	if !strings.Contains(formatted, "</temporal>") {
		t.Error("Formatted DSL missing closing tag")
	}
}

func TestDurationParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"5m", 5 * time.Minute, false},
		{"30s", 30 * time.Second, false},
		{"2h", 2 * time.Hour, false},
		{"immediate", 0, false},
		{"default", 5 * time.Minute, false},
		{"60", 60 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseDuration() = %v, want %v", result, tt.expected)
			}
		})
	}
}
