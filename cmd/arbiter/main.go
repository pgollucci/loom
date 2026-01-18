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

	"github.com/jordanhubbard/arbiter/internal/api"
	"github.com/jordanhubbard/arbiter/internal/arbiter"
	"github.com/jordanhubbard/arbiter/pkg/config"
)

var (
	configPath = flag.String("config", "config.yaml", "Path to configuration file")
	version    = "dev"
)

func main() {
	flag.Parse()

	log.Printf("Starting Arbiter v%s", version)

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create arbiter instance
	arb := arbiter.New(cfg)

	// Initialize
	ctx := context.Background()
	if err := arb.Initialize(ctx); err != nil {
		log.Fatalf("Failed to initialize arbiter: %v", err)
	}

	log.Printf("Loaded %d projects", len(arb.GetProjectManager().ListProjects()))

	// Start maintenance loop
	maintenanceCtx, cancelMaintenance := context.WithCancel(ctx)
	go arb.StartMaintenanceLoop(maintenanceCtx)
	defer cancelMaintenance()

	// Create API server
	apiServer := api.NewServer(arb, cfg)
	handler := apiServer.SetupRoutes()

	// Start HTTP server if enabled
	var httpServer *http.Server
	if cfg.Server.EnableHTTP {
		httpServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Server.HTTPPort),
			Handler:      handler,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		}

		go func() {
			log.Printf("Starting HTTP server on port %d", cfg.Server.HTTPPort)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP server error: %v", err)
			}
		}()
	}

	// Start HTTPS server if enabled
	var httpsServer *http.Server
	if cfg.Server.EnableHTTPS {
		if cfg.Server.TLSCertFile == "" || cfg.Server.TLSKeyFile == "" {
			log.Fatal("TLS certificate and key files are required for HTTPS")
		}

		httpsServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Server.HTTPSPort),
			Handler:      handler,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		}

		go func() {
			log.Printf("Starting HTTPS server on port %d", cfg.Server.HTTPSPort)
			if err := httpsServer.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTPS server error: %v", err)
			}
		}()
	}

	if !cfg.Server.EnableHTTP && !cfg.Server.EnableHTTPS {
		log.Fatal("At least one of HTTP or HTTPS must be enabled")
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}

	if httpsServer != nil {
		if err := httpsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTPS server shutdown error: %v", err)
		}
	}

	log.Println("Arbiter stopped")
}

func loadConfig(path string) (*config.Config, error) {
	// Check if config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("Configuration file not found at %s, using defaults", path)
		return config.DefaultConfig(), nil
	}

	return config.LoadConfig(path)
}
