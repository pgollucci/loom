package api

import "net/http"

// handleSystemStatus handles GET /api/v1/system/status
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	d := s.app.GetDispatcher()
	if d == nil {
		s.respondJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "dispatcher": "unavailable"})
		return
	}
	status := d.GetSystemStatus()
	s.respondJSON(w, http.StatusOK, status)
}
