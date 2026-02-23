package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jordanhubbard/loom/internal/projectagent"
	"github.com/jordanhubbard/loom/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	var (
		projectID         = flag.String("project-id", os.Getenv("PROJECT_ID"), "Project ID")
		controlPlaneURL   = flag.String("control-plane-url", os.Getenv("CONTROL_PLANE_URL"), "Control plane URL")
		port              = flag.String("port", getEnvOrDefault("PORT", "8090"), "HTTP port for agent API")
		workDir           = flag.String("work-dir", getEnvOrDefault("WORK_DIR", "/workspace"), "Project workspace directory")
		heartbeatInterval = flag.Duration("heartbeat", 30*time.Second, "Heartbeat interval")
		natsURL           = flag.String("nats-url", os.Getenv("NATS_URL"), "NATS server URL")
		role              = flag.String("role", os.Getenv("AGENT_ROLE"), "Agent role (coder, reviewer, qa, pm, architect). Empty = run all roles.")
		providerEndpoint  = flag.String("provider-endpoint", os.Getenv("PROVIDER_ENDPOINT"), "LLM provider endpoint")
		providerModel     = flag.String("provider-model", os.Getenv("PROVIDER_MODEL"), "LLM model name")
		providerAPIKey    = flag.String("provider-api-key", getEnvOrDefault("PROVIDER_API_KEY", "default-api-key"), "LLM provider API key")
		personaPath       = flag.String("persona-path", os.Getenv("PERSONA_PATH"), "Path to persona file (single-role mode)")
		personaBasePath   = flag.String("persona-base-path", getEnvOrDefault("PERSONA_BASE_PATH", "/app/personas"), "Base dir for per-role persona files (multi-role mode)")
		actionLoop        = flag.Bool("action-loop", getEnvBool("ACTION_LOOP_ENABLED", false), "Enable multi-turn action loop")
		maxIterations     = flag.Int("max-iterations", getEnvInt("MAX_LOOP_ITERATIONS", 20), "Max action loop iterations")
	)

	flag.Parse()

	if *projectID == "" {
		log.Fatal("PROJECT_ID is required")
	}
	if *controlPlaneURL == "" {
		log.Fatal("CONTROL_PLANE_URL is required")
	}

	// Initialize OpenTelemetry tracing.
	if otelEndpoint := os.Getenv("OTEL_ENDPOINT"); otelEndpoint != "" {
		serviceName := "loom-agent"
		if *role != "" {
			serviceName = fmt.Sprintf("loom-agent-%s", *role)
		}
		shutdown, err := telemetry.InitTelemetry(context.Background(), serviceName, otelEndpoint)
		if err != nil {
			log.Printf("Warning: OTel init failed: %v", err)
		} else {
			defer shutdown(context.Background())
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down agent...")
		cancel()
	}()

	serviceID := getEnvOrDefault("SERVICE_ID", fmt.Sprintf("agent-%s", *projectID))
	instanceID := getEnvOrDefault("INSTANCE_ID", "")

	// Multi-role mode: AGENT_ROLE is unset → run all roles in one process.
	// Single-role mode: AGENT_ROLE is set → backward-compatible single agent.
	if *role == "" {
		log.Printf("Starting multi-role agent container (project=%s, roles=%v)", *projectID, projectagent.DefaultRoles)
		orch := projectagent.NewInContainerOrchestrator(projectagent.OrchestratorConfig{
			ProjectID:         *projectID,
			ControlPlaneURL:   *controlPlaneURL,
			NatsURL:           *natsURL,
			WorkDir:           *workDir,
			HeartbeatInterval: *heartbeatInterval,
			ServiceID:         serviceID,
			InstanceID:        instanceID,
			ProviderEndpoint:  *providerEndpoint,
			ProviderModel:     *providerModel,
			ProviderAPIKey:    *providerAPIKey,
			PersonaBasePath:   *personaBasePath,
			ActionLoopEnabled: *actionLoop,
			MaxLoopIterations: *maxIterations,
		})
		if err := orch.Start(ctx); err != nil && err != context.Canceled {
			log.Fatalf("Orchestrator error: %v", err)
		}
		log.Println("Agent stopped")
		return
	}

	// ── Single-role mode (backward compatible) ──────────────────────────────
	log.Printf("Starting Loom Agent Service (single-role mode)")
	log.Printf("  Project ID: %s", *projectID)
	log.Printf("  Role: %s", *role)
	log.Printf("  Control Plane: %s", *controlPlaneURL)
	log.Printf("  Work Directory: %s", *workDir)
	log.Printf("  Listen Port: %s", *port)

	agent, err := projectagent.New(projectagent.Config{
		ProjectID:         *projectID,
		ControlPlaneURL:   *controlPlaneURL,
		WorkDir:           *workDir,
		HeartbeatInterval: *heartbeatInterval,
		NatsURL:           *natsURL,
		ServiceID:         serviceID,
		InstanceID:        instanceID,
		Role:              *role,
		ProviderEndpoint:  *providerEndpoint,
		ProviderModel:     *providerModel,
		ProviderAPIKey:    *providerAPIKey,
		PersonaPath:       *personaPath,
		ActionLoopEnabled: *actionLoop,
		MaxLoopIterations: *maxIterations,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	mux := http.NewServeMux()
	agent.RegisterHandlers(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", *port),
		Handler: otelhttp.NewHandler(mux, "loom-agent-http"),
	}

	go func() {
		if err := agent.Start(ctx); err != nil && err != context.Canceled {
			log.Printf("Agent background tasks error: %v", err)
		}
	}()

	go func() {
		log.Printf("Agent listening on :%s", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	log.Println("Agent stopped")
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultValue
	}
	return b
}

func getEnvInt(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return i
}
