package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/jordanhubbard/loom/api/proto/connectors"
	connectorssvc "github.com/jordanhubbard/loom/internal/connectors"
	"github.com/jordanhubbard/loom/internal/telemetry"
	pkgconnectors "github.com/jordanhubbard/loom/pkg/connectors"
)

func main() {
	port := envOrDefault("GRPC_PORT", "50051")
	configPath := envOrDefault("CONFIG_PATH", "/app/config/connectors.yaml")
	healthInterval := envOrDefault("HEALTH_INTERVAL", "30s")
	otelEndpoint := os.Getenv("OTEL_ENDPOINT")

	log := slog.New(slog.NewJSONHandler(os.Stdout))
log.Info("Starting ConnectorsService", slog.String("port", port), slog.String("config", configPath))

	// Optionally initialise OTel tracing
	if otelEndpoint != "" {
		shutdown, err := telemetry.InitTelemetry(context.Background(), "connectors-service", otelEndpoint)
		if err != nil {
			log.Error("OTel init failed", slog.Error(err))
		} else {
			defer shutdown(context.Background())
		}
	}

	mgr := pkgconnectors.NewManager(configPath)
	if err := mgr.LoadConfig(); err != nil {
		log.Warn("Config load failed", slog.Error(err))
	}

	interval, err := time.ParseDuration(healthInterval)
	if err != nil {
		interval = 30 * time.Second
	}
	mgr.StartHealthMonitoring(interval)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Error("Failed to listen on port", slog.String("port", port), slog.Error(err))
	}

	grpcServer := grpc.NewServer()
	pb.RegisterConnectorsServiceServer(grpcServer, connectorssvc.NewGRPCServer(mgr))

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("connectors.ConnectorsService", healthpb.HealthCheckResponse_SERVING)

	reflection.Register(grpcServer)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[ConnectorsService] Shutting down...")
		grpcServer.GracefulStop()
		mgr.Close()
	}()

	log.Info("Serving gRPC", slog.String("port", port))
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("gRPC serve error", slog.Error(err))
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
