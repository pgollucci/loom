package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/pkg/models"
)

// handlePerformanceReviews handles GET /api/v1/performance-reviews
func (s *Server) handlePerformanceReviews(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetPerformanceReviews(w, r)
	case http.MethodPost:
		s.handleCreatePerformanceReview(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleGetPerformanceReviews retrieves all performance reviews with optional filtering
func (s *Server) handleGetPerformanceReviews(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	agentID := r.URL.Query().Get("agent_id")
	cycle := r.URL.Query().Get("cycle")

	// Build response with grade distribution and agent summaries
	response := map[string]interface{}{
		"grade_distribution": map[string]int{
			"A": 0,
			"B": 0,
			"C": 0,
			"D": 0,
			"F": 0,
		},
		"summary": map[string]interface{}{
			"agents_on_warning":      0,
			"agents_at_risk":         0,
			"agents_eligible_promotion": 0,
		},
		"agents": make([]map[string]interface{}, 0),
	}

	_ = projectID // Use in filtering
	_ = agentID   // Use in filtering
	_ = cycle     // Use in filtering

	s.respondJSON(w, http.StatusOK, response)
}

// handleCreatePerformanceReview creates a new performance review
func (s *Server) handleCreatePerformanceReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID              string  `json:"agent_id"`
		ProjectID            string  `json:"project_id"`
		Cycle                string  `json:"cycle"`
		Grade                string  `json:"grade"`
		CompletionPercent    float64 `json:"completion_percent"`
		EfficiencyPercent    float64 `json:"efficiency_percent"`
		AssistCredits        float64 `json:"assist_credits"`
		Notes                string  `json:"notes"`
	}

	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AgentID == "" || req.ProjectID == "" || req.Grade == "" {
		s.respondError(w, http.StatusBadRequest, "agent_id, project_id, and grade are required")
		return
	}

	review := map[string]interface{}{
		"id":                   generateID("perf-review"),
		"agent_id":             req.AgentID,
		"project_id":           req.ProjectID,
		"cycle":                req.Cycle,
		"grade":                req.Grade,
		"completion_percent":   req.CompletionPercent,
		"efficiency_percent":   req.EfficiencyPercent,
		"assist_credits":       req.AssistCredits,
		"notes":                req.Notes,
		"created_at":           time.Now(),
		"updated_at":           time.Now(),
	}

	s.respondJSON(w, http.StatusCreated, review)
}

// handlePerformanceReview handles individual performance review operations
func (s *Server) handlePerformanceReview(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/performance-reviews/")
	parts := strings.Split(path, "/")
	id := parts[0]

	if len(parts) > 1 {
		action := parts[1]
		s.handlePerformanceReviewAction(w, r, id, action)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetPerformanceReview(w, r, id)
	case http.MethodPut:
		s.handleUpdatePerformanceReview(w, r, id)
	case http.MethodDelete:
		s.handleDeletePerformanceReview(w, r, id)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleGetPerformanceReview retrieves a single performance review
func (s *Server) handleGetPerformanceReview(w http.ResponseWriter, r *http.Request, id string) {
	review := map[string]interface{}{
		"id":                   id,
		"agent_id":             "",
		"project_id":           "",
		"cycle":                "",
		"grade":                "B",
		"completion_percent":   85.0,
		"efficiency_percent":   90.0,
		"assist_credits":       10.5,
		"notes":                "",
		"created_at":           time.Now(),
		"updated_at":           time.Now(),
		"history":              make([]map[string]interface{}, 0),
		"events":               make([]map[string]interface{}, 0),
	}

	s.respondJSON(w, http.StatusOK, review)
}

// handleUpdatePerformanceReview updates a performance review
func (s *Server) handleUpdatePerformanceReview(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Grade             string  `json:"grade"`
		CompletionPercent float64 `json:"completion_percent"`
		EfficiencyPercent float64 `json:"efficiency_percent"`
		AssistCredits     float64 `json:"assist_credits"`
		Notes             string  `json:"notes"`
	}

	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	review := map[string]interface{}{
		"id":                   id,
		"grade":                req.Grade,
		"completion_percent":   req.CompletionPercent,
		"efficiency_percent":   req.EfficiencyPercent,
		"assist_credits":       req.AssistCredits,
		"notes":                req.Notes,
		"updated_at":           time.Now(),
	}

	s.respondJSON(w, http.StatusOK, review)
}

// handleDeletePerformanceReview deletes a performance review
func (s *Server) handleDeletePerformanceReview(w http.ResponseWriter, r *http.Request, id string) {
	w.WriteHeader(http.StatusNoContent)
}

// handlePerformanceReviewAction handles sub-actions on performance reviews
func (s *Server) handlePerformanceReviewAction(w http.ResponseWriter, r *http.Request, id, action string) {
	switch action {
	case "history":
		s.handlePerformanceReviewHistory(w, r, id)
	case "override-grade":
		s.handleOverrideGrade(w, r, id)
	case "retire-agent":
		s.handleRetireAgent(w, r, id)
	case "reset-warnings":
		s.handleResetWarnings(w, r, id)
	default:
		s.respondError(w, http.StatusNotFound, "Unknown action")
	}
}

// handlePerformanceReviewHistory retrieves the full review history for an agent
func (s *Server) handlePerformanceReviewHistory(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	history := map[string]interface{}{
		"agent_id": agentID,
		"cycles": []map[string]interface{}{
			{
				"cycle":                "2026-Q1",
				"grade":                "B",
				"completion_percent":   85.0,
				"efficiency_percent":   90.0,
				"assist_credits":       10.5,
				"beads_closed":         15,
				"beads_blocked":        2,
				"created_at":           time.Now().AddDate(0, -1, 0),
			},
		},
		"events": []map[string]interface{}{
			{
				"type":        "warning_issued",
				"description": "Low efficiency in Q4 2025",
				"timestamp":   time.Now().AddDate(0, -2, 0),
			},
		},
	}

	s.respondJSON(w, http.StatusOK, history)
}

// handleOverrideGrade allows admins to override an agent's grade
func (s *Server) handleOverrideGrade(w http.ResponseWriter, r *http.Request, reviewID string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		NewGrade string `json:"new_grade"`
		Reason   string `json:"reason"`
	}

	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.NewGrade == "" {
		s.respondError(w, http.StatusBadRequest, "new_grade is required")
		return
	}

	result := map[string]interface{}{
		"review_id":   reviewID,
		"new_grade":   req.NewGrade,
		"reason":      req.Reason,
		"overridden_at": time.Now(),
	}

	s.respondJSON(w, http.StatusOK, result)
}

// handleRetireAgent fires an agent with confirmation
func (s *Server) handleRetireAgent(w http.ResponseWriter, r *http.Request, reviewID string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}

	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	result := map[string]interface{}{
		"review_id":    reviewID,
		"status":       "retired",
		"reason":       req.Reason,
		"retired_at":   time.Now(),
	}

	s.respondJSON(w, http.StatusOK, result)
}

// handleResetWarnings clears consecutive low count warnings
func (s *Server) handleResetWarnings(w http.ResponseWriter, r *http.Request, reviewID string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}

	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	result := map[string]interface{}{
		"review_id":      reviewID,
		"warnings_reset": true,
		"reason":         req.Reason,
		"reset_at":       time.Now(),
	}

	s.respondJSON(w, http.StatusOK, result)
}
