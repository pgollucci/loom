package actions

import (
	"testing"
)

func TestParseSetupCommands_BareArray(t *testing.T) {
	cmds, err := parseSetupCommands(`["apt-get install -y nodejs", "npm install"]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] != "apt-get install -y nodejs" {
		t.Errorf("unexpected cmd[0]: %s", cmds[0])
	}
}

func TestParseSetupCommands_WrappedObject(t *testing.T) {
	cmds, err := parseSetupCommands(`{"commands": ["go mod download"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0] != "go mod download" {
		t.Errorf("unexpected: %v", cmds)
	}
}

func TestParseSetupCommands_MarkdownFenced(t *testing.T) {
	input := "```json\n[\"pip install -r requirements.txt\"]\n```"
	cmds, err := parseSetupCommands(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
}

func TestParseSetupCommands_EmptyArray(t *testing.T) {
	cmds, err := parseSetupCommands(`[]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(cmds))
	}
}

func TestParseSetupCommands_GenericObject(t *testing.T) {
	cmds, err := parseSetupCommands(`{"setup": ["make deps", "go mod tidy"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
}

func TestParseSetupCommands_Invalid(t *testing.T) {
	_, err := parseSetupCommands(`this is not json`)
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestHeuristicSetup_GoProject(t *testing.T) {
	m := &BuildEnvManager{ready: make(map[string]bool), running: make(map[string]bool), osFamilies: make(map[string]OSFamily)}
	entries := []string{"go.mod", "go.sum", "main.go", "internal/foo/bar.go"}
	cmds := m.heuristicSetup(entries, nil)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "cd /workspace && go mod download" {
		t.Errorf("unexpected: %s", cmds[0])
	}
}

func TestHeuristicSetup_NodeProject(t *testing.T) {
	m := &BuildEnvManager{ready: make(map[string]bool), running: make(map[string]bool), osFamilies: make(map[string]OSFamily)}
	entries := []string{"package.json", "src/index.js"}
	cmds := m.heuristicSetup(entries, nil)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
}

func TestHeuristicSetup_NodeProjectAlpine(t *testing.T) {
	m := &BuildEnvManager{ready: make(map[string]bool), running: make(map[string]bool), osFamilies: make(map[string]OSFamily)}
	entries := []string{"package.json", "src/index.js"}
	cmds := m.heuristicSetupForOS(entries, nil, OSFamilyAlpine)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "apk add --no-cache nodejs npm" {
		t.Errorf("expected alpine node install, got: %s", cmds[0])
	}
}

func TestHeuristicSetup_PythonProject(t *testing.T) {
	m := &BuildEnvManager{ready: make(map[string]bool), running: make(map[string]bool), osFamilies: make(map[string]OSFamily)}
	entries := []string{"requirements.txt", "app.py"}
	cmds := m.heuristicSetup(entries, nil)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
}

func TestHeuristicSetup_Empty(t *testing.T) {
	m := &BuildEnvManager{ready: make(map[string]bool), running: make(map[string]bool), osFamilies: make(map[string]OSFamily)}
	cmds := m.heuristicSetup([]string{"README.md"}, nil)
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(cmds))
	}
}

func TestInstallPackages_Debian(t *testing.T) {
	cmd := InstallPackages(OSFamilyDebian, []string{"curl", "git"})
	if cmd != "apt-get update -qq && apt-get install -y --no-install-recommends curl git" {
		t.Errorf("unexpected debian install cmd: %s", cmd)
	}
}

func TestInstallPackages_Alpine(t *testing.T) {
	cmd := InstallPackages(OSFamilyAlpine, []string{"curl", "git"})
	if cmd != "apk add --no-cache curl git" {
		t.Errorf("unexpected alpine install cmd: %s", cmd)
	}
}

func TestInstallPackages_Empty(t *testing.T) {
	cmd := InstallPackages(OSFamilyDebian, nil)
	if cmd != "" {
		t.Errorf("expected empty string for no packages, got: %s", cmd)
	}
}

func TestBuildEnvManager_IsReady(t *testing.T) {
	m := NewBuildEnvManager(nil)
	if m.IsReady("proj-1") {
		t.Fatal("expected not ready")
	}
	m.mu.Lock()
	m.ready["proj-1"] = true
	m.mu.Unlock()
	if !m.IsReady("proj-1") {
		t.Fatal("expected ready after setting")
	}
}

func TestBuildEnvManager_GetOSFamily(t *testing.T) {
	m := NewBuildEnvManager(nil)
	if m.GetOSFamily("proj-1") != OSFamilyUnknown {
		t.Fatal("expected unknown for unset project")
	}
	m.mu.Lock()
	m.osFamilies["proj-1"] = OSFamilyAlpine
	m.mu.Unlock()
	if m.GetOSFamily("proj-1") != OSFamilyAlpine {
		t.Fatal("expected alpine after setting")
	}
}
