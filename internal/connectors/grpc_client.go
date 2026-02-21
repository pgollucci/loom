package connectors

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/jordanhubbard/loom/api/proto/connectors"
	pkgconnectors "github.com/jordanhubbard/loom/pkg/connectors"
)

// GRPCClient provides access to the remote ConnectorsService.
// It implements the same high-level operations as the local Manager,
// allowing the control plane to swap between local and remote seamlessly.
type GRPCClient struct {
	conn   *grpc.ClientConn
	client pb.ConnectorsServiceClient
}

// NewGRPCClient connects to the connectors gRPC service at the given address.
func NewGRPCClient(addr string) (*GRPCClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr, //nolint:staticcheck // TODO: migrate to grpc.NewClient
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), //nolint:staticcheck // TODO: migrate to grpc.NewClient
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to connectors service at %s: %w", addr, err)
	}

	log.Printf("[ConnectorsClient] Connected to connectors service at %s", addr)
	return &GRPCClient{
		conn:   conn,
		client: pb.NewConnectorsServiceClient(conn),
	}, nil
}

// Close shuts down the gRPC connection.
func (c *GRPCClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ListConnectors returns all connectors from the remote service.
func (c *GRPCClient) ListConnectors(ctx context.Context) ([]*pb.ConnectorInfo, error) {
	resp, err := c.client.ListConnectors(ctx, &pb.ListConnectorsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Connectors, nil
}

// ListConnectorsByType returns connectors filtered by type.
func (c *GRPCClient) ListConnectorsByType(ctx context.Context, cType pkgconnectors.ConnectorType) ([]*pb.ConnectorInfo, error) {
	pt := goTypeToProto(cType)
	resp, err := c.client.ListConnectors(ctx, &pb.ListConnectorsRequest{Type: &pt})
	if err != nil {
		return nil, err
	}
	return resp.Connectors, nil
}

// GetConnector retrieves a specific connector.
func (c *GRPCClient) GetConnector(ctx context.Context, id string) (*pb.ConnectorInfo, *pb.ConnectorConfig, error) {
	resp, err := c.client.GetConnector(ctx, &pb.GetConnectorRequest{Id: id})
	if err != nil {
		return nil, nil, err
	}
	return resp.Connector, resp.Config, nil
}

// HealthCheck checks a single connector's health.
func (c *GRPCClient) HealthCheck(ctx context.Context, id string) (*pb.HealthCheckResponse, error) {
	return c.client.HealthCheck(ctx, &pb.HealthCheckRequest{Id: id})
}

// HealthCheckAll checks all connectors.
func (c *GRPCClient) HealthCheckAll(ctx context.Context) (map[string]pb.ConnectorStatus, error) {
	resp, err := c.client.HealthCheckAll(ctx, &pb.HealthCheckAllRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Statuses, nil
}

// RegisterConnector adds a new connector to the remote service.
func (c *GRPCClient) RegisterConnector(ctx context.Context, cfg *pb.ConnectorConfig) (string, error) {
	resp, err := c.client.RegisterConnector(ctx, &pb.RegisterConnectorRequest{Config: cfg})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("register failed: %s", resp.Message)
	}
	return resp.ConnectorId, nil
}

// RemoveConnector deletes a connector from the remote service.
func (c *GRPCClient) RemoveConnector(ctx context.Context, id string) error {
	resp, err := c.client.RemoveConnector(ctx, &pb.RemoveConnectorRequest{Id: id})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("remove failed: %s", resp.Message)
	}
	return nil
}
