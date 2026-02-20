package api

import (
	"net/http"
	"os"
	"time"
)

// handleSystemState handles GET /api/v1/system/state
// Returns a machine-readable JSON snapshot of the entire running system.
// An LLM can call this endpoint to understand system state without reading logs.
func (s *Server) handleSystemState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	now := time.Now().UTC()
	app := s.app

	// ── Control plane ──────────────────────────────────────────────────────
	hostname, _ := os.Hostname()
	uptimeSec := int64(0)
	if sa := app.StartedAt(); !sa.IsZero() {
		uptimeSec = int64(now.Sub(sa).Seconds())
	}

	dbStatus := "not configured"
	if app.GetDatabase() != nil {
		dbStatus = "connected"
	}
	natsStatus := "not configured"
	if mb := app.GetMessageBus(); mb != nil {
		natsStatus = "connected"
	}

	// Providers
	type providerInfo struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var providers []providerInfo
	if reg := app.GetProviderRegistry(); reg != nil {
		for _, p := range reg.List() {
			if p.Config == nil {
				continue
			}
			providers = append(providers, providerInfo{
				ID:     p.Config.ID,
				Name:   p.Config.Name,
				Type:   p.Config.Type,
				Status: p.Config.Status,
			})
		}
	}

	controlPlane := map[string]interface{}{
		"instance_id":    hostname,
		"uptime_seconds": uptimeSec,
		"database":       dbStatus,
		"nats":           natsStatus,
		"providers":      providers,
	}

	// ── Dispatch queue ─────────────────────────────────────────────────────
	var dispatchQueue map[string]interface{}
	if d := app.GetDispatcher(); d != nil {
		status := d.GetSystemStatus()
		dispatchQueue = map[string]interface{}{
			"state":      string(status.State),
			"reason":     status.Reason,
			"updated_at": status.UpdatedAt,
		}
	}

	// ── Projects ───────────────────────────────────────────────────────────
	type projectInfo struct {
		ID           string      `json:"id"`
		Name         string      `json:"name"`
		Status       string      `json:"status"`
		GitHubRepo   string      `json:"github_repo,omitempty"`
		LastActivity interface{} `json:"last_activity,omitempty"`
		OpenBeads    int         `json:"open_beads"`
	}
	var projectInfos []projectInfo
	if pm := app.GetProjectManager(); pm != nil {
		projects := pm.ListProjects()
		for _, p := range projects {
			openBeads := 0
			if bm := app.GetBeadsManager(); bm != nil {
				beads, _ := bm.ListBeads(map[string]interface{}{
					"project_id": p.ID,
					"status":     "open",
				})
				openBeads = len(beads)
			}
			pi := projectInfo{
				ID:         p.ID,
				Name:       p.Name,
				Status:     string(p.Status),
				GitHubRepo: p.GitHubRepo,
				OpenBeads:  openBeads,
			}
			if !p.UpdatedAt.IsZero() {
				pi.LastActivity = p.UpdatedAt
			}
			projectInfos = append(projectInfos, pi)
		}
	}

	// ── Swarm members ──────────────────────────────────────────────────────
	type memberInfo struct {
		ServiceID   string   `json:"service_id"`
		ServiceType string   `json:"service_type"`
		InstanceID  string   `json:"instance_id"`
		Roles       []string `json:"roles"`
		ProjectIDs  []string `json:"project_ids"`
		Status      string   `json:"status"`
		LastSeen    string   `json:"last_seen"`
	}
	var members []memberInfo
	if sm := app.GetSwarmManager(); sm != nil {
		for _, m := range sm.GetMembers() {
			members = append(members, memberInfo{
				ServiceID:   m.ServiceID,
				ServiceType: m.ServiceType,
				InstanceID:  m.InstanceID,
				Roles:       m.Roles,
				ProjectIDs:  m.ProjectIDs,
				Status:      m.Status,
				LastSeen:    m.LastSeen.UTC().Format(time.RFC3339),
			})
		}
	}

	// ── Recent errors ──────────────────────────────────────────────────────
	type errorEntry struct {
		Timestamp string `json:"timestamp"`
		Source    string `json:"source"`
		Message   string `json:"message"`
	}
	var recentErrors []errorEntry
	if lm := app.GetLogManager(); lm != nil {
		logs, _ := lm.Query(10, "error", "", "", "", "", time.Time{}, time.Time{})
		for _, l := range logs {
			recentErrors = append(recentErrors, errorEntry{
				Timestamp: l.Timestamp.UTC().Format(time.RFC3339),
				Source:    l.Source,
				Message:   l.Message,
			})
		}
	}

	// ── Assemble response ──────────────────────────────────────────────────
	state := map[string]interface{}{
		"timestamp":      now.Format(time.RFC3339),
		"control_plane":  controlPlane,
		"dispatch_queue": dispatchQueue,
		"projects":       projectInfos,
		"swarm_members":  members,
		"recent_errors":  recentErrors,
	}

	s.respondJSON(w, http.StatusOK, state)
}
