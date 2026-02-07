package api

import (
	"encoding/json"
	"net/http"
)

// handleFederationStatus handles GET /api/v1/federation/status
func (s *Server) handleFederationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.config.Beads.Federation.Enabled {
		s.respondJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": false,
			"message": "Federation is not enabled",
		})
		return
	}

	output, err := s.app.GetBeadsManager().FederationStatus(r.Context())
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse and re-encode to ensure valid JSON response
	var status interface{}
	if err := json.Unmarshal(output, &status); err != nil {
		// Return raw output as a string if not valid JSON
		s.respondJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": true,
			"raw":     string(output),
		})
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
		"status":  status,
	})
}

// handleFederationSync handles POST /api/v1/federation/sync
func (s *Server) handleFederationSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.config.Beads.Federation.Enabled {
		s.respondError(w, http.StatusBadRequest, "Federation is not enabled")
		return
	}

	err := s.app.GetBeadsManager().SyncFederation(r.Context(), &s.config.Beads.Federation)
	if err != nil {
		s.respondJSON(w, http.StatusOK, map[string]interface{}{
			"synced": false,
			"error":  err.Error(),
		})
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"synced": true,
	})
}
