package projectagent

import (
	"testing"
)

func TestParseActionsFromResponse_DirectJSON(t *testing.T) {
	input := `{"thinking":"let me check","actions":[{"type":"bash","params":{"command":"ls"}},{"type":"done","params":{"message":"finished"}}]}`
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Thinking != "let me check" {
		t.Errorf("got thinking %q", resp.Thinking)
	}
	if len(resp.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(resp.Actions))
	}
	if resp.Actions[0].Type != "bash" {
		t.Errorf("got action[0].type %q", resp.Actions[0].Type)
	}
	if resp.Actions[1].Type != "done" {
		t.Errorf("got action[1].type %q", resp.Actions[1].Type)
	}
}

func TestParseActionsFromResponse_CodeBlock(t *testing.T) {
	input := "Here's what I'll do:\n```json\n{\"actions\":[{\"type\":\"read\",\"params\":{\"path\":\"main.go\"}}]}\n```"
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(resp.Actions))
	}
	if resp.Actions[0].Type != "read" {
		t.Errorf("got type %q", resp.Actions[0].Type)
	}
}

func TestParseActionsFromResponse_EmbeddedJSON(t *testing.T) {
	input := `I think we should start with this: {"actions":[{"type":"write","params":{"path":"test.go","content":"package main"}}]} and see what happens.`
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Type != "write" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestParseActionsFromResponse_NoJSON(t *testing.T) {
	input := "I'm not sure what to do next."
	_, err := parseActionsFromResponse(input)
	if err == nil {
		t.Error("expected error for no JSON")
	}
}

func TestParseActionsFromResponse_EmptyActions(t *testing.T) {
	input := `{"actions":[]}`
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(resp.Actions))
	}
}

func TestParseActionsFromResponse_MalformedJSON(t *testing.T) {
	input := `{"actions":[{broken}`
	_, err := parseActionsFromResponse(input)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestFormatActionFeedback_Success(t *testing.T) {
	action := LLMAction{Type: "bash", Params: map[string]interface{}{"command": "ls"}}
	result := ActionResult{Type: "bash", Success: true, Output: "file1.go\nfile2.go"}

	feedback := formatActionFeedback(action, result)
	if feedback == "" {
		t.Error("feedback should not be empty")
	}

	// Should contain SUCCESS
	if !contains(feedback, "SUCCESS") {
		t.Error("feedback should contain SUCCESS")
	}
	if !contains(feedback, "file1.go") {
		t.Error("feedback should contain output")
	}
}

func TestFormatActionFeedback_Failure(t *testing.T) {
	action := LLMAction{Type: "bash"}
	result := ActionResult{Type: "bash", Success: false, Error: "command not found", Output: ""}

	feedback := formatActionFeedback(action, result)
	if !contains(feedback, "FAILED") {
		t.Error("feedback should contain FAILED")
	}
	if !contains(feedback, "command not found") {
		t.Error("feedback should contain error message")
	}
}

func TestFormatActionFeedback_LongOutput(t *testing.T) {
	action := LLMAction{Type: "read"}
	// Create output > 4000 chars
	longOutput := make([]byte, 5000)
	for i := range longOutput {
		longOutput[i] = 'x'
	}
	result := ActionResult{Type: "read", Success: true, Output: string(longOutput)}

	feedback := formatActionFeedback(action, result)
	if !contains(feedback, "truncated") {
		t.Error("long output should be truncated")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	agent := &Agent{role: "coder"}
	cfg := ActionLoopConfig{
		PersonaInstructions: "You are a senior Go developer.",
	}

	prompt := agent.buildSystemPrompt(cfg)

	if !contains(prompt, "coder") {
		t.Error("prompt should mention role")
	}
	if !contains(prompt, "senior Go developer") {
		t.Error("prompt should include persona instructions")
	}
	if !contains(prompt, "bash") {
		t.Error("prompt should list available actions")
	}
	if !contains(prompt, "done") {
		t.Error("prompt should mention done action")
	}
}

func TestBuildSystemPrompt_EmptyRole(t *testing.T) {
	agent := &Agent{role: ""}
	cfg := ActionLoopConfig{}

	prompt := agent.buildSystemPrompt(cfg)
	if contains(prompt, "Your role is:") {
		t.Error("should not mention role when empty")
	}
}

func TestBuildSystemPrompt_NoPersona(t *testing.T) {
	agent := &Agent{role: "qa"}
	cfg := ActionLoopConfig{PersonaInstructions: ""}

	prompt := agent.buildSystemPrompt(cfg)
	if !contains(prompt, "qa") {
		t.Error("prompt should mention qa role")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestParseActionsFromResponse_DoneAction(t *testing.T) {
	input := `{"actions":[{"type":"done","params":{"message":"Task complete"}}]}`
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(resp.Actions))
	}
	if resp.Actions[0].Type != "done" {
		t.Errorf("got type %q", resp.Actions[0].Type)
	}
	msg, _ := resp.Actions[0].Params["message"].(string)
	if msg != "Task complete" {
		t.Errorf("got message %q", msg)
	}
}

func TestParseActionsFromResponse_MultipleActions(t *testing.T) {
	input := `{"actions":[
		{"type":"bash","params":{"command":"make test"}},
		{"type":"read","params":{"path":"go.mod"}},
		{"type":"git_commit","params":{"message":"fix: test"}}
	]}`
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(resp.Actions))
	}
	expected := []string{"bash", "read", "git_commit"}
	for i, want := range expected {
		if resp.Actions[i].Type != want {
			t.Errorf("action %d: got type %q, want %q", i, resp.Actions[i].Type, want)
		}
	}
}

func TestParseActionsFromResponse_WithThinking(t *testing.T) {
	input := `{"thinking":"I need to check the files first","actions":[{"type":"read","params":{"path":"main.go"}}]}`
	resp, err := parseActionsFromResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Thinking != "I need to check the files first" {
		t.Errorf("got thinking %q", resp.Thinking)
	}
}

func TestFormatActionFeedback_EmptyOutput(t *testing.T) {
	action := LLMAction{Type: "git_push"}
	result := ActionResult{Type: "git_push", Success: true, Output: ""}

	feedback := formatActionFeedback(action, result)
	if !contains(feedback, "SUCCESS") {
		t.Error("should contain SUCCESS")
	}
}

func TestFormatActionFeedback_ExactlyAtLimit(t *testing.T) {
	action := LLMAction{Type: "bash"}
	output := make([]byte, 4000)
	for i := range output {
		output[i] = 'a'
	}
	result := ActionResult{Type: "bash", Success: true, Output: string(output)}

	feedback := formatActionFeedback(action, result)
	if contains(feedback, "truncated") {
		t.Error("4000 chars should NOT be truncated")
	}
}

func TestActionResult_Fields(t *testing.T) {
	r := ActionResult{
		Type:    "bash",
		Success: false,
		Output:  "some output",
		Error:   "permission denied",
	}
	if r.Type != "bash" {
		t.Errorf("got type %q", r.Type)
	}
	if r.Success {
		t.Error("should be false")
	}
}

func TestLLMAction_Fields(t *testing.T) {
	a := LLMAction{
		Type:   "write",
		Params: map[string]interface{}{"path": "main.go", "content": "package main"},
	}
	if a.Type != "write" {
		t.Errorf("got type %q", a.Type)
	}
	if a.Params["path"] != "main.go" {
		t.Error("params mismatch")
	}
}

func TestActionLoopConfig_Fields(t *testing.T) {
	cfg := ActionLoopConfig{
		MaxIterations:       30,
		ProviderEndpoint:    "http://llm:8000/v1",
		ProviderModel:       "gpt-4",
		ProviderAPIKey:      "sk-test",
		PersonaInstructions: "Be concise",
	}
	if cfg.MaxIterations != 30 {
		t.Errorf("got max iterations %d", cfg.MaxIterations)
	}
	if cfg.ProviderEndpoint != "http://llm:8000/v1" {
		t.Errorf("got endpoint %q", cfg.ProviderEndpoint)
	}
}
