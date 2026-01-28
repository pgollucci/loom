package analytics

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// BatchingOptions configures batching recommendation analysis.
type BatchingOptions struct {
	Window               time.Duration
	MinBatchSize         int
	MaxBatchSize         int
	MaxRecommendations   int
	OverheadTokens       int64
	OverheadTokenRatio   float64
	OverheadLatencyMs    int64
	IncludeAutoBatchPlan bool
	MaxAutoBatchGroups   int
}

// DefaultBatchingOptions returns recommended defaults for batching analysis.
func DefaultBatchingOptions() *BatchingOptions {
	return &BatchingOptions{
		Window:               5 * time.Minute,
		MinBatchSize:         3,
		MaxBatchSize:         10,
		MaxRecommendations:   20,
		OverheadTokens:       100,
		OverheadTokenRatio:   0.05,
		OverheadLatencyMs:    50,
		IncludeAutoBatchPlan: true,
		MaxAutoBatchGroups:   50,
	}
}

// BatchingSummary provides aggregated batching insights.
type BatchingSummary struct {
	BatchableRequests          int     `json:"batchable_requests"`
	RecommendedBatches         int     `json:"recommended_batches"`
	AverageBatchSize           float64 `json:"average_batch_size"`
	EstimatedTokensSaved       int64   `json:"estimated_tokens_saved"`
	EstimatedCostSavingsUSD    float64 `json:"estimated_cost_savings_usd"`
	EstimatedLatencySavingsMs  int64   `json:"estimated_latency_savings_ms"`
	EstimatedEfficiencyGainPct float64 `json:"estimated_efficiency_gain_pct"`
	AutoBatchGroups            int     `json:"auto_batch_groups"`
	TotalRecommendations       int     `json:"total_recommendations"`
	TotalCostUSD               float64 `json:"total_cost_usd"`
	TotalTokens                int64   `json:"total_tokens"`
}

// BatchRecommendation captures a single batching opportunity.
type BatchRecommendation struct {
	ID                        string    `json:"id"`
	UserID                    string    `json:"user_id"`
	ProviderID                string    `json:"provider_id"`
	ModelName                 string    `json:"model_name"`
	Method                    string    `json:"method"`
	Path                      string    `json:"path"`
	RequestCount              int       `json:"request_count"`
	BatchSize                 int       `json:"batch_size"`
	RecommendedBatches        int       `json:"recommended_batches"`
	TotalTokens               int64     `json:"total_tokens"`
	TotalCostUSD              float64   `json:"total_cost_usd"`
	AvgLatencyMs              int64     `json:"avg_latency_ms"`
	TimeWindowStart           time.Time `json:"time_window_start"`
	TimeWindowEnd             time.Time `json:"time_window_end"`
	EstimatedTokensSaved      int64     `json:"estimated_tokens_saved"`
	EstimatedCostSavingsUSD   float64   `json:"estimated_cost_savings_usd"`
	EstimatedLatencySavingsMs int64     `json:"estimated_latency_savings_ms"`
	EfficiencyGainPercent     float64   `json:"efficiency_gain_percent"`
	SampleRequestIDs          []string  `json:"sample_request_ids"`
}

// AutoBatchGroup describes a suggested batch grouping.
type AutoBatchGroup struct {
	ID               string   `json:"id"`
	RecommendationID string   `json:"recommendation_id"`
	BatchSize        int      `json:"batch_size"`
	RequestIDs       []string `json:"request_ids"`
}

// BatchingRecommendations is the full response payload.
type BatchingRecommendations struct {
	GeneratedAt     time.Time             `json:"generated_at"`
	Summary         BatchingSummary       `json:"summary"`
	Recommendations []BatchRecommendation `json:"recommendations"`
	AutoBatchPlan   []AutoBatchGroup      `json:"auto_batch_plan"`
}

// BuildBatchingRecommendations analyzes logs to identify batching opportunities.
func BuildBatchingRecommendations(logs []*RequestLog, options *BatchingOptions) *BatchingRecommendations {
	if options == nil {
		options = DefaultBatchingOptions()
	}

	filtered := filterBatchableLogs(logs)
	if len(filtered) == 0 {
		return &BatchingRecommendations{
			GeneratedAt:     time.Now(),
			Summary:         BatchingSummary{},
			Recommendations: []BatchRecommendation{},
			AutoBatchPlan:   []AutoBatchGroup{},
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.Before(filtered[j].Timestamp)
	})

	grouped := map[string][]*RequestLog{}
	for _, log := range filtered {
		key := buildBatchKey(log)
		grouped[key] = append(grouped[key], log)
	}

	summary := BatchingSummary{}
	recommendations := []BatchRecommendation{}
	autoPlan := []AutoBatchGroup{}

	for _, groupLogs := range grouped {
		sort.Slice(groupLogs, func(i, j int) bool {
			return groupLogs[i].Timestamp.Before(groupLogs[j].Timestamp)
		})

		for idx := 0; idx < len(groupLogs); {
			windowLogs, nextIdx := sliceWindow(groupLogs, idx, options)
			if len(windowLogs) >= options.MinBatchSize {
				recommendation := buildRecommendation(windowLogs, options)
				recommendations = append(recommendations, recommendation)
				summary.BatchableRequests += recommendation.RequestCount
				summary.RecommendedBatches += recommendation.RecommendedBatches
				summary.EstimatedTokensSaved += recommendation.EstimatedTokensSaved
				summary.EstimatedCostSavingsUSD += recommendation.EstimatedCostSavingsUSD
				summary.EstimatedLatencySavingsMs += recommendation.EstimatedLatencySavingsMs
				summary.TotalCostUSD += recommendation.TotalCostUSD
				summary.TotalTokens += recommendation.TotalTokens

				if options.IncludeAutoBatchPlan {
					autoBatches := buildAutoBatchPlan(recommendation, windowLogs, options)
					autoPlan = append(autoPlan, autoBatches...)
					summary.AutoBatchGroups += len(autoBatches)
				}

				summary.TotalRecommendations++
			}

			if nextIdx <= idx {
				idx++
			} else {
				idx = nextIdx
			}

			if options.MaxRecommendations > 0 && len(recommendations) >= options.MaxRecommendations {
				break
			}
		}

		if options.MaxRecommendations > 0 && len(recommendations) >= options.MaxRecommendations {
			break
		}
	}

	if summary.RecommendedBatches > 0 {
		summary.AverageBatchSize = float64(summary.BatchableRequests) / float64(summary.RecommendedBatches)
	}
	if summary.TotalCostUSD > 0 {
		summary.EstimatedEfficiencyGainPct = (summary.EstimatedCostSavingsUSD / summary.TotalCostUSD) * 100
	}

	sort.Slice(recommendations, func(i, j int) bool {
		if recommendations[i].EstimatedCostSavingsUSD == recommendations[j].EstimatedCostSavingsUSD {
			return recommendations[i].RequestCount > recommendations[j].RequestCount
		}
		return recommendations[i].EstimatedCostSavingsUSD > recommendations[j].EstimatedCostSavingsUSD
	})

	if options.MaxRecommendations > 0 && len(recommendations) > options.MaxRecommendations {
		recommendations = recommendations[:options.MaxRecommendations]
	}

	if options.MaxAutoBatchGroups > 0 && len(autoPlan) > options.MaxAutoBatchGroups {
		autoPlan = autoPlan[:options.MaxAutoBatchGroups]
	}

	return &BatchingRecommendations{
		GeneratedAt:     time.Now(),
		Summary:         summary,
		Recommendations: recommendations,
		AutoBatchPlan:   autoPlan,
	}
}

func filterBatchableLogs(logs []*RequestLog) []*RequestLog {
	filtered := make([]*RequestLog, 0, len(logs))
	for _, log := range logs {
		if log == nil {
			continue
		}
		if log.StatusCode >= 400 {
			continue
		}
		if log.TotalTokens <= 0 {
			continue
		}
		filtered = append(filtered, log)
	}
	return filtered
}

func buildBatchKey(log *RequestLog) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", log.UserID, log.ProviderID, log.ModelName, log.Method, log.Path)
}

func sliceWindow(logs []*RequestLog, startIdx int, options *BatchingOptions) ([]*RequestLog, int) {
	if startIdx >= len(logs) {
		return nil, len(logs)
	}

	startTime := logs[startIdx].Timestamp
	windowLogs := []*RequestLog{}
	nextIdx := startIdx
	for nextIdx < len(logs) {
		if options.Window > 0 && logs[nextIdx].Timestamp.Sub(startTime) > options.Window {
			break
		}
		windowLogs = append(windowLogs, logs[nextIdx])
		nextIdx++
		if options.MaxBatchSize > 0 && len(windowLogs) >= options.MaxBatchSize {
			break
		}
	}

	return windowLogs, nextIdx
}

func buildRecommendation(logs []*RequestLog, options *BatchingOptions) BatchRecommendation {
	totalTokens := int64(0)
	totalCost := 0.0
	totalLatency := int64(0)
	sampleIDs := []string{}

	for idx, log := range logs {
		totalTokens += log.TotalTokens
		totalCost += log.CostUSD
		totalLatency += log.LatencyMs
		if idx < 5 {
			sampleIDs = append(sampleIDs, log.ID)
		}
	}

	requestCount := len(logs)
	batchSize := suggestBatchSize(requestCount, options)
	recommendedBatches := int(math.Ceil(float64(requestCount) / float64(batchSize)))
	avgLatency := int64(0)
	if requestCount > 0 {
		avgLatency = totalLatency / int64(requestCount)
	}

	avgTokens := float64(0)
	if requestCount > 0 {
		avgTokens = float64(totalTokens) / float64(requestCount)
	}

	overheadTokens := int64(options.OverheadTokens)
	tokenRatio := int64(avgTokens * options.OverheadTokenRatio)
	if tokenRatio > overheadTokens {
		overheadTokens = tokenRatio
	}

	savingsRequests := requestCount - recommendedBatches
	if savingsRequests < 0 {
		savingsRequests = 0
	}

	estimatedTokensSaved := int64(savingsRequests) * overheadTokens
	costPerToken := 0.0
	if totalTokens > 0 {
		costPerToken = totalCost / float64(totalTokens)
	}

	estimatedCostSavings := float64(estimatedTokensSaved) * costPerToken
	estimatedLatencySavings := int64(savingsRequests) * options.OverheadLatencyMs
	efficiencyGain := 0.0
	if totalCost > 0 {
		efficiencyGain = (estimatedCostSavings / totalCost) * 100
	}

	return BatchRecommendation{
		ID:                        fmt.Sprintf("batch-%d", logs[0].Timestamp.UnixNano()),
		UserID:                    logs[0].UserID,
		ProviderID:                logs[0].ProviderID,
		ModelName:                 logs[0].ModelName,
		Method:                    logs[0].Method,
		Path:                      logs[0].Path,
		RequestCount:              requestCount,
		BatchSize:                 batchSize,
		RecommendedBatches:        recommendedBatches,
		TotalTokens:               totalTokens,
		TotalCostUSD:              totalCost,
		AvgLatencyMs:              avgLatency,
		TimeWindowStart:           logs[0].Timestamp,
		TimeWindowEnd:             logs[len(logs)-1].Timestamp,
		EstimatedTokensSaved:      estimatedTokensSaved,
		EstimatedCostSavingsUSD:   estimatedCostSavings,
		EstimatedLatencySavingsMs: estimatedLatencySavings,
		EfficiencyGainPercent:     efficiencyGain,
		SampleRequestIDs:          sampleIDs,
	}
}

func suggestBatchSize(requestCount int, options *BatchingOptions) int {
	if requestCount <= 0 {
		return 0
	}

	minSize := options.MinBatchSize
	if minSize <= 0 {
		minSize = 2
	}
	maxSize := options.MaxBatchSize
	if maxSize <= 0 {
		maxSize = requestCount
	}

	target := int(math.Ceil(float64(requestCount) / 2.0))
	if target < minSize {
		target = minSize
	}
	if target > maxSize {
		target = maxSize
	}
	if target > requestCount {
		target = requestCount
	}
	if target < 1 {
		target = 1
	}
	return target
}

func buildAutoBatchPlan(recommendation BatchRecommendation, logs []*RequestLog, options *BatchingOptions) []AutoBatchGroup {
	if len(logs) == 0 || recommendation.BatchSize <= 0 {
		return nil
	}

	plan := []AutoBatchGroup{}
	batchIndex := 0
	for idx := 0; idx < len(logs); idx += recommendation.BatchSize {
		end := idx + recommendation.BatchSize
		if end > len(logs) {
			end = len(logs)
		}
		requestIDs := make([]string, 0, end-idx)
		for _, log := range logs[idx:end] {
			requestIDs = append(requestIDs, log.ID)
		}

		plan = append(plan, AutoBatchGroup{
			ID:               fmt.Sprintf("%s-%d", recommendation.ID, batchIndex+1),
			RecommendationID: recommendation.ID,
			BatchSize:        recommendation.BatchSize,
			RequestIDs:       requestIDs,
		})
		batchIndex++
		if options.MaxAutoBatchGroups > 0 && len(plan) >= options.MaxAutoBatchGroups {
			break
		}
	}

	return plan
}
