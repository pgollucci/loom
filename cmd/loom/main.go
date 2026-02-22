package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"net"

	"github.com/jordanhubbard/loom/internal/api"
	"github.com/jordanhubbard/loom/internal/audit"
	"github.com/jordanhubbard/loom/internal/auth"
	"github.com/jordanhubbard/loom/internal/automerge"
	internalconnectors "github.com/jordanhubbard/loom/internal/connectors"
	"github.com/jordanhubbard/loom/internal/hotreload"
	"github.com/jordanhubbard/loom/internal/keymanager"
	"github.com/jordanhubbard/loom/internal/loom"
	"github.com/jordanhubbard/loom/internal/telemetry"
	"github.com/jordanhubbard/loom/pkg/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"

	pb "github.com/jordanhubbard/loom/api/proto/connectors"
)

const version = "0.1.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	showHelp := flag.Bool("help", false, "Show help message")
	flag.Parse()

	if *showHelp {
		printHelp()
		return
	}

	if *showVersion {
		fmt.Printf("Loom v%s\n", version)
		return
	}

	cfg, err := config.LoadConfigFromFile(*configPath)
	if err != nil {
		log.Fatalf("failed to load config from %s: %v", *configPath, err)
	}

	// Override with environment variables if set
	if temporalHost := os.Getenv("TEMPORAL_HOST"); temporalHost != "" {
		cfg.Temporal.Host = temporalHost
		log.Printf("Using Temporal host from environment: %s", temporalHost)
	}
	if temporalNamespace := os.Getenv("TEMPORAL_NAMESPACE"); temporalNamespace != "" {
		cfg.Temporal.Namespace = temporalNamespace
		log.Printf("Using Temporal namespace from environment: %s", temporalNamespace)
	}

	arb, err := loom.New(cfg)
	if err != nil {
		log.Fatalf("failed to create loom: %v", err)
	}

	// Initialize key manager before Loom.Initialize() so Temporal activities
	// can use it for provider API key retrieval during heartbeats.
	keyStorePath := filepath.Join(".", ".keys.json")
	km := keymanager.NewKeyManager(keyStorePath)

	password := loadPassword()
	if password == "" {
		log.Printf("Warning: No password found. Using default password. Set LOOM_PASSWORD environment variable or create .env file")
		password = "loom-default-password"
	}

	if err := km.Unlock(password); err != nil {
		log.Printf("Password unlock failed: %v. Trying default password...", err)
		if err := km.Unlock("loom-default-password"); err != nil {
			log.Fatalf("Failed to unlock key manager with both passwords: %v", err)
		}
	}

	arb.SetKeyManager(km)

	// Initialize OpenTelemetry
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelEndpoint == "" {
		otelEndpoint = "otel-collector:4317"
	}
	shutdownTelemetry, err := telemetry.InitTelemetry(context.Background(), "loom", otelEndpoint)
	if err != nil {
		log.Printf("Warning: Failed to initialize telemetry: %v", err)
	} else {
		defer func() {
			if err := shutdownTelemetry(context.Background()); err != nil {
				log.Printf("Error shutting down telemetry: %v", err)
			}
		}()
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := arb.Initialize(runCtx); err != nil {
		log.Fatalf("failed to initialize loom: %v", err)
	}

	// Initialize hot-reload for development
	var hrManager *hotreload.Manager
	if cfg.HotReload.Enabled {
		hrManager, err = hotreload.NewManager(
			cfg.HotReload.Enabled,
			cfg.HotReload.WatchDirs,
			cfg.HotReload.Patterns,
		)
		if err != nil {
			log.Printf("Hot-reload initialization failed: %v", err)
		} else {
			defer hrManager.Close()
		}
	}

	go arb.StartMaintenanceLoop(runCtx)

	// Task executor: direct bead-claim → ExecuteTaskWithLoop loop per project.
	// Bypasses Temporal, NATS, and the WorkerPool for reliable execution.
	log.Printf("Starting task executor")
	go arb.StartTaskExecutor(runCtx)

	// Self-audit loop: periodically run build/test/lint and file beads for failures.
	// Disabled by default via env var. Set SELF_AUDIT_INTERVAL_MINUTES to enable.
	selfAuditInterval := 0
	if interval := os.Getenv("SELF_AUDIT_INTERVAL_MINUTES"); interval != "" {
		if n, err := fmt.Sscanf(interval, "%d", &selfAuditInterval); err == nil && n == 1 {
			log.Printf("Self-audit enabled with %d minute interval", selfAuditInterval)
		}
	}
	if selfAuditInterval > 0 {
		selfAuditRunner := audit.NewRunner("loom", ".", selfAuditInterval, arb)
		go selfAuditRunner.Start(runCtx)
	}

	autoMergeInterval := 0
	if interval := os.Getenv("AUTO_MERGE_INTERVAL_MINUTES"); interval != "" {
		if n, err := fmt.Sscanf(interval, "%d", &autoMergeInterval); err == nil && n == 1 {
			log.Printf("Auto-merge enabled with %d minute interval", autoMergeInterval)
		}
	}
	if autoMergeInterval > 0 {
		autoMergeRunner := automerge.NewRunner(arb)
		go autoMergeRunner.Start(runCtx, time.Duration(autoMergeInterval)*time.Minute)
	}

	// Initialize auth manager (JWT + API key support)
	authManager := auth.NewManager(cfg.Security.JWTSecret)

	apiServer := api.NewServer(arb, km, authManager, cfg)
	handler := apiServer.SetupRoutes()

	// Add hot-reload WebSocket endpoint if enabled
	if hrManager != nil && hrManager.IsEnabled() {
		mux := http.NewServeMux()
		mux.Handle("/", handler)
		mux.HandleFunc("/ws/hotreload", hrManager.GetServer().HandleWebSocket)
		mux.HandleFunc("/api/v1/hotreload/status", hrManager.GetServer().HandleStatus)
		handler = mux
		log.Println("[HotReload] WebSocket endpoint registered at /ws/hotreload")
	}

	// Wrap handler with OpenTelemetry instrumentation
	handler = otelhttp.NewHandler(handler, "loom-http-server")

	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.HTTPPort),
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		os.WriteFile("/tmp/http-server-starting.txt", []byte("HTTP SERVER STARTING\n"), 0644)
		log.Printf("Loom API listening on %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	// Start gRPC ConnectorsService
	grpcPort := cfg.Server.GRPCPort
	if grpcPort == 0 {
		grpcPort = 9090
	}
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Printf("Warning: failed to start gRPC listener on :%d: %v", grpcPort, err)
	} else {
		grpcSrv := grpc.NewServer()
		pb.RegisterConnectorsServiceServer(grpcSrv, internalconnectors.NewGRPCServer(arb.GetConnectorManager()))
		log.Printf("gRPC ConnectorsService listening on :%d", grpcPort)
		go func() {
			if err := grpcSrv.Serve(grpcListener); err != nil {
				log.Printf("gRPC server stopped: %v", err)
			}
		}()
		defer grpcSrv.GracefulStop()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = httpSrv.Shutdown(shutdownCtx)
	arb.Shutdown()

}

func loadPassword() string {
	// First, check environment variable
	if pwd := os.Getenv("LOOM_PASSWORD"); pwd != "" {
		return pwd
	}

	// Second, try to load from .env file
	if envData, err := os.ReadFile(".env"); err == nil {
		lines := strings.Split(string(envData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Skip comments and empty lines
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Look for LOOM_PASSWORD=value
			if strings.HasPrefix(line, "LOOM_PASSWORD=") {
				pwd := strings.TrimPrefix(line, "LOOM_PASSWORD=")
				pwd = strings.Trim(pwd, "\"'")
				return pwd
			}
		}
	}

	return ""
}

func printHelp() {
	fmt.Println("Usage: loom [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -config   Path to configuration file (default: config.yaml)")
	fmt.Println("  -version  Show version information")
	fmt.Println("  -help     Show help message")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  LOOM_PASSWORD  Master password for UI login and key encryption")
}
