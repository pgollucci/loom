package connectors

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PrometheusConnector implements connector for Prometheus
type PrometheusConnector struct {
	config Config
	client *http.Client
}

// NewPrometheusConnector creates a new Prometheus connector
func NewPrometheusConnector(config Config) *PrometheusConnector {
	return &PrometheusConnector{
		config: config,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *PrometheusConnector) ID() string          { return p.config.ID }
func (p *PrometheusConnector) Name() string        { return p.config.Name }
func (p *PrometheusConnector) Type() ConnectorType { return ConnectorTypeObservability }
func (p *PrometheusConnector) Description() string { return p.config.Description }
func (p *PrometheusConnector) GetEndpoint() string { return p.config.GetFullURL() }
func (p *PrometheusConnector) GetConfig() Config   { return p.config }

func (p *PrometheusConnector) Initialize(ctx context.Context, config Config) error {
	p.config = config
	if config.Host == "" {
		p.config.Host = "localhost"
	}
	if config.Port == 0 {
		p.config.Port = 9090
	}
	if config.Scheme == "" {
		p.config.Scheme = "http"
	}
	return nil
}

func (p *PrometheusConnector) HealthCheck(ctx context.Context) (ConnectorStatus, error) {
	healthPath := "/-/healthy"
	if p.config.HealthCheck != nil && p.config.HealthCheck.Path != "" {
		healthPath = p.config.HealthCheck.Path
	}

	url := p.GetEndpoint() + healthPath
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ConnectorStatusUnhealthy, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return ConnectorStatusUnhealthy, fmt.Errorf("prometheus unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return ConnectorStatusHealthy, nil
	}

	return ConnectorStatusUnhealthy, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (p *PrometheusConnector) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

// Query executes a PromQL query
func (p *PrometheusConnector) Query(ctx context.Context, query string) ([]byte, error) {
	url := fmt.Sprintf("%s/api/v1/query?query=%s", p.GetEndpoint(), query)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// GrafanaConnector implements connector for Grafana
type GrafanaConnector struct {
	config Config
	client *http.Client
}

func NewGrafanaConnector(config Config) *GrafanaConnector {
	return &GrafanaConnector{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GrafanaConnector) ID() string          { return g.config.ID }
func (g *GrafanaConnector) Name() string        { return g.config.Name }
func (g *GrafanaConnector) Type() ConnectorType { return ConnectorTypeObservability }
func (g *GrafanaConnector) Description() string { return g.config.Description }
func (g *GrafanaConnector) GetEndpoint() string { return g.config.GetFullURL() }
func (g *GrafanaConnector) GetConfig() Config   { return g.config }

func (g *GrafanaConnector) Initialize(ctx context.Context, config Config) error {
	g.config = config
	if config.Host == "" {
		g.config.Host = "localhost"
	}
	if config.Port == 0 {
		g.config.Port = 3000
	}
	if config.Scheme == "" {
		g.config.Scheme = "http"
	}
	return nil
}

func (g *GrafanaConnector) HealthCheck(ctx context.Context) (ConnectorStatus, error) {
	healthPath := "/api/health"
	if g.config.HealthCheck != nil && g.config.HealthCheck.Path != "" {
		healthPath = g.config.HealthCheck.Path
	}

	url := g.GetEndpoint() + healthPath
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ConnectorStatusUnhealthy, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return ConnectorStatusUnhealthy, fmt.Errorf("grafana unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return ConnectorStatusHealthy, nil
	}

	return ConnectorStatusUnhealthy, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (g *GrafanaConnector) Close() error {
	g.client.CloseIdleConnections()
	return nil
}

// JaegerConnector implements connector for Jaeger
type JaegerConnector struct {
	config Config
	client *http.Client
}

func NewJaegerConnector(config Config) *JaegerConnector {
	return &JaegerConnector{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (j *JaegerConnector) ID() string          { return j.config.ID }
func (j *JaegerConnector) Name() string        { return j.config.Name }
func (j *JaegerConnector) Type() ConnectorType { return ConnectorTypeObservability }
func (j *JaegerConnector) Description() string { return j.config.Description }
func (j *JaegerConnector) GetEndpoint() string { return j.config.GetFullURL() }
func (j *JaegerConnector) GetConfig() Config   { return j.config }

func (j *JaegerConnector) Initialize(ctx context.Context, config Config) error {
	j.config = config
	if config.Host == "" {
		j.config.Host = "localhost"
	}
	if config.Port == 0 {
		j.config.Port = 16686
	}
	if config.Scheme == "" {
		j.config.Scheme = "http"
	}
	return nil
}

func (j *JaegerConnector) HealthCheck(ctx context.Context) (ConnectorStatus, error) {
	// Jaeger UI doesn't have a standard health endpoint, try accessing the main page
	url := j.GetEndpoint()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ConnectorStatusUnhealthy, err
	}

	resp, err := j.client.Do(req)
	if err != nil {
		return ConnectorStatusUnhealthy, fmt.Errorf("jaeger unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return ConnectorStatusHealthy, nil
	}

	return ConnectorStatusUnhealthy, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (j *JaegerConnector) Close() error {
	j.client.CloseIdleConnections()
	return nil
}
