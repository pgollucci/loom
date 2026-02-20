package projectagent

import (
	"context"
	"testing"
	"time"
)

func TestNewAgent_RequiresProjectID(t *testing.T) {
	_, err := New(Config{ControlPlaneURL: "http://localhost:8080"})
	if err == nil {
		t.Error("expected error when project_id is empty")
	}
}

func TestNewAgent_RequiresControlPlaneURL(t *testing.T) {
	_, err := New(Config{ProjectID: "proj-1"})
	if err == nil {
		t.Error("expected error when control_plane_url is empty")
	}
}

func TestNewAgent_Defaults(t *testing.T) {
	agent, err := New(Config{
		ProjectID:       "proj-1",
		ControlPlaneURL: "http://localhost:8080",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if agent.config.WorkDir != "/workspace" {
		t.Errorf("default work dir should be /workspace, got %q", agent.config.WorkDir)
	}
	if agent.config.HeartbeatInterval != 30*time.Second {
		t.Errorf("default heartbeat should be 30s, got %v", agent.config.HeartbeatInterval)
	}
	if agent.config.MaxLoopIterations != 20 {
		t.Errorf("default max iterations should be 20, got %d", agent.config.MaxLoopIterations)
	}
}

func TestNewAgent_WithRole(t *testing.T) {
	agent, err := New(Config{
		ProjectID:       "proj-1",
		ControlPlaneURL: "http://localhost:8080",
		Role:            "coder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.role != "coder" {
		t.Errorf("expected role coder, got %q", agent.role)
	}
}

func TestNewAgent_CustomValues(t *testing.T) {
	agent, err := New(Config{
		ProjectID:         "proj-1",
		ControlPlaneURL:   "http://cp:8080",
		WorkDir:           "/custom",
		HeartbeatInterval: 10 * time.Second,
		MaxLoopIterations: 50,
		Role:              "qa",
		ProviderEndpoint:  "http://llm:8000/v1",
		ProviderModel:     "gpt-4",
		ActionLoopEnabled: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if agent.config.WorkDir != "/custom" {
		t.Errorf("got work_dir %q", agent.config.WorkDir)
	}
	if agent.config.MaxLoopIterations != 50 {
		t.Errorf("got max_iterations %d", agent.config.MaxLoopIterations)
	}
	if !agent.config.ActionLoopEnabled {
		t.Error("action loop should be enabled")
	}
}

func TestNewAgent_PersonaPathNotFound(t *testing.T) {
	agent, err := New(Config{
		ProjectID:       "proj-1",
		ControlPlaneURL: "http://localhost:8080",
		PersonaPath:     "/nonexistent/persona.txt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should log warning but not fail
	if agent.personaInstructions != "" {
		t.Errorf("expected empty persona, got %q", agent.personaInstructions)
	}
}

func TestExecuteAction_UnsupportedType(t *testing.T) {
	agent := &Agent{config: Config{WorkDir: "/tmp"}}
	action := LLMAction{Type: "unsupported_action"}

	result := agent.executeAction(context.Background(), action)
	if result.Success {
		t.Error("unsupported action should fail")
	}
	if result.Error == "" {
		t.Error("should have error message")
	}
}
