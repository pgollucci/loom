package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jordanhubbard/loom/pkg/models"
)

// handleOrgChart handles org chart operations
func (s *Server) handleOrgChart(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/org-charts/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		s.respondError(w, http.StatusBadRequest, "Org chart ID is required")
		return
	}

	orgChartID := parts[0]

	if len(parts) > 1 && parts[1] == "positions" {
		if len(parts) > 2 {
			positionID := parts[2]
			if len(parts) > 3 && parts[3] == "assign" {
				s.handleAssignAgentToPosition(w, r, orgChartID, positionID)
			} else if len(parts) > 3 && parts[3] == "unassign" {
				s.handleUnassignAgentFromPosition(w, r, orgChartID, positionID)
			} else {
				s.handleDeletePosition(w, r, orgChartID, positionID)
			}
		} else {
			s.handleAddPosition(w, r, orgChartID)
		}
	} else {
		switch r.Method {
		case http.MethodGet:
			s.handleGetOrgChart(w, r, orgChartID)
		case http.MethodPut:
			s.handleUpdateOrgChart(w, r, orgChartID)
		default:
			s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	}
}

func (s *Server) handleGetOrgChart(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	mgr := s.app.GetOrgChartManager()
	if mgr == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Org chart manager not available")
		return
	}

	chart, err := mgr.GetByProject(projectID)
	if err != nil {
		project, projErr := s.app.GetProjectManager().GetProject(projectID)
		if projErr != nil {
			s.respondError(w, http.StatusNotFound, fmt.Sprintf("Org chart not found: %v", err))
			return
		}
		chart, err = mgr.CreateForProject(projectID, project.Name)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.respondJSON(w, http.StatusOK, chart)
}

func (s *Server) handleUpdateOrgChart(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodPut {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	// Org chart updates are handled via position assignment/unassignment.
	s.respondJSON(w, http.StatusOK, map[string]interface{}{"message": "Use /positions endpoints to modify the org chart"})
}

func (s *Server) handleAddPosition(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		RoleName     string `json:"role_name"`
		PersonaPath  string `json:"persona_path"`
		Required     bool   `json:"required"`
		MaxInstances int    `json:"max_instances"`
		ReportsTo    string `json:"reports_to,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	if req.RoleName == "" {
		s.respondError(w, http.StatusBadRequest, "role_name is required")
		return
	}

	mgr := s.app.GetOrgChartManager()
	if mgr == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Org chart manager not available")
		return
	}

	pos := models.Position{
		RoleName:     req.RoleName,
		PersonaPath:  req.PersonaPath,
		Required:     req.Required,
		MaxInstances: req.MaxInstances,
		ReportsTo:    req.ReportsTo,
	}
	if err := mgr.AddPosition(projectID, pos); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to add position: %v", err))
		return
	}
	s.respondJSON(w, http.StatusCreated, pos)
}

func (s *Server) handleDeletePosition(w http.ResponseWriter, r *http.Request, projectID, positionID string) {
	if r.Method != http.MethodDelete {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	mgr := s.app.GetOrgChartManager()
	if mgr == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Org chart manager not available")
		return
	}

	if err := mgr.RemovePosition(projectID, positionID); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to remove position: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAssignAgentToPosition(w http.ResponseWriter, r *http.Request, projectID, positionID string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	if req.AgentID == "" {
		s.respondError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	mgr := s.app.GetOrgChartManager()
	if mgr == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Org chart manager not available")
		return
	}

	if err := mgr.AssignAgent(projectID, positionID, req.AgentID); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to assign agent: %v", err))
		return
	}
	s.respondJSON(w, http.StatusOK, map[string]interface{}{"message": "Agent assigned successfully"})
}

func (s *Server) handleUnassignAgentFromPosition(w http.ResponseWriter, r *http.Request, projectID, positionID string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}
	if req.AgentID == "" {
		s.respondError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	mgr := s.app.GetOrgChartManager()
	if mgr == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Org chart manager not available")
		return
	}

	if err := mgr.UnassignAgent(projectID, positionID, req.AgentID); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to unassign agent: %v", err))
		return
	}
	s.respondJSON(w, http.StatusOK, map[string]interface{}{"message": "Agent unassigned successfully"})
}
