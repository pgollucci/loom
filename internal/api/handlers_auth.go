package api

import (
	"net/http"
)

// ChangePasswordRequest is the request body for changing the password
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword handles POST /api/v1/auth/change-password
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req ChangePasswordRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		s.respondError(w, http.StatusBadRequest, "Old password and new password are required")
		return
	}

	if req.NewPassword == req.OldPassword {
		s.respondError(w, http.StatusBadRequest, "New password must be different from old password")
		return
	}

	if len(req.NewPassword) < 8 {
		s.respondError(w, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	if s.keyManager == nil {
		s.respondError(w, http.StatusInternalServerError, "Key manager not initialized")
		return
	}

	if !s.keyManager.IsUnlocked() {
		s.respondError(w, http.StatusInternalServerError, "Key manager is locked")
		return
	}

	// Change the password
	if err := s.keyManager.ChangePassword(req.OldPassword, req.NewPassword); err != nil {
		// Don't leak details about whether password verification failed
		s.respondError(w, http.StatusUnauthorized, "Failed to change password: invalid old password")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"message": "Password changed successfully",
	})
}
