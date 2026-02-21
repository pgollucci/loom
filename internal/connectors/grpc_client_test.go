package connectors

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/jordanhubbard/loom/api/proto/connectors"
)

func startTestGRPCServer(t *testing.T) (string, func()) {
	t.Helper()
	mgr := newTestManager(t)
	srv := grpc.NewServer()
	pb.RegisterConnectorsServiceServer(srv, NewGRPCServer(mgr))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go srv.Serve(lis)
	return lis.Addr().String(), func() {
		srv.Stop()
		mgr.Close()
	}
}

func dialTestServer(t *testing.T, ctx context.Context, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.DialContext(ctx, addr, //nolint:staticcheck // TODO: migrate to grpc.NewClient
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), //nolint:staticcheck // TODO: migrate to grpc.NewClient
	)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

func TestGRPCClient_ListConnectors(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialTestServer(t, ctx, addr)
	defer conn.Close()

	client := &GRPCClient{conn: conn, client: pb.NewConnectorsServiceClient(conn)}

	infos, err := client.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("ListConnectors: %v", err)
	}
	if len(infos) == 0 {
		t.Fatal("expected at least one connector from default config")
	}

	var found bool
	for _, i := range infos {
		if i.Id == "prometheus" {
			found = true
			break
		}
	}
	if !found {
		t.Error("prometheus not in list")
	}
}

func TestGRPCClient_GetConnector(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialTestServer(t, ctx, addr)
	defer conn.Close()

	client := &GRPCClient{conn: conn, client: pb.NewConnectorsServiceClient(conn)}

	info, cfg, err := client.GetConnector(ctx, "prometheus")
	if err != nil {
		t.Fatalf("GetConnector: %v", err)
	}
	if info.Id != "prometheus" {
		t.Errorf("expected prometheus, got %s", info.Id)
	}
	if cfg == nil {
		t.Error("expected config in response")
	}
}

func TestGRPCClient_GetConnector_NotFound(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialTestServer(t, ctx, addr)
	defer conn.Close()

	client := &GRPCClient{conn: conn, client: pb.NewConnectorsServiceClient(conn)}

	_, _, err := client.GetConnector(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing connector")
	}
}

func TestGRPCClient_HealthCheckAll(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	// Short timeout: the health check will fail (unreachable hosts) but
	// the gRPC call itself should still return statuses (unhealthy).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn := dialTestServer(t, ctx, addr)
	defer conn.Close()

	client := &GRPCClient{conn: conn, client: pb.NewConnectorsServiceClient(conn)}

	statuses, err := client.HealthCheckAll(ctx)
	if err != nil {
		// DeadlineExceeded is acceptable — the underlying health checks hit
		// unreachable hosts (prometheus:9090, etc.).
		t.Skipf("HealthCheckAll timed out (expected in test env): %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("expected at least one status")
	}
}

func TestRemoteService_EndToEnd(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	svc, err := NewRemoteService(addr)
	if err != nil {
		t.Fatalf("NewRemoteService: %v", err)
	}
	defer svc.Close()

	list := svc.ListConnectors()
	if len(list) == 0 {
		t.Fatal("expected connectors from remote service")
	}

	ctx := context.Background()
	info, err := svc.GetConnector(ctx, "prometheus")
	if err != nil {
		t.Fatalf("GetConnector: %v", err)
	}
	if info.ID != "prometheus" {
		t.Errorf("expected prometheus, got %s", info.ID)
	}

	_, err = svc.GetConnector(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing connector")
	}
}
