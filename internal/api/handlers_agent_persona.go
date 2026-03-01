package api

import (
	"net/http"
	"strings"
)

// handleAgentPersonaDetail handles GET /api/v1/agents/{id}/persona
// Returns the full three-file persona for the named agent
func (s *Server) handleAgentPersonaDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract agent ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "persona" {
		s.respondError(w, http.StatusNotFound, "Not found")
		return
	}

	agentID := parts[0]
	if agentID == "" {
		s.respondError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	// Get agent
	agent, err := s.app.GetAgentManager().GetAgent(agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Agent not found")
		return
	}

	if agent.Persona == nil {
		s.respondError(w, http.StatusNotFound, "Agent has no persona")
		return
	}

	// Load full persona with file contents
	personaName := agent.Persona.Name
	if personaName == "" {
		s.respondError(w, http.StatusInternalServerError, "Agent persona name is empty")
		return
	}

	fullPersona, err := s.app.GetPersonaManager().LoadPersona(personaName)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to load persona details")
		return
	}

	s.respondJSON(w, http.StatusOK, fullPersona)
}
