package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jordanhubbard/loom/pkg/config"
	"github.com/jordanhubbard/loom/pkg/server"
)

func main() {
	fmt.Println("Welcome to Loom - AI Coding Agent Orchestrator")
	fmt.Println("==================================================")

	// Load configuration from config.yaml if it exists, otherwise use defaults
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.LoadConfigFromFile(configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s: %v", configPath, err)
		log.Printf("Using default configuration")
		cfg = config.DefaultConfig()
	} else {
		log.Printf("Loaded configuration from %s", configPath)
	}

	fmt.Println("\nLoom Worker System initialized")
	fmt.Println("See docs/WORKER_SYSTEM.md for usage information")

	// Start the server
	fmt.Println("\nStarting Loom server...")
	srv := server.NewServer(cfg)
	if err := srv.Start(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
