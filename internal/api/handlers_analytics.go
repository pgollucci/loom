package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jordanhubbard/agenticorp/internal/analytics"
	"github.com/jordanhubbard/agenticorp/internal/auth"
)

// handleGetLogs handles GET /api/v1/analytics/logs
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetUserIDFromRequest(r)
	// If auth is disabled, allow access with empty userID (show all logs)
	if userID == "" && s.config.Security.EnableAuth {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	filter := &analytics.LogFilter{
		UserID: userID, // Users can only see their own logs (or all if auth disabled)
		Limit:  100,    // Default limit
	}

	if providerID := r.URL.Query().Get("provider_id"); providerID != "" {
		filter.ProviderID = providerID
	}

	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = t
		}
	}

	logs, err := s.analyticsLogger.GetLogs(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// handleGetLogStats handles GET /api/v1/analytics/stats
func (s *Server) handleGetLogStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetUserIDFromRequest(r)
	// If auth is disabled, allow access with empty userID (show all stats)
	if userID == "" && s.config.Security.EnableAuth {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	filter := &analytics.LogFilter{
		UserID: userID, // Users can only see their own stats (or all if auth disabled)
	}

	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = t
		}
	}

	// Admin can see all stats
	role := auth.GetRoleFromRequest(r)
	if role == "admin" {
		filter.UserID = "" // Remove user filter for admins
		if queryUserID := r.URL.Query().Get("user_id"); queryUserID != "" {
			filter.UserID = queryUserID
		}
	}

	stats, err := s.analyticsLogger.GetStats(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleExportLogs handles GET /api/v1/analytics/export
func (s *Server) handleExportLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetUserIDFromRequest(r)
	// If auth is disabled, allow access with empty userID (export all logs)
	if userID == "" && s.config.Security.EnableAuth {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	filter := &analytics.LogFilter{
		UserID: userID,
		Limit:  10000, // Higher limit for exports
	}

	if providerID := r.URL.Query().Get("provider_id"); providerID != "" {
		filter.ProviderID = providerID
	}

	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = t
		}
	}

	logs, err := s.analyticsLogger.GetLogs(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Default to JSON export
	format := r.URL.Query().Get("format")
	switch format {
	case "csv":
		exportLogsAsCSV(w, logs)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\"logs.json\"")
		json.NewEncoder(w).Encode(logs)
	}
}

// handleGetCostReport handles GET /api/v1/analytics/costs
func (s *Server) handleGetCostReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetUserIDFromRequest(r)
	// If auth is disabled, allow access with empty userID (show all costs)
	if userID == "" && s.config.Security.EnableAuth {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	filter := &analytics.LogFilter{
		UserID: userID, // Users can only see their own costs by default (or all if auth disabled)
	}

	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = t
		}
	}

	// Admin can see all costs
	role := auth.GetRoleFromRequest(r)
	if role == "admin" {
		filter.UserID = "" // Remove user filter for admins
		if queryUserID := r.URL.Query().Get("user_id"); queryUserID != "" {
			filter.UserID = queryUserID
		}
	}

	stats, err := s.analyticsLogger.GetStats(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build cost report
	costReport := map[string]interface{}{
		"total_cost_usd": stats.TotalCostUSD,
		"total_requests": stats.TotalRequests,
		"total_tokens":   stats.TotalTokens,
		"cost_per_request": func() float64 {
			if stats.TotalRequests > 0 {
				return stats.TotalCostUSD / float64(stats.TotalRequests)
			}
			return 0.0
		}(),
		"cost_per_1k_tokens": func() float64 {
			if stats.TotalTokens > 0 {
				return (stats.TotalCostUSD / float64(stats.TotalTokens)) * 1000
			}
			return 0.0
		}(),
		"cost_by_provider": stats.CostByProvider,
		"cost_by_user":     stats.CostByUser,
		"time_range": map[string]interface{}{
			"start": filter.StartTime,
			"end":   filter.EndTime,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(costReport)
}

// handleExportStats handles GET /api/v1/analytics/export-stats
func (s *Server) handleExportStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetUserIDFromRequest(r)
	// If auth is disabled, allow access with empty userID (export all stats)
	if userID == "" && s.config.Security.EnableAuth {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	filter := &analytics.LogFilter{
		UserID: userID,
	}

	if startTime := r.URL.Query().Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = t
		}
	}

	if endTime := r.URL.Query().Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = t
		}
	}

	// Admin can see all stats
	role := auth.GetRoleFromRequest(r)
	if role == "admin" {
		filter.UserID = ""
		if queryUserID := r.URL.Query().Get("user_id"); queryUserID != "" {
			filter.UserID = queryUserID
		}
	}

	stats, err := s.analyticsLogger.GetStats(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Export format
	format := r.URL.Query().Get("format")
	switch format {
	case "csv":
		exportStatsAsCSV(w, stats, filter)
	default:
		// JSON export
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\"agenticorp-stats-"+time.Now().Format("2006-01-02")+".json\"")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exported_at": time.Now().Format(time.RFC3339),
			"time_range": map[string]string{
				"start": filter.StartTime.Format(time.RFC3339),
				"end":   filter.EndTime.Format(time.RFC3339),
			},
			"summary": map[string]interface{}{
				"total_requests": stats.TotalRequests,
				"total_tokens":   stats.TotalTokens,
				"total_cost_usd": stats.TotalCostUSD,
				"avg_latency_ms": stats.AvgLatencyMs,
				"error_rate":     stats.ErrorRate,
			},
			"cost_by_provider":     stats.CostByProvider,
			"cost_by_user":         stats.CostByUser,
			"requests_by_provider": stats.RequestsByProvider,
			"requests_by_user":     stats.RequestsByUser,
		})
	}
}

// exportStatsAsCSV exports stats summary in CSV format
func exportStatsAsCSV(w http.ResponseWriter, stats *analytics.LogStats, filter *analytics.LogFilter) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"agenticorp-stats-"+time.Now().Format("2006-01-02")+".csv\"")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Summary section
	writer.Write([]string{"Summary", "", "", ""})
	writer.Write([]string{"Metric", "Value", "", ""})
	writer.Write([]string{"Total Requests", fmt.Sprintf("%d", stats.TotalRequests), "", ""})
	writer.Write([]string{"Total Tokens", fmt.Sprintf("%d", stats.TotalTokens), "", ""})
	writer.Write([]string{"Total Cost (USD)", fmt.Sprintf("%.4f", stats.TotalCostUSD), "", ""})
	writer.Write([]string{"Avg Latency (ms)", fmt.Sprintf("%.2f", stats.AvgLatencyMs), "", ""})
	writer.Write([]string{"Error Rate", fmt.Sprintf("%.2f%%", stats.ErrorRate*100), "", ""})
	writer.Write([]string{""})

	// Cost by Provider
	writer.Write([]string{"Cost by Provider", "", "", ""})
	writer.Write([]string{"Provider ID", "Requests", "Cost (USD)", ""})
	for provider, cost := range stats.CostByProvider {
		requests := stats.RequestsByProvider[provider]
		writer.Write([]string{provider, fmt.Sprintf("%d", requests), fmt.Sprintf("%.4f", cost), ""})
	}
	writer.Write([]string{""})

	// Cost by User
	writer.Write([]string{"Cost by User", "", "", ""})
	writer.Write([]string{"User ID", "Requests", "Cost (USD)", ""})
	for user, cost := range stats.CostByUser {
		requests := stats.RequestsByUser[user]
		writer.Write([]string{user, fmt.Sprintf("%d", requests), fmt.Sprintf("%.4f", cost), ""})
	}
}

// exportLogsAsCSV exports logs in CSV format
func exportLogsAsCSV(w http.ResponseWriter, logs []*analytics.RequestLog) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"agenticorp-logs-"+time.Now().Format("2006-01-02")+".csv\"")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV header
	writer.Write([]string{
		"Timestamp",
		"User ID",
		"Method",
		"Path",
		"Provider ID",
		"Model",
		"Prompt Tokens",
		"Completion Tokens",
		"Total Tokens",
		"Latency (ms)",
		"Status Code",
		"Cost (USD)",
		"Error Message",
	})

	// Write data rows
	for _, log := range logs {
		writer.Write([]string{
			log.Timestamp.Format(time.RFC3339),
			log.UserID,
			log.Method,
			log.Path,
			log.ProviderID,
			log.ModelName,
			fmt.Sprintf("%d", log.PromptTokens),
			fmt.Sprintf("%d", log.CompletionTokens),
			fmt.Sprintf("%d", log.TotalTokens),
			fmt.Sprintf("%d", log.LatencyMs),
			fmt.Sprintf("%d", log.StatusCode),
			fmt.Sprintf("%.4f", log.CostUSD),
			log.ErrorMessage,
		})
	}
}
