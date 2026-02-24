package provider

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type ProviderConfig struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Type                   string    `json:"type"`
	Endpoint               string    `json:"endpoint"`
	APIKey                 string    `json:"api_key,omitempty"`
	Model                  string    `json:"model"`
	ConfiguredModel        string    `json:"configured_model,omitempty"`
	SelectedModel          string    `json:"selected_model,omitempty"`
	Status                 string    `json:"status,omitempty"`
	LastHeartbeatAt        time.Time `json:"last_heartbeat_at,omitempty"`
	LastHeartbeatLatencyMs int64     `json:"last_heartbeat_latency_ms,omitempty"`
	ContextWindow          int       `json:"context_window,omitempty"`
	TotalRequests          int64     `json:"total_requests,omitempty"`
	SuccessRequests        int64     `json:"success_requests,omitempty"`
}

type MetricsCallback func(providerID string, success bool, latencyMs int64, totalTokens int64)

type Registry struct {
	mu              sync.RWMutex
	providers       map[string]*RegisteredProvider
	metricsCallback MetricsCallback
}

type RegisteredProvider struct {
	Config   *ProviderConfig
	Protocol Protocol
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]*RegisteredProvider),
	}
}

func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = make(map[string]*RegisteredProvider)
}

func (r *Registry) Register(config *ProviderConfig) error {
	// Always start with pending status - health check will promote to healthy
	config.Status = "pending"

	protocol := createProtocol(config)
	if protocol == nil {
		return fmt.Errorf("unsupported provider type: %s", config.Type)
	}

	// Run immediate health check before accepting the provider
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := protocol.GetModels(ctx)
	if err != nil {
		log.Printf("[Registry] Health check failed for provider %s: %v", config.ID, err)
		// Still register but keep status as pending
	} else {
		// Health check passed - promote to healthy
		config.Status = "healthy"
		config.LastHeartbeatAt = time.Now()
		log.Printf("[Registry] Provider %s passed health check, status: healthy", config.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[config.ID]; exists {
		return fmt.Errorf("provider %s already registered", config.ID)
	}

	r.providers[config.ID] = &RegisteredProvider{
		Config:   config,
		Protocol: protocol,
	}
	return nil
}

func (r *Registry) Upsert(config *ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if config.Status == "" {
		config.Status = "pending"
	}

	protocol := createProtocol(config)
	if protocol == nil {
		return fmt.Errorf("unsupported provider type: %s", config.Type)
	}

	// Update existing RegisteredProvider in-place so that workers holding
	// a pointer to it see the new Config/Protocol immediately.  Replacing
	// the struct would leave stale pointers in long-lived workers (bd-105).
	if existing, ok := r.providers[config.ID]; ok {
		existing.Config = config
		existing.Protocol = protocol
		return nil
	}

	r.providers[config.ID] = &RegisteredProvider{Config: config, Protocol: protocol}
	return nil
}

func createProtocol(config *ProviderConfig) Protocol {
	switch config.Type {
	case "openai", "anthropic", "local", "custom", "vllm", "ollama", "tokenhub":
		if config.APIKey == "" {
			log.Printf("[Registry] Warning: API key is missing for provider %s", config.ID)
		}
		return NewOpenAIProvider(config.Endpoint, config.APIKey)
	case "mock":
		return NewMockProvider()
	default:
		return nil
	}
}

func (r *Registry) Unregister(providerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[providerID]; !exists {
		return fmt.Errorf("provider %s not found", providerID)
	}

	delete(r.providers, providerID)
	return nil
}

func (r *Registry) Get(providerID string) (*RegisteredProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[providerID]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", providerID)
	}
	return provider, nil
}

func (r *Registry) List() []*RegisteredProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]*RegisteredProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	return providers
}

func (r *Registry) ListActive() []*RegisteredProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]*RegisteredProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		if provider != nil && provider.Config != nil && provider.Config.Status == "healthy" {
			providers = append(providers, provider)
		}
	}
	return providers
}

func (r *Registry) IsActive(providerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[providerID]
	if !exists || provider == nil || provider.Config == nil {
		return false
	}
	return provider.Config.Status == "healthy"
}

func (r *Registry) SetMetricsCallback(callback MetricsCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metricsCallback = callback
}

func (r *Registry) SendChatCompletionStream(ctx context.Context, providerID string, req *ChatCompletionRequest, handler StreamHandler) error {
	start := time.Now()

	registered, err := r.Get(providerID)
	if err != nil {
		return err
	}

	streamProvider, ok := registered.Protocol.(StreamingProtocol)
	if !ok {
		return fmt.Errorf("provider %s does not support streaming", providerID)
	}

	err = streamProvider.CreateChatCompletionStream(ctx, req, handler)

	latencyMs := time.Since(start).Milliseconds()
	if r.metricsCallback != nil {
		r.metricsCallback(providerID, err == nil, latencyMs, 0)
	}

	return err
}

func (r *Registry) SendChatCompletion(ctx context.Context, providerID string, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	startTime := time.Now()

	provider, err := r.Get(providerID)
	if err != nil {
		return nil, err
	}
	if provider.Config != nil && provider.Config.Status != "healthy" {
		return nil, fmt.Errorf("provider %s is disabled", providerID)
	}

	if req.Model == "" {
		req.Model = provider.Config.Model
	}

	resp, err := provider.Protocol.CreateChatCompletion(ctx, req)

	// If model not found (404), rediscover and retry once.
	if err != nil && (strings.Contains(err.Error(), "status code 404") || strings.Contains(err.Error(), "not found")) {
		log.Printf("[Registry] Model %q returned 404 on provider %s — rediscovering models", req.Model, providerID)
		models, modelErr := provider.Protocol.GetModels(ctx)
		if modelErr == nil && len(models) > 0 {
			newModel := models[0].ID
			log.Printf("[Registry] Provider %s model changed: %q → %q", providerID, req.Model, newModel)
			r.mu.Lock()
			if p, ok := r.providers[providerID]; ok && p.Config != nil {
				p.Config.Model = newModel
				p.Config.SelectedModel = newModel
			}
			r.mu.Unlock()
			req.Model = newModel
			resp, err = provider.Protocol.CreateChatCompletion(ctx, req)
		}
	}

	latencyMs := time.Since(startTime).Milliseconds()
	success := err == nil
	totalTokens := int64(0)
	if resp != nil {
		totalTokens = int64(resp.Usage.TotalTokens)
	}

	r.RecordRequestMetrics(providerID, latencyMs, success)

	r.mu.RLock()
	callback := r.metricsCallback
	r.mu.RUnlock()

	if callback != nil {
		callback(providerID, success, latencyMs, totalTokens)
	}

	return resp, err
}

func (r *Registry) GetModels(ctx context.Context, providerID string) ([]Model, error) {
	provider, err := r.Get(providerID)
	if err != nil {
		return nil, err
	}
	return provider.Protocol.GetModels(ctx)
}

func (r *Registry) RecordRequestMetrics(providerID string, latencyMs int64, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	provider, exists := r.providers[providerID]
	if !exists || provider == nil || provider.Config == nil {
		return
	}

	cfg := provider.Config
	cfg.TotalRequests++
	if success {
		cfg.SuccessRequests++
	}
}

func (r *Registry) UpdateHeartbeatLatency(providerID string, latencyMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	provider, exists := r.providers[providerID]
	if !exists || provider == nil || provider.Config == nil {
		return
	}

	provider.Config.LastHeartbeatLatencyMs = latencyMs
	provider.Config.LastHeartbeatAt = time.Now()
}

func isProviderHealthy(status string) bool {
	return status == "healthy"
}
