package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// OpenClawConnector implements connector for OpenClaw agent system
// Supports both API-level integration (remote) and command-level (local)
type OpenClawConnector struct {
	config Config
	client *http.Client
}

func NewOpenClawConnector(config Config) *OpenClawConnector {
	return &OpenClawConnector{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (o *OpenClawConnector) ID() string          { return o.config.ID }
func (o *OpenClawConnector) Name() string        { return o.config.Name }
func (o *OpenClawConnector) Type() ConnectorType { return ConnectorTypeAgent }
func (o *OpenClawConnector) Description() string { return o.config.Description }
func (o *OpenClawConnector) GetEndpoint() string { return o.config.GetFullURL() }
func (o *OpenClawConnector) GetConfig() Config   { return o.config }

func (o *OpenClawConnector) Initialize(ctx context.Context, config Config) error {
	o.config = config

	// Defaults for local OpenClaw installation
	if config.Mode == ConnectionModeLocal {
		if config.Host == "" {
			o.config.Host = "localhost"
		}
		if config.Port == 0 {
			o.config.Port = 3721 // Default OpenClaw gateway port
		}
	}

	if config.Scheme == "" {
		o.config.Scheme = "http"
	}

	return nil
}

func (o *OpenClawConnector) HealthCheck(ctx context.Context) (ConnectorStatus, error) {
	// Try API health check first (remote mode)
	if o.config.Mode == ConnectionModeRemote {
		return o.healthCheckAPI(ctx)
	}

	// Local mode: check both API and command availability
	apiStatus, apiErr := o.healthCheckAPI(ctx)
	if apiStatus == ConnectorStatusHealthy {
		return apiStatus, nil
	}

	// Fallback to command check
	cmdStatus, cmdErr := o.healthCheckCommand(ctx)
	if cmdStatus == ConnectorStatusHealthy {
		return cmdStatus, nil
	}

	// Both failed
	return ConnectorStatusUnhealthy, fmt.Errorf("api: %v, command: %v", apiErr, cmdErr)
}

func (o *OpenClawConnector) healthCheckAPI(ctx context.Context) (ConnectorStatus, error) {
	// OpenClaw gateway health endpoint (adjust based on actual API)
	url := o.GetEndpoint() + "/health"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ConnectorStatusUnhealthy, err
	}

	// Add authentication if configured
	if o.config.Auth != nil && o.config.Auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+o.config.Auth.Token)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return ConnectorStatusUnhealthy, fmt.Errorf("openclaw api unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return ConnectorStatusHealthy, nil
	}

	return ConnectorStatusUnhealthy, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (o *OpenClawConnector) healthCheckCommand(ctx context.Context) (ConnectorStatus, error) {
	// Check if openclaw command is available
	cmd := exec.CommandContext(ctx, "openclaw", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ConnectorStatusUnhealthy, fmt.Errorf("openclaw command not available: %w", err)
	}

	// Verify output looks reasonable
	if !strings.Contains(strings.ToLower(string(output)), "openclaw") {
		return ConnectorStatusUnhealthy, fmt.Errorf("unexpected version output: %s", output)
	}

	return ConnectorStatusHealthy, nil
}

func (o *OpenClawConnector) Close() error {
	o.client.CloseIdleConnections()
	return nil
}

// AgentRequest represents a request to execute an OpenClaw agent
type AgentRequest struct {
	AgentID string `json:"agent_id"`
	Message string `json:"message"`
	Session string `json:"session,omitempty"`
}

// AgentResponse represents the response from OpenClaw agent execution
type AgentResponse struct {
	RunID    string       `json:"run_id"`
	Response string       `json:"response"`
	Events   []AgentEvent `json:"events,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// AgentEvent represents an event from agent execution
type AgentEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// ExecuteAgent sends a task to an OpenClaw agent
func (o *OpenClawConnector) ExecuteAgent(ctx context.Context, req AgentRequest) (*AgentResponse, error) {
	if o.config.Mode == ConnectionModeRemote {
		return o.executeAgentAPI(ctx, req)
	}

	// Try API first, fallback to command
	resp, err := o.executeAgentAPI(ctx, req)
	if err == nil {
		return resp, nil
	}

	return o.executeAgentCommand(ctx, req)
}

func (o *OpenClawConnector) executeAgentAPI(ctx context.Context, req AgentRequest) (*AgentResponse, error) {
	url := o.GetEndpoint() + "/agent"

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if o.config.Auth != nil && o.config.Auth.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.config.Auth.Token)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	var agentResp AgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &agentResp, nil
}

func (o *OpenClawConnector) executeAgentCommand(ctx context.Context, req AgentRequest) (*AgentResponse, error) {
	// Execute via openclaw CLI command
	args := []string{"agent"}
	if req.AgentID != "" {
		args = append(args, "--agent", req.AgentID)
	}
	if req.Session != "" {
		args = append(args, "--session", req.Session)
	}
	args = append(args, req.Message)

	cmd := exec.CommandContext(ctx, "openclaw", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w\noutput: %s", err, string(output))
	}

	return &AgentResponse{
		Response: string(output),
	}, nil
}

// ListAgents retrieves available agents from OpenClaw
func (o *OpenClawConnector) ListAgents(ctx context.Context) ([]AgentInfo, error) {
	if o.config.Mode == ConnectionModeRemote {
		return o.listAgentsAPI(ctx)
	}

	// Try API first, fallback to command
	agents, err := o.listAgentsAPI(ctx)
	if err == nil {
		return agents, nil
	}

	return o.listAgentsCommand(ctx)
}

type AgentInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Workspace   string `json:"workspace"`
}

func (o *OpenClawConnector) listAgentsAPI(ctx context.Context) ([]AgentInfo, error) {
	url := o.GetEndpoint() + "/agents"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if o.config.Auth != nil && o.config.Auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+o.config.Auth.Token)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %d", resp.StatusCode)
	}

	var agents []AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, err
	}

	return agents, nil
}

func (o *OpenClawConnector) listAgentsCommand(ctx context.Context) ([]AgentInfo, error) {
	cmd := exec.CommandContext(ctx, "openclaw", "agents", "list", "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w", err)
	}

	var agents []AgentInfo
	if err := json.Unmarshal(output, &agents); err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	return agents, nil
}
