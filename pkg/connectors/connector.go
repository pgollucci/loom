// Package connectors provides a plugin-like architecture for external service integrations.
// Inspired by OpenCode's tool abstraction and OpenClaw's configuration-driven design.
package connectors

import (
	"context"
	"fmt"
	"time"
)

// ConnectorType represents the type of connector
type ConnectorType string

const (
	ConnectorTypeObservability ConnectorType = "observability" // Prometheus, Grafana, Jaeger
	ConnectorTypeAgent         ConnectorType = "agent"         // OpenClaw
	ConnectorTypeStorage       ConnectorType = "storage"       // S3, MinIO, etc.
	ConnectorTypeMessaging     ConnectorType = "messaging"     // Slack, Discord, etc.
	ConnectorTypeDatabase      ConnectorType = "database"      // External databases
	ConnectorTypeCustom        ConnectorType = "custom"        // User-defined
)

// ConnectionMode indicates whether the connector is local (same network) or remote
type ConnectionMode string

const (
	ConnectionModeLocal  ConnectionMode = "local"  // localhost or docker network
	ConnectionModeRemote ConnectionMode = "remote" // External network
)

// ConnectorStatus represents the health status of a connector
type ConnectorStatus string

const (
	ConnectorStatusHealthy     ConnectorStatus = "healthy"
	ConnectorStatusUnhealthy   ConnectorStatus = "unhealthy"
	ConnectorStatusUnavailable ConnectorStatus = "unavailable"
	ConnectorStatusUnknown     ConnectorStatus = "unknown"
)

// Connector is the core interface that all connectors must implement.
// This provides location transparency - the implementation details of whether
// a service is local or remote are abstracted away.
type Connector interface {
	// ID returns the unique identifier for this connector
	ID() string

	// Name returns the display name
	Name() string

	// Type returns the connector type
	Type() ConnectorType

	// Description returns a human-readable description
	Description() string

	// Initialize sets up the connector with configuration
	Initialize(ctx context.Context, config Config) error

	// HealthCheck verifies the connector is reachable and functional
	HealthCheck(ctx context.Context) (ConnectorStatus, error)

	// GetEndpoint returns the base URL/address for this connector
	GetEndpoint() string

	// GetConfig returns the current configuration
	GetConfig() Config

	// Close cleans up resources
	Close() error
}

// Config holds the configuration for a connector
type Config struct {
	ID          string         `json:"id" yaml:"id"`
	Name        string         `json:"name" yaml:"name"`
	Type        ConnectorType  `json:"type" yaml:"type"`
	Mode        ConnectionMode `json:"mode" yaml:"mode"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`

	// Connection details
	Host     string `json:"host" yaml:"host"`                         // e.g., "localhost", "prometheus.example.com"
	Port     int    `json:"port" yaml:"port"`                         // e.g., 9090, 3000
	Scheme   string `json:"scheme,omitempty" yaml:"scheme,omitempty"` // http, https, grpc
	BasePath string `json:"base_path,omitempty" yaml:"base_path,omitempty"`

	// Authentication
	Auth *AuthConfig `json:"auth,omitempty" yaml:"auth,omitempty"`

	// Additional metadata
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Tags     []string          `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Health check configuration
	HealthCheck *HealthCheckConfig `json:"health_check,omitempty" yaml:"health_check,omitempty"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Type     string            `json:"type" yaml:"type"` // none, basic, bearer, api_key, oauth2
	Username string            `json:"username,omitempty" yaml:"username,omitempty"`
	Password string            `json:"password,omitempty" yaml:"password,omitempty"`
	Token    string            `json:"token,omitempty" yaml:"token,omitempty"`
	APIKey   string            `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	Headers  map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// HealthCheckConfig configures health check behavior
type HealthCheckConfig struct {
	Enabled  bool          `json:"enabled" yaml:"enabled"`
	Interval time.Duration `json:"interval" yaml:"interval"`             // How often to check
	Timeout  time.Duration `json:"timeout" yaml:"timeout"`               // Request timeout
	Path     string        `json:"path,omitempty" yaml:"path,omitempty"` // Health check endpoint path
}

// GetFullURL returns the complete URL for this connector
func (c *Config) GetFullURL() string {
	scheme := c.Scheme
	if scheme == "" {
		scheme = "http"
	}

	url := fmt.Sprintf("%s://%s:%d", scheme, c.Host, c.Port)
	if c.BasePath != "" {
		url += c.BasePath
	}
	return url
}

// Registry manages all registered connectors
type Registry struct {
	connectors map[string]Connector
	configs    map[string]Config
}

// NewRegistry creates a new connector registry
func NewRegistry() *Registry {
	return &Registry{
		connectors: make(map[string]Connector),
		configs:    make(map[string]Config),
	}
}

// Register adds a connector to the registry
func (r *Registry) Register(connector Connector) error {
	if connector == nil {
		return fmt.Errorf("cannot register nil connector")
	}

	id := connector.ID()
	if id == "" {
		return fmt.Errorf("connector ID cannot be empty")
	}

	if _, exists := r.connectors[id]; exists {
		return fmt.Errorf("connector with ID %s already registered", id)
	}

	r.connectors[id] = connector
	r.configs[id] = connector.GetConfig()
	return nil
}

// Get retrieves a connector by ID
func (r *Registry) Get(id string) (Connector, error) {
	connector, exists := r.connectors[id]
	if !exists {
		return nil, fmt.Errorf("connector %s not found", id)
	}
	return connector, nil
}

// List returns all registered connectors
func (r *Registry) List() []Connector {
	connectors := make([]Connector, 0, len(r.connectors))
	for _, c := range r.connectors {
		connectors = append(connectors, c)
	}
	return connectors
}

// ListByType returns connectors of a specific type
func (r *Registry) ListByType(connectorType ConnectorType) []Connector {
	var connectors []Connector
	for _, c := range r.connectors {
		if c.Type() == connectorType {
			connectors = append(connectors, c)
		}
	}
	return connectors
}

// Remove unregisters a connector
func (r *Registry) Remove(id string) error {
	connector, exists := r.connectors[id]
	if !exists {
		return fmt.Errorf("connector %s not found", id)
	}

	// Close the connector
	if err := connector.Close(); err != nil {
		return fmt.Errorf("failed to close connector %s: %w", id, err)
	}

	delete(r.connectors, id)
	delete(r.configs, id)
	return nil
}

// HealthCheckAll checks health of all enabled connectors
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]ConnectorStatus {
	results := make(map[string]ConnectorStatus)
	for id, connector := range r.connectors {
		config := r.configs[id]
		if !config.Enabled {
			results[id] = ConnectorStatusUnavailable
			continue
		}

		status, err := connector.HealthCheck(ctx)
		if err != nil {
			results[id] = ConnectorStatusUnhealthy
		} else {
			results[id] = status
		}
	}
	return results
}
