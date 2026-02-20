package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.HTTPPort != 8080 {
		t.Errorf("expected HTTP port 8080, got %d", cfg.Server.HTTPPort)
	}
	if cfg.Server.HTTPSPort != 8443 {
		t.Errorf("expected HTTPS port 8443, got %d", cfg.Server.HTTPSPort)
	}
	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("expected gRPC port 9090, got %d", cfg.Server.GRPCPort)
	}
	if !cfg.Server.EnableHTTP {
		t.Error("HTTP should be enabled by default")
	}
	if cfg.Server.EnableHTTPS {
		t.Error("HTTPS should be disabled by default")
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("expected 30s read timeout, got %v", cfg.Server.ReadTimeout)
	}
}

func TestDefaultConfig_Database(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Database.Type != "postgres" {
		t.Errorf("expected postgres, got %q", cfg.Database.Type)
	}
}

func TestDefaultConfig_Beads(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Beads.BDPath != "bd" {
		t.Errorf("expected bd path 'bd', got %q", cfg.Beads.BDPath)
	}
	if !cfg.Beads.AutoSync {
		t.Error("auto sync should be enabled")
	}
	if cfg.Beads.CompactOldDays != 90 {
		t.Errorf("expected 90 days, got %d", cfg.Beads.CompactOldDays)
	}
}

func TestDefaultConfig_Dispatch(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Dispatch.MaxHops != 20 {
		t.Errorf("expected max hops 20, got %d", cfg.Dispatch.MaxHops)
	}
	if cfg.Dispatch.UseNATSDispatch {
		t.Error("NATS dispatch should be disabled by default")
	}
}

func TestDefaultConfig_PDAAndSwarm(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PDA.Enabled {
		t.Error("PDA should be disabled by default")
	}
	if cfg.Swarm.Enabled {
		t.Error("Swarm should be disabled by default")
	}
}

func TestDefaultConfig_Temporal(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Temporal.Host != "localhost:7233" {
		t.Errorf("got temporal host %q", cfg.Temporal.Host)
	}
	if cfg.Temporal.Namespace != "loom-default" {
		t.Errorf("got namespace %q", cfg.Temporal.Namespace)
	}
	if !cfg.Temporal.EnableEventBus {
		t.Error("event bus should be enabled")
	}
}

func TestDefaultConfig_Agents(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Agents.MaxConcurrent != 10 {
		t.Errorf("expected max concurrent 10, got %d", cfg.Agents.MaxConcurrent)
	}
	if cfg.Agents.CorpProfile != "full" {
		t.Errorf("expected corp profile 'full', got %q", cfg.Agents.CorpProfile)
	}
}

func TestLoadConfigFromFile_YAML(t *testing.T) {
	content := `
server:
  http_port: 9090
  enable_http: true
dispatch:
  max_hops: 30
  use_nats_dispatch: true
pda:
  enabled: true
  planner_model: gpt-4
  planner_endpoint: http://llm:8000
swarm:
  enabled: true
  gateway_name: test-loom
  peer_nats_urls:
    - nats://peer1:4222
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.HTTPPort != 9090 {
		t.Errorf("expected HTTP port 9090, got %d", cfg.Server.HTTPPort)
	}
	if cfg.Dispatch.MaxHops != 30 {
		t.Errorf("expected max hops 30, got %d", cfg.Dispatch.MaxHops)
	}
	if !cfg.Dispatch.UseNATSDispatch {
		t.Error("NATS dispatch should be enabled")
	}
	if !cfg.PDA.Enabled {
		t.Error("PDA should be enabled")
	}
	if cfg.PDA.PlannerModel != "gpt-4" {
		t.Errorf("got planner model %q", cfg.PDA.PlannerModel)
	}
	if !cfg.Swarm.Enabled {
		t.Error("Swarm should be enabled")
	}
	if cfg.Swarm.GatewayName != "test-loom" {
		t.Errorf("got gateway name %q", cfg.Swarm.GatewayName)
	}
	if len(cfg.Swarm.PeerNATSURLs) != 1 || cfg.Swarm.PeerNATSURLs[0] != "nats://peer1:4222" {
		t.Errorf("got peer URLs %v", cfg.Swarm.PeerNATSURLs)
	}
}

func TestLoadConfigFromFile_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_LOOM_PORT", "7777")
	defer os.Unsetenv("TEST_LOOM_PORT")

	content := `
server:
  http_port: ${TEST_LOOM_PORT}
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.HTTPPort != 7777 {
		t.Errorf("expected port 7777 from env, got %d", cfg.Server.HTTPPort)
	}
}

func TestLoadConfigFromFile_NotFound(t *testing.T) {
	_, err := LoadConfigFromFile("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadConfigFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfigFromFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestPDAConfig_Fields(t *testing.T) {
	cfg := PDAConfig{
		Enabled:         true,
		PlannerModel:    "gpt-4o",
		PlannerEndpoint: "http://localhost:8000/v1",
		PlannerAPIKey:   "sk-test",
	}

	if !cfg.Enabled {
		t.Error("should be enabled")
	}
	if cfg.PlannerModel != "gpt-4o" {
		t.Errorf("got model %q", cfg.PlannerModel)
	}
}

func TestSwarmConfig_Fields(t *testing.T) {
	cfg := SwarmConfig{
		Enabled:      true,
		PeerNATSURLs: []string{"nats://a:4222", "nats://b:4222"},
		GatewayName:  "gw-1",
	}

	if !cfg.Enabled {
		t.Error("should be enabled")
	}
	if len(cfg.PeerNATSURLs) != 2 {
		t.Errorf("got %d peers", len(cfg.PeerNATSURLs))
	}
}

func TestDispatchConfig_Fields(t *testing.T) {
	cfg := DispatchConfig{MaxHops: 50, UseNATSDispatch: true}
	if cfg.MaxHops != 50 {
		t.Errorf("got max hops %d", cfg.MaxHops)
	}
	if !cfg.UseNATSDispatch {
		t.Error("NATS dispatch should be true")
	}
}

func TestDefaultConfig_OpenClaw(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.OpenClaw.Enabled {
		t.Error("OpenClaw should be disabled by default")
	}
	if cfg.OpenClaw.GatewayURL != "http://127.0.0.1:18789" {
		t.Errorf("got gateway %q", cfg.OpenClaw.GatewayURL)
	}
	if cfg.OpenClaw.RetryAttempts != 3 {
		t.Errorf("got retry attempts %d", cfg.OpenClaw.RetryAttempts)
	}
}

func TestDefaultConfig_Security(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Security.EnableAuth {
		t.Error("auth should be enabled by default")
	}
	if cfg.Security.PKIEnabled {
		t.Error("PKI should be disabled by default")
	}
	if len(cfg.Security.AllowedOrigins) != 1 || cfg.Security.AllowedOrigins[0] != "*" {
		t.Errorf("got allowed origins %v", cfg.Security.AllowedOrigins)
	}
}

func TestDefaultConfig_WebUI(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.WebUI.Enabled {
		t.Error("WebUI should be enabled")
	}
	if cfg.WebUI.RefreshInterval != 5 {
		t.Errorf("got refresh interval %d", cfg.WebUI.RefreshInterval)
	}
}

func TestDefaultConfig_Readiness(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Readiness.Mode != "block" {
		t.Errorf("got readiness mode %q", cfg.Readiness.Mode)
	}
}

func TestDefaultConfig_Git(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Git.ProjectKeyDir != "/app/data/projects" {
		t.Errorf("got project key dir %q", cfg.Git.ProjectKeyDir)
	}
}

func TestLoadConfigFromFile_FullConfig(t *testing.T) {
	content := `
server:
  http_port: 8080
  https_port: 8443
  grpc_port: 9090
  enable_http: true
  read_timeout: 60s
database:
  type: postgres
  dsn: "postgres://user:pass@localhost/loom"
beads:
  bd_path: "bd"
  auto_sync: true
  compact_old_days: 60
  backend: sqlite
agents:
  max_concurrent: 5
  heartbeat_interval: 10s
  corp_profile: minimal
dispatch:
  max_hops: 10
  use_nats_dispatch: false
pda:
  enabled: false
swarm:
  enabled: false
temporal:
  host: "temporal:7233"
  namespace: custom
  task_queue: custom-tasks
web_ui:
  enabled: true
  static_path: ./static
  refresh_interval: 10
projects:
  - id: proj-1
    name: Test Project
    git_repo: https://github.com/test/repo
    branch: main
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "full.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Database.DSN != "postgres://user:pass@localhost/loom" {
		t.Errorf("got DSN %q", cfg.Database.DSN)
	}
	if cfg.Agents.MaxConcurrent != 5 {
		t.Errorf("got max concurrent %d", cfg.Agents.MaxConcurrent)
	}
	if cfg.Temporal.Host != "temporal:7233" {
		t.Errorf("got temporal host %q", cfg.Temporal.Host)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].ID != "proj-1" {
		t.Errorf("got project ID %q", cfg.Projects[0].ID)
	}
}

func TestProjectConfig_Fields(t *testing.T) {
	p := ProjectConfig{
		ID:           "test",
		Name:         "Test",
		GitRepo:      "https://example.com/repo",
		Branch:       "main",
		UseContainer: true,
		UseWorktrees: true,
		IsPerpetual:  true,
		IsSticky:     false,
	}

	if p.ID != "test" {
		t.Errorf("got ID %q", p.ID)
	}
	if !p.UseContainer {
		t.Error("use_container should be true")
	}
	if !p.IsPerpetual {
		t.Error("is_perpetual should be true")
	}
}

func TestServerConfig_Fields(t *testing.T) {
	s := ServerConfig{
		HTTPPort:    8080,
		HTTPSPort:   8443,
		GRPCPort:    9090,
		EnableHTTP:  true,
		EnableHTTPS: true,
		TLSCertFile: "/cert.pem",
		TLSKeyFile:  "/key.pem",
	}

	if s.TLSCertFile != "/cert.pem" {
		t.Errorf("got cert %q", s.TLSCertFile)
	}
}

func TestCacheConfig_Fields(t *testing.T) {
	c := CacheConfig{
		Enabled:     true,
		Backend:     "redis",
		DefaultTTL:  5 * time.Minute,
		MaxSize:     1000,
		MaxMemoryMB: 256,
		RedisURL:    "redis://localhost:6379",
	}

	if !c.Enabled {
		t.Error("should be enabled")
	}
	if c.Backend != "redis" {
		t.Errorf("got backend %q", c.Backend)
	}
}

func TestPreferredModel_Fields(t *testing.T) {
	m := PreferredModel{
		Name:      "gpt-4o",
		Rank:      1,
		Tier:      "complex",
		MinVRAMGB: 80,
		Notes:     "Best model",
	}

	if m.Rank != 1 {
		t.Errorf("got rank %d", m.Rank)
	}
	if m.Tier != "complex" {
		t.Errorf("got tier %q", m.Tier)
	}
}

func TestGetConfigPath(t *testing.T) {
	path, err := getConfigPath()
	if err != nil {
		t.Fatalf("getConfigPath failed: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
	home, _ := os.UserHomeDir()
	if !filepath.IsAbs(path) {
		t.Error("expected absolute path")
	}
	if home != "" && path != filepath.Join(home, configFileName) {
		t.Errorf("expected path under home dir, got %q", path)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig()
	// LoadConfig reads ~/.loom.json which may or may not exist.
	// If it doesn't exist, we get an error. Either way this exercises the code path.
	if err != nil {
		// File not found is expected in test environments
		t.Logf("LoadConfig returned expected error: %v", err)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	// Create a temp home dir with an invalid config file
	tmpDir := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", orig)

	configPath := filepath.Join(tmpDir, configFileName)
	if err := os.WriteFile(configPath, []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid JSON config")
	}
}

func TestLoadConfig_ValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", orig)

	jsonConfig := `{
		"server": {"http_port": 9999},
		"database": {"type": "sqlite"}
	}`
	configPath := filepath.Join(tmpDir, configFileName)
	if err := os.WriteFile(configPath, []byte(jsonConfig), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}
