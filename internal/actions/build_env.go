package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/internal/containers"
	"github.com/jordanhubbard/loom/internal/provider"
)

// BuildEnvManager tracks whether each project container has had its build
// environment initialised (dependencies installed, toolchain verified) and,
// if not, asks an LLM provider for the required setup commands.
// ContainerSnapshotter is called after successful env init to persist the
// container's current state as a new image layer.
type ContainerSnapshotter func(ctx context.Context, projectID string)

type BuildEnvManager struct {
	mu         sync.RWMutex
	ready      map[string]bool     // projectID → initialised
	running    map[string]bool     // projectID → currently initialising
	osFamilies map[string]OSFamily // projectID → detected OS family
	registry   *provider.Registry
	onReady    ContainerSnapshotter // optional: called after successful init
}

func NewBuildEnvManager(registry *provider.Registry) *BuildEnvManager {
	return &BuildEnvManager{
		ready:      make(map[string]bool),
		running:    make(map[string]bool),
		osFamilies: make(map[string]OSFamily),
		registry:   registry,
	}
}

// SetOnReady registers a callback invoked after each project's environment
// is successfully initialised (e.g. to snapshot the container).
func (m *BuildEnvManager) SetOnReady(fn ContainerSnapshotter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReady = fn
}

// GetOSFamily returns the cached OS family for a project, or OSFamilyUnknown.
func (m *BuildEnvManager) GetOSFamily(projectID string) OSFamily {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if f, ok := m.osFamilies[projectID]; ok {
		return f
	}
	return OSFamilyUnknown
}

// IsReady returns true if the project's build env has already been initialised.
func (m *BuildEnvManager) IsReady(projectID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ready[projectID]
}

// EnsureReady checks whether the container already has a marker file
// (.loom-build-ready). If not, it reads the project file listing, asks an
// LLM for the install commands, executes them, and writes the marker.
func (m *BuildEnvManager) EnsureReady(ctx context.Context, projectID string, agent *containers.ProjectAgentClient) error {
	if m.IsReady(projectID) {
		return nil
	}

	// Prevent concurrent initialisation of the same project
	m.mu.Lock()
	if m.ready[projectID] {
		m.mu.Unlock()
		return nil
	}
	if m.running[projectID] {
		m.mu.Unlock()
		// Wait for the other goroutine to finish
		for i := 0; i < 120; i++ {
			time.Sleep(time.Second)
			m.mu.RLock()
			done := m.ready[projectID]
			m.mu.RUnlock()
			if done {
				return nil
			}
		}
		return fmt.Errorf("timed out waiting for build env init for %s", projectID)
	}
	m.running[projectID] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.running, projectID)
		m.mu.Unlock()
	}()

	// Fast path: marker file already present in the container
	markerRes, err := agent.ReadFile(ctx, ".loom-env-ready")
	if err == nil && markerRes != nil && strings.TrimSpace(markerRes.Content) != "" {
		log.Printf("[BuildEnv] Project %s already initialised (marker found)", projectID)
		m.mu.Lock()
		m.ready[projectID] = true
		m.mu.Unlock()
		return nil
	}

	log.Printf("[BuildEnv] Initialising build environment for project %s", projectID)

	// 0. Detect container OS family (Alpine vs Debian/Ubuntu)
	osFamily := DetectOSFamily(ctx, agent)
	osFamilyName := "debian"
	if osFamily == OSFamilyAlpine {
		osFamilyName = "alpine"
	}
	log.Printf("[BuildEnv] Detected OS family for %s: %s", projectID, osFamilyName)
	m.mu.Lock()
	m.osFamilies[projectID] = osFamily
	m.mu.Unlock()

	// 1. Read the project file tree to understand what we're dealing with
	tree, err := agent.ReadTree(ctx, ".", 2)
	if err != nil {
		return fmt.Errorf("failed to read project tree: %w", err)
	}

	// 2. Read key manifest files to give the LLM more context
	manifests := m.readManifests(ctx, agent, tree.Entries)

	// 3. Ask an LLM what commands are needed
	commands, err := m.askLLMForSetup(ctx, projectID, tree.Entries, manifests)
	if err != nil {
		log.Printf("[BuildEnv] LLM query failed for %s, trying heuristic fallback: %v", projectID, err)
		commands = m.heuristicSetupForOS(tree.Entries, manifests, osFamily)
	}

	if len(commands) == 0 {
		log.Printf("[BuildEnv] No setup commands needed for project %s", projectID)
		m.markReady(ctx, projectID, agent, "no-op", osFamilyName)
		return nil
	}

	// 4. Execute the setup commands
	log.Printf("[BuildEnv] Running %d setup commands for project %s", len(commands), projectID)
	for i, cmd := range commands {
		log.Printf("[BuildEnv] [%d/%d] %s", i+1, len(commands), cmd)
		result, execErr := agent.ExecSync(ctx, cmd, "/workspace", 300)
		if execErr != nil {
			log.Printf("[BuildEnv] Command failed (non-fatal): %v", execErr)
			continue
		}
		if result.ExitCode != 0 {
			log.Printf("[BuildEnv] Command exited %d (non-fatal): %s", result.ExitCode, result.Stderr)
		}
	}

	m.markReady(ctx, projectID, agent, strings.Join(commands, "\n"), osFamilyName)
	return nil
}

func (m *BuildEnvManager) markReady(ctx context.Context, projectID string, agent *containers.ProjectAgentClient, cmds, osFamily string) {
	marker := fmt.Sprintf("initialised=%s\nos_family=%s\ncommands=%s\n", time.Now().UTC().Format(time.RFC3339), osFamily, cmds)
	_, _ = agent.WriteFile(ctx, ".loom-env-ready", marker)
	// Persist to history volume so it survives container rebuilds
	_, _ = agent.WriteFile(ctx, "/root/.loom-history/.loom-env-ready", marker)
	m.mu.Lock()
	m.ready[projectID] = true
	onReady := m.onReady
	m.mu.Unlock()
	log.Printf("[BuildEnv] Project %s build environment ready", projectID)

	// Snapshot the container so installed tools survive restarts.
	if onReady != nil {
		onReady(ctx, projectID)
	}
}

// readManifests reads common dependency/manifest files from the container.
func (m *BuildEnvManager) readManifests(ctx context.Context, agent *containers.ProjectAgentClient, entries []string) map[string]string {
	interesting := map[string]bool{
		"go.mod": true, "go.sum": false,
		"package.json": true, "package-lock.json": false,
		"requirements.txt": true, "setup.py": true, "pyproject.toml": true,
		"Cargo.toml": true, "Gemfile": true, "pom.xml": true,
		"build.gradle": true, "Makefile": true, "CMakeLists.txt": true,
		"composer.json": true, "mix.exs": true, "Dockerfile": true,
	}

	result := make(map[string]string)
	for _, entry := range entries {
		name := entry
		// Strip directory prefix
		if idx := strings.LastIndex(entry, "/"); idx >= 0 {
			name = entry[idx+1:]
		}
		if !interesting[name] {
			continue
		}
		res, err := agent.ReadFile(ctx, entry)
		if err == nil && res != nil {
			content := res.Content
			if len(content) > 2000 {
				content = content[:2000] + "\n... (truncated)"
			}
			result[entry] = content
		}
	}
	return result
}

// askLLMForSetup queries an active provider for the shell commands needed to
// install build dependencies inside the container.
func (m *BuildEnvManager) askLLMForSetup(ctx context.Context, projectID string, entries []string, manifests map[string]string) ([]string, error) {
	if m.registry == nil {
		return nil, fmt.Errorf("no provider registry")
	}

	providers := m.registry.ListActive()
	if len(providers) == 0 {
		return nil, fmt.Errorf("no active providers")
	}

	// Build the prompt
	var sb strings.Builder
	sb.WriteString("Files in project:\n")
	for _, e := range entries {
		sb.WriteString("  " + e + "\n")
	}
	sb.WriteString("\nManifest contents:\n")
	for path, content := range manifests {
		sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", path, content))
	}

	prompt := fmt.Sprintf(`You are a DevOps expert. A project container (Ubuntu 22.04 with git, curl, wget, build-essential, and Go 1.25 pre-installed) needs its build dependencies installed.

Examine the project files below and return ONLY a JSON array of shell commands to install all missing dependencies. Each command must be a single string that can be passed to "bash -c".

Rules:
- Only include commands for tools/packages NOT already installed (git, curl, wget, build-essential, Go are present).
- For Go projects: run "go mod download" if go.mod exists.
- For Node.js projects: install Node.js/npm first, then "npm install".
- For Python projects: install python3/pip, then "pip install -r requirements.txt" or equivalent.
- For Rust projects: install rustup/cargo, then "cargo fetch".
- If Makefile exists, include any "make deps" or similar dependency targets if obvious.
- Return an empty array [] if nothing is needed.
- Return ONLY valid JSON. No markdown, no explanation.

%s`, sb.String())

	// Try each provider until one succeeds
	for _, p := range providers {
		if p.Config == nil {
			continue
		}

		resp, err := m.registry.SendChatCompletion(ctx, p.Config.ID, &provider.ChatCompletionRequest{
			Model: p.Config.Model,
			Messages: []provider.ChatMessage{
				{Role: "user", Content: prompt},
			},
			Temperature: 0.1,
			MaxTokens:   1024,
			ResponseFormat: &provider.ResponseFormat{
				Type: "json_object",
			},
		})
		if err != nil {
			log.Printf("[BuildEnv] Provider %s failed: %v", p.Config.ID, err)
			continue
		}

		if len(resp.Choices) == 0 {
			continue
		}

		text := strings.TrimSpace(resp.Choices[0].Message.Content)
		commands, parseErr := parseSetupCommands(text)
		if parseErr != nil {
			log.Printf("[BuildEnv] Failed to parse LLM response from %s: %v\nRaw: %s", p.Config.ID, parseErr, text[:min(len(text), 200)])
			continue
		}

		log.Printf("[BuildEnv] LLM (%s) recommended %d commands for project %s", p.Config.ID, len(commands), projectID)
		return commands, nil
	}

	return nil, fmt.Errorf("all providers failed")
}

// parseSetupCommands extracts a string array from the LLM's JSON response.
// Accepts both bare arrays and {"commands": [...]} objects.
func parseSetupCommands(text string) ([]string, error) {
	text = strings.TrimSpace(text)

	// Strip markdown fences if present
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	// Try bare array
	var cmds []string
	if err := json.Unmarshal([]byte(text), &cmds); err == nil {
		return cmds, nil
	}

	// Try {"commands": [...]}
	var obj struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal([]byte(text), &obj); err == nil && len(obj.Commands) > 0 {
		return obj.Commands, nil
	}

	// Try generic object with any array value
	var generic map[string]interface{}
	if err := json.Unmarshal([]byte(text), &generic); err == nil {
		for _, v := range generic {
			if arr, ok := v.([]interface{}); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok {
						cmds = append(cmds, s)
					}
				}
				if len(cmds) > 0 {
					return cmds, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("could not parse commands from: %s", text[:min(len(text), 100)])
}

// OSFamily represents the container's base OS package manager family.
type OSFamily int

const (
	OSFamilyDebian OSFamily = iota // apt-get (Ubuntu, Debian)
	OSFamilyAlpine                 // apk (Alpine)
	OSFamilyUnknown
)

func (f OSFamily) String() string {
	switch f {
	case OSFamilyDebian:
		return "debian"
	case OSFamilyAlpine:
		return "alpine"
	default:
		return "unknown"
	}
}

// DetectOSFamily probes the container to determine its package manager.
func DetectOSFamily(ctx context.Context, agent *containers.ProjectAgentClient) OSFamily {
	res, err := agent.ExecSync(ctx, "cat /etc/os-release", "/", 10)
	if err != nil || res == nil {
		return OSFamilyUnknown
	}
	lower := strings.ToLower(res.Stdout)
	if strings.Contains(lower, "alpine") {
		return OSFamilyAlpine
	}
	if strings.Contains(lower, "ubuntu") || strings.Contains(lower, "debian") {
		return OSFamilyDebian
	}
	// Fallback: check which package manager exists
	if apkRes, _ := agent.ExecSync(ctx, "which apk", "/", 5); apkRes != nil && apkRes.ExitCode == 0 {
		return OSFamilyAlpine
	}
	return OSFamilyDebian
}

// InstallPackages generates the correct install command for the detected OS.
func InstallPackages(osFamily OSFamily, packages []string) string {
	if len(packages) == 0 {
		return ""
	}
	pkgList := strings.Join(packages, " ")
	switch osFamily {
	case OSFamilyAlpine:
		return "apk add --no-cache " + pkgList
	default:
		return "apt-get update -qq && apt-get install -y --no-install-recommends " + pkgList
	}
}

// heuristicSetup returns setup commands based on file patterns, without LLM.
// It adapts to the container's OS family for package installation commands.
func (m *BuildEnvManager) heuristicSetup(entries []string, manifests map[string]string) []string {
	return m.heuristicSetupForOS(entries, manifests, OSFamilyDebian)
}

// heuristicSetupForOS is the OS-aware version of heuristicSetup.
func (m *BuildEnvManager) heuristicSetupForOS(entries []string, manifests map[string]string, osFamily OSFamily) []string {
	has := func(name string) bool {
		for _, e := range entries {
			if e == name || strings.HasSuffix(e, "/"+name) {
				return true
			}
		}
		return false
	}

	var cmds []string

	if has("go.mod") {
		cmds = append(cmds, "cd /workspace && go mod download")
	}
	if has("package.json") {
		switch osFamily {
		case OSFamilyAlpine:
			cmds = append(cmds, "apk add --no-cache nodejs npm")
		default:
			cmds = append(cmds, "curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && apt-get install -y nodejs")
		}
		cmds = append(cmds, "cd /workspace && npm install")
	}
	if has("requirements.txt") {
		cmds = append(cmds, InstallPackages(osFamily, []string{"python3", "py3-pip"}))
		cmds = append(cmds, "cd /workspace && pip3 install -r requirements.txt")
	}
	if has("pyproject.toml") {
		cmds = append(cmds, InstallPackages(osFamily, []string{"python3", "py3-pip"}))
		cmds = append(cmds, "cd /workspace && pip3 install -e .")
	}
	if has("Cargo.toml") {
		cmds = append(cmds,
			"curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y",
			"source $HOME/.cargo/env && cd /workspace && cargo fetch",
		)
	}
	if has("Gemfile") {
		switch osFamily {
		case OSFamilyAlpine:
			cmds = append(cmds, "apk add --no-cache ruby ruby-dev ruby-bundler")
		default:
			cmds = append(cmds, "apt-get update && apt-get install -y ruby-full")
		}
		cmds = append(cmds, "cd /workspace && bundle install")
	}

	return cmds
}
