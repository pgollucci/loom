package api

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/jordanhubbard/loom/internal/project"
	"github.com/jordanhubbard/loom/pkg/models"
)

// handleProjectStateEndpoints handles project state management endpoints
func (s *Server) handleProjectStateEndpoints(w http.ResponseWriter, r *http.Request, id string, action string) {
	switch action {
	case "close":
		s.handleCloseProject(w, r, id)
	case "reopen":
		s.handleReopenProject(w, r, id)
	case "comments":
		s.handleProjectComments(w, r, id)
	case "state":
		s.handleProjectState(w, r, id)
	case "agents":
		s.handleProjectAgents(w, r, id)
	case "git-key":
		s.handleProjectGitKey(w, r, id)
	case "github":
		s.handleProjectGitHub(w, r, id)
	case "memory":
		s.handleProjectMemory(w, r, id)
	default:
		s.respondError(w, http.StatusNotFound, "Unknown action")
	}
}

// handleProjectAgents handles GET /api/v1/projects/{id}/agents
// handleProjectBeads handles GET /api/v1/projects/{id}/beads
// handleProjectWorkflows handles GET /api/v1/projects/{id}/workflows
// handleProjectLogs handles GET /api/v1/projects/{id}/logs
// handleProjectEvents handles GET /api/v1/projects/{id}/events
// handleProjectConversations handles GET /api/v1/projects/{id}/conversations
func (s *Server) handleProjectAgents(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
		Action  string `json:"action"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.AgentID == "" {
		s.respondError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if req.Action == "" {
		req.Action = "assign"
	}

	switch req.Action {
	case "assign":
		if err := s.app.AssignAgentToProject(req.AgentID, id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				s.respondError(w, http.StatusNotFound, err.Error())
			} else {
				s.respondError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
	case "unassign":
		if err := s.app.UnassignAgentFromProject(req.AgentID, id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				s.respondError(w, http.StatusNotFound, err.Error())
			} else {
				s.respondError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
	default:
		s.respondError(w, http.StatusBadRequest, "action must be assign or unassign")
		return
	}

	project, _ := s.app.GetProjectManager().GetProject(id)
	s.respondJSON(w, http.StatusOK, project)
}

// handleProjectGitKey handles GET/POST /api/v1/projects/{id}/git-key
func (s *Server) handleProjectGitKey(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	project, err := s.app.GetProjectManager().GetProject(id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Project not found")
		return
	}

	var publicKey string
	if r.Method == http.MethodPost {
		publicKey, err = s.app.RotateProjectGitKey(id)
	} else {
		publicKey, err = s.app.GetProjectGitPublicKey(id)
	}
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"project_id":  project.ID,
		"auth_method": project.GitAuthMethod,
		"public_key":  publicKey,
		"rotated":     r.Method == http.MethodPost,
	})
}

// handleCloseProject handles POST /api/v1/projects/{id}/close
func (s *Server) handleCloseProject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		AuthorID string `json:"author_id"`
		Comment  string `json:"comment"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AuthorID == "" {
		s.respondError(w, http.StatusBadRequest, "author_id is required")
		return
	}

	// Check if project has open work
	openBeads, err := s.app.GetReadyBeads(id)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hasOpenWork := len(openBeads) > 0
	if hasOpenWork {
		// Create a decision bead for closure
		decision, err := s.app.CreateDecisionBead(
			fmt.Sprintf("Should project '%s' be closed despite having %d open beads?", id, len(openBeads)),
			"",
			req.AuthorID,
			[]string{"yes", "no"},
			"no",
			models.BeadPriorityP1,
			id,
		)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.respondJSON(w, http.StatusAccepted, map[string]interface{}{
			"status":        "decision_required",
			"message":       "Project has open work, decision required from agents",
			"decision_bead": decision,
		})
		return
	}

	// No open work, close directly
	if err := s.app.GetProjectManager().CloseProject(id, req.AuthorID, req.Comment); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.app.PersistProject(id)

	project, _ := s.app.GetProjectManager().GetProject(id)
	s.respondJSON(w, http.StatusOK, project)
}

// handleReopenProject handles POST /api/v1/projects/{id}/reopen
func (s *Server) handleReopenProject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		AuthorID string `json:"author_id"`
		Comment  string `json:"comment"`
	}
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AuthorID == "" {
		s.respondError(w, http.StatusBadRequest, "author_id is required")
		return
	}

	if err := s.app.GetProjectManager().ReopenProject(id, req.AuthorID, req.Comment); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.app.PersistProject(id)

	project, _ := s.app.GetProjectManager().GetProject(id)
	s.respondJSON(w, http.StatusOK, project)
}

// handleProjectComments handles GET/POST /api/v1/projects/{id}/comments
func (s *Server) handleProjectComments(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		comments, err := s.app.GetProjectManager().GetComments(id)
		if err != nil {
			s.respondError(w, http.StatusNotFound, err.Error())
			return
		}
		s.respondJSON(w, http.StatusOK, comments)

	case http.MethodPost:
		var req struct {
			AuthorID string `json:"author_id"`
			Comment  string `json:"comment"`
		}
		if err := s.parseJSON(r, &req); err != nil {
			s.respondError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.AuthorID == "" || req.Comment == "" {
			s.respondError(w, http.StatusBadRequest, "author_id and comment are required")
			return
		}

		comment, err := s.app.GetProjectManager().AddComment(id, req.AuthorID, req.Comment)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.app.PersistProject(id)

		s.respondJSON(w, http.StatusCreated, comment)

	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleProjectState handles GET /api/v1/projects/{id}/state
func (s *Server) handleProjectState(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	project, err := s.app.GetProjectManager().GetProject(id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Project not found")
		return
	}

	// Check for open work
	openBeads, _ := s.app.GetReadyBeads(id)
	hasOpenWork := len(openBeads) > 0
	canClose := s.app.GetProjectManager().CanClose(id, hasOpenWork)

	// Check project readiness
	readinessOK, readinessIssues := s.app.CheckProjectReadiness(r.Context(), id)

	state := map[string]interface{}{
		"id":               project.ID,
		"name":             project.Name,
		"status":           project.Status,
		"is_perpetual":     project.IsPerpetual,
		"is_sticky":        project.IsSticky,
		"open_beads":       len(openBeads),
		"can_close":        canClose,
		"readiness_ok":     readinessOK,
		"readiness_issues": readinessIssues,
		"created_at":       project.CreatedAt,
		"updated_at":       project.UpdatedAt,
		"closed_at":        project.ClosedAt,
		"comments_count":   len(project.Comments),
	}

	s.respondJSON(w, http.StatusOK, state)
}

// handleProjectBeadsReset handles POST /api/v1/projects/{id}/beads/reset
// It clears the in-memory bead state for the project and reloads from the
// beads-sync branch on disk/git. Use this after a force-push, dolt recovery,
// or any out-of-band change to the beads worktree.
func (s *Server) handleProjectBeadsReset(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	count, err := s.app.ReloadProjectBeads(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "project not found") || strings.Contains(err.Error(), "no beads path") {
			s.respondError(w, http.StatusNotFound, err.Error())
		} else {
			s.respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Wake the executor now that beads have been reloaded
	s.app.WakeProject(id)

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"project_id":   id,
		"beads_loaded": count,
		"status":       "reloaded",
	})
}

// handleBootstrapProject handles POST /api/v1/projects/bootstrap
func (s *Server) handleBootstrapProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req project.BootstrapRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Create bootstrap service (using current directory as workspace for now)
	workspaceDir := "./projects"
	bootstrapService := project.NewBootstrapService(
		s.app.GetProjectManager(),
		"./templates",
		workspaceDir,
		s.app.GetGitOpsManager(),
		s.config.Beads.Backend,
	)

	// Ensure workspace directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create workspace directory: %v", err))
		return
	}

	// Bootstrap the project
	result, err := bootstrapService.Bootstrap(r.Context(), req)
	if err != nil {
		log.Printf("[Bootstrap] Error: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Create default agents (org chart) for the new project.
	// Bootstrap uses project.Manager.CreateProjectWithID directly, bypassing
	// Loom.CreateProject which normally calls ensureDefaultAgents.
	if result.ProjectID != "" {
		if agentErr := s.app.EnsureDefaultAgents(r.Context(), result.ProjectID); agentErr != nil {
			log.Printf("[Bootstrap] Warning: failed to create default agents for %s: %v", result.ProjectID, agentErr)
		}
	}

	s.respondJSON(w, http.StatusCreated, result)
}
