package analytics

import (
	"testing"
	"time"
)

func TestBuildBatchingRecommendations(t *testing.T) {
	baseTime := time.Date(2026, 1, 27, 12, 0, 0, 0, time.UTC)
	logs := []*RequestLog{
		{
			ID:          "log-1",
			Timestamp:   baseTime,
			UserID:      "user-1",
			Method:      "POST",
			Path:        "/api/v1/chat/completions",
			ProviderID:  "provider-a",
			ModelName:   "model-x",
			TotalTokens: 1200,
			LatencyMs:   300,
			StatusCode:  200,
			CostUSD:     0.12,
		},
		{
			ID:          "log-2",
			Timestamp:   baseTime.Add(1 * time.Minute),
			UserID:      "user-1",
			Method:      "POST",
			Path:        "/api/v1/chat/completions",
			ProviderID:  "provider-a",
			ModelName:   "model-x",
			TotalTokens: 900,
			LatencyMs:   280,
			StatusCode:  200,
			CostUSD:     0.09,
		},
		{
			ID:          "log-3",
			Timestamp:   baseTime.Add(2 * time.Minute),
			UserID:      "user-1",
			Method:      "POST",
			Path:        "/api/v1/chat/completions",
			ProviderID:  "provider-a",
			ModelName:   "model-x",
			TotalTokens: 1100,
			LatencyMs:   320,
			StatusCode:  200,
			CostUSD:     0.11,
		},
		{
			ID:          "log-4",
			Timestamp:   baseTime.Add(3 * time.Minute),
			UserID:      "user-1",
			Method:      "POST",
			Path:        "/api/v1/chat/completions",
			ProviderID:  "provider-a",
			ModelName:   "model-x",
			TotalTokens: 1000,
			LatencyMs:   310,
			StatusCode:  200,
			CostUSD:     0.1,
		},
	}

	options := DefaultBatchingOptions()
	options.Window = 10 * time.Minute
	options.MaxRecommendations = 5
	options.IncludeAutoBatchPlan = true

	result := BuildBatchingRecommendations(logs, options)
	if result == nil {
		t.Fatalf("expected batching recommendations, got nil")
	}

	if len(result.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(result.Recommendations))
	}

	rec := result.Recommendations[0]
	if rec.RequestCount != 4 {
		t.Errorf("expected request count 4, got %d", rec.RequestCount)
	}
	if rec.BatchSize < options.MinBatchSize {
		t.Errorf("expected batch size >= %d, got %d", options.MinBatchSize, rec.BatchSize)
	}
	if rec.EstimatedTokensSaved <= 0 {
		t.Errorf("expected tokens saved > 0, got %d", rec.EstimatedTokensSaved)
	}
	if result.Summary.BatchableRequests != 4 {
		t.Errorf("expected batchable requests 4, got %d", result.Summary.BatchableRequests)
	}
	if result.Summary.EstimatedCostSavingsUSD <= 0 {
		t.Errorf("expected cost savings > 0, got %.4f", result.Summary.EstimatedCostSavingsUSD)
	}
	if result.Summary.AutoBatchGroups == 0 {
		t.Errorf("expected auto batch groups > 0")
	}
}

func TestBuildBatchingRecommendations_SkipsSparseLogs(t *testing.T) {
	baseTime := time.Date(2026, 1, 27, 12, 0, 0, 0, time.UTC)
	logs := []*RequestLog{
		{
			ID:          "log-1",
			Timestamp:   baseTime,
			UserID:      "user-1",
			Method:      "POST",
			Path:        "/api/v1/chat/completions",
			ProviderID:  "provider-a",
			ModelName:   "model-x",
			TotalTokens: 1200,
			LatencyMs:   300,
			StatusCode:  200,
			CostUSD:     0.12,
		},
		{
			ID:          "log-2",
			Timestamp:   baseTime.Add(30 * time.Minute),
			UserID:      "user-1",
			Method:      "POST",
			Path:        "/api/v1/chat/completions",
			ProviderID:  "provider-a",
			ModelName:   "model-x",
			TotalTokens: 1200,
			LatencyMs:   300,
			StatusCode:  200,
			CostUSD:     0.12,
		},
	}

	options := DefaultBatchingOptions()
	options.Window = 5 * time.Minute
	options.IncludeAutoBatchPlan = true

	result := BuildBatchingRecommendations(logs, options)
	if len(result.Recommendations) != 0 {
		t.Errorf("expected no recommendations, got %d", len(result.Recommendations))
	}
	if result.Summary.BatchableRequests != 0 {
		t.Errorf("expected batchable requests 0, got %d", result.Summary.BatchableRequests)
	}
}
