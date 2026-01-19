package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/jordanhubbard/arbiter/internal/database"
	internalmodels "github.com/jordanhubbard/arbiter/internal/models"
	"github.com/jordanhubbard/arbiter/internal/provider"
	"github.com/jordanhubbard/arbiter/internal/temporal/eventbus"
)

// ProviderHeartbeatInput represents heartbeat activity input.
type ProviderHeartbeatInput struct {
	ProviderID string
}

// ProviderHeartbeatResult captures heartbeat measurements.
type ProviderHeartbeatResult struct {
	ProviderID string    `json:"provider_id"`
	Status     string    `json:"status"`
	LatencyMs  int64     `json:"latency_ms"`
	Error      string    `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

// ProviderQueryInput represents a direct provider query.
type ProviderQueryInput struct {
	ProviderID   string  `json:"provider_id"`
	SystemPrompt string  `json:"system_prompt"`
	Message      string  `json:"message"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
}

// ProviderQueryResult represents the response from a provider query.
type ProviderQueryResult struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	Response   string `json:"response"`
	TokensUsed int    `json:"tokens_used"`
	LatencyMs  int64  `json:"latency_ms"`
}

// ProviderActivities supplies heartbeat and query activities.
type ProviderActivities struct {
	registry *provider.Registry
	database *database.Database
	eventBus *eventbus.EventBus
}

func NewProviderActivities(registry *provider.Registry, db *database.Database, eb *eventbus.EventBus) *ProviderActivities {
	return &ProviderActivities{registry: registry, database: db, eventBus: eb}
}

// ProviderHeartbeatActivity checks provider responsiveness and updates status.
func (a *ProviderActivities) ProviderHeartbeatActivity(ctx context.Context, input ProviderHeartbeatInput) (*ProviderHeartbeatResult, error) {
	if input.ProviderID == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	start := time.Now()
	result := &ProviderHeartbeatResult{ProviderID: input.ProviderID, CheckedAt: time.Now().UTC()}

	regProvider, err := a.registry.Get(input.ProviderID)
	if err != nil {
		result.Status = "disabled"
		result.Error = err.Error()
		a.persistHeartbeat(result)
		return result, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err = regProvider.Protocol.GetModels(ctx)
	result.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "disabled"
		result.Error = err.Error()
		a.persistHeartbeat(result)
		return result, nil
	}

	result.Status = "active"
	result.Error = ""
	a.persistHeartbeat(result)
	return result, nil
}

func (a *ProviderActivities) ProviderQueryActivity(ctx context.Context, input ProviderQueryInput) (*ProviderQueryResult, error) {
	if input.ProviderID == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	regProvider, err := a.registry.Get(input.ProviderID)
	if err != nil {
		return nil, err
	}
	if regProvider.Config == nil {
		return nil, fmt.Errorf("provider %s is missing config", input.ProviderID)
	}
	if regProvider.Config.Status != "active" {
		return nil, fmt.Errorf("provider %s is disabled", input.ProviderID)
	}
	if regProvider.Config.Model == "" {
		return nil, fmt.Errorf("provider %s has no model configured", input.ProviderID)
	}

	req := &provider.ChatCompletionRequest{
		Model: regProvider.Config.Model,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: input.SystemPrompt},
			{Role: "user", Content: input.Message},
		},
		Temperature: input.Temperature,
		MaxTokens:   input.MaxTokens,
	}

	start := time.Now()
	resp, err := regProvider.Protocol.CreateChatCompletion(ctx, req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return nil, err
	}

	responseText := ""
	if len(resp.Choices) > 0 {
		responseText = resp.Choices[0].Message.Content
	}

	result := &ProviderQueryResult{
		ProviderID: input.ProviderID,
		Model:      resp.Model,
		Response:   responseText,
		TokensUsed: resp.Usage.TotalTokens,
		LatencyMs:  latencyMs,
	}
	return result, nil
}

func (a *ProviderActivities) persistHeartbeat(result *ProviderHeartbeatResult) {
	if result == nil || a.database == nil {
		return
	}

	record, err := a.database.GetProvider(result.ProviderID)
	if err != nil {
		return
	}
	record.Status = result.Status
	record.LastHeartbeatAt = result.CheckedAt
	record.LastHeartbeatLatencyMs = result.LatencyMs
	record.LastHeartbeatError = result.Error
	_ = a.database.UpsertProvider(record)

	a.syncRegistry(record)

	if a.eventBus != nil {
		_ = a.eventBus.Publish(&eventbus.Event{
			Type:   eventbus.EventTypeProviderUpdated,
			Source: "provider-heartbeat",
			Data: map[string]interface{}{
				"provider_id": record.ID,
				"status":      record.Status,
				"latency_ms":  record.LastHeartbeatLatencyMs,
			},
		})
	}
}

func (a *ProviderActivities) syncRegistry(record *internalmodels.Provider) {
	if record == nil || a.registry == nil {
		return
	}
	selected := record.SelectedModel
	if selected == "" {
		selected = record.Model
	}
	if selected == "" {
		selected = record.ConfiguredModel
	}
	_ = a.registry.Upsert(&provider.ProviderConfig{
		ID:                     record.ID,
		Name:                   record.Name,
		Type:                   record.Type,
		Endpoint:               record.Endpoint,
		Model:                  selected,
		ConfiguredModel:        record.ConfiguredModel,
		SelectedModel:          selected,
		SelectedGPU:            record.SelectedGPU,
		Status:                 record.Status,
		LastHeartbeatAt:        record.LastHeartbeatAt,
		LastHeartbeatLatencyMs: record.LastHeartbeatLatencyMs,
	})
}
