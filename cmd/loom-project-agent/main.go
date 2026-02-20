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
		role              = flag.String("role", os.Getenv("AGENT_ROLE"), "Agent role (coder, reviewer, qa, pm, architect)")
		providerEndpoint  = flag.String("provider-endpoint", os.Getenv("PROVIDER_ENDPOINT"), "LLM provider endpoint")
		providerModel     = flag.String("provider-model", os.Getenv("PROVIDER_MODEL"), "LLM model name")
		providerAPIKey    = flag.String("provider-api-key", os.Getenv("PROVIDER_API_KEY"), "LLM provider API key")
		personaPath       = flag.String("persona-path", os.Getenv("PERSONA_PATH"), "Path to persona instructions file")
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

	log.Printf("Starting Loom Agent Service")
	log.Printf("  Project ID: %s", *projectID)
	log.Printf("  Control Plane: %s", *controlPlaneURL)
	log.Printf("  Work Directory: %s", *workDir)
	log.Printf("  Listen Port: %s", *port)
	if *role != "" {
		log.Printf("  Role: %s", *role)
	}
	if *providerEndpoint != "" {
		log.Printf("  Provider: %s (model: %s)", *providerEndpoint, *providerModel)
	}
	if *natsURL != "" {
		log.Printf("  NATS: %s", *natsURL)
	}

	// Initialize OpenTelemetry tracing for agent
	if otelEndpoint := os.Getenv("OTEL_ENDPOINT"); otelEndpoint != "" {
		serviceName := fmt.Sprintf("loom-agent-%s", *role)
		if *role == "" {
			serviceName = "loom-agent"
		}
		shutdown, err := telemetry.InitTelemetry(context.Background(), serviceName, otelEndpoint)
		if err != nil {
			log.Printf("Warning: OTel init failed: %v", err)
		} else {
			defer shutdown(context.Background())
		}
	}

	agent, err := projectagent.New(projectagent.Config{
		ProjectID:         *projectID,
		ControlPlaneURL:   *controlPlaneURL,
		WorkDir:           *workDir,
		HeartbeatInterval: *heartbeatInterval,
		NatsURL:           *natsURL,
		ServiceID:         getEnvOrDefault("SERVICE_ID", fmt.Sprintf("agent-%s", *projectID)),
		InstanceID:        getEnvOrDefault("INSTANCE_ID", ""),
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := agent.Start(ctx); err != nil {
			log.Printf("Agent background tasks error: %v", err)
		}
	}()

	go func() {
		log.Printf("Agent listening on :%s", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down agent...")
	cancel()

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
