package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jordanhubbard/loom/internal/projectagent"
)

func main() {
	var (
		projectID        = flag.String("project-id", os.Getenv("PROJECT_ID"), "Project ID")
		controlPlaneURL  = flag.String("control-plane-url", os.Getenv("CONTROL_PLANE_URL"), "Control plane URL")
		port             = flag.String("port", getEnvOrDefault("PORT", "8090"), "HTTP port for agent API")
		workDir          = flag.String("work-dir", getEnvOrDefault("WORK_DIR", "/workspace"), "Project workspace directory")
		heartbeatInterval = flag.Duration("heartbeat", 30*time.Second, "Heartbeat interval")
	)

	flag.Parse()

	if *projectID == "" {
		log.Fatal("PROJECT_ID is required")
	}

	if *controlPlaneURL == "" {
		log.Fatal("CONTROL_PLANE_URL is required")
	}

	log.Printf("Starting Loom Project Agent")
	log.Printf("  Project ID: %s", *projectID)
	log.Printf("  Control Plane: %s", *controlPlaneURL)
	log.Printf("  Work Directory: %s", *workDir)
	log.Printf("  Listen Port: %s", *port)

	// Create project agent
	agent, err := projectagent.New(projectagent.Config{
		ProjectID:         *projectID,
		ControlPlaneURL:   *controlPlaneURL,
		WorkDir:           *workDir,
		HeartbeatInterval: *heartbeatInterval,
	})
	if err != nil {
		log.Fatalf("Failed to create project agent: %v", err)
	}

	// Start HTTP server for task reception
	mux := http.NewServeMux()
	agent.RegisterHandlers(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", *port),
		Handler: mux,
	}

	// Start agent background tasks (heartbeat, etc.)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := agent.Start(ctx); err != nil {
			log.Printf("Agent background tasks error: %v", err)
		}
	}()

	// Start HTTP server
	go func() {
		log.Printf("Project agent listening on :%s", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down project agent...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Project agent stopped")
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
