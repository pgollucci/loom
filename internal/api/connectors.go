package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jordanhubbard/loom/pkg/connectors"
)

// ConnectorResponse is the API response format for a connector
type ConnectorResponse struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Type        connectors.ConnectorType   `json:"type"`
	Mode        connectors.ConnectionMode  `json:"mode"`
	Enabled     bool                       `json:"enabled"`
	Description string                     `json:"description"`
	Endpoint    string                     `json:"endpoint"`
	Status      connectors.ConnectorStatus `json:"status"`
	Tags        []string                   `json:"tags"`
	Metadata    map[string]string          `json:"metadata,omitempty"`
}

// HandleConnectors routes connector requests based on path and method
func (s *Server) HandleConnectors(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/connectors")

	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			s.listConnectors(w, r)
		case http.MethodPost:
			s.createConnector(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if path == "/health" {
		if r.Method == http.MethodGet {
			s.checkAllHealth(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Connector ID required", http.StatusBadRequest)
		return
	}

	id := parts[0]

	if len(parts) > 1 {
		switch parts[1] {
		case "health":
			if r.Method == http.MethodGet {
				s.checkConnectorHealth(w, r, id)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		case "test":
			if r.Method == http.MethodPost {
				s.testConnector(w, r, id)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		default:
			http.Error(w, "Unknown endpoint", http.StatusNotFound)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getConnector(w, r, id)
	case http.MethodPut:
		s.updateConnector(w, r, id)
	case http.MethodDelete:
		s.deleteConnector(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listConnectors(w http.ResponseWriter, r *http.Request) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}

	all := s.connectorService.ListConnectors()
	responses := make([]ConnectorResponse, 0, len(all))
	for _, c := range all {
		responses = append(responses, ConnectorResponse{
			ID:          c.ID,
			Name:        c.Name,
			Type:        c.Type,
			Mode:        c.Mode,
			Enabled:     c.Enabled,
			Description: c.Description,
			Endpoint:    c.Endpoint,
			Status:      c.Status,
			Tags:        c.Tags,
			Metadata:    c.Metadata,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connectors": responses,
		"count":      len(responses),
	})
}

func (s *Server) getConnector(w http.ResponseWriter, r *http.Request, id string) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}

	c, err := s.connectorService.GetConnector(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	response := ConnectorResponse{
		ID:          c.ID,
		Name:        c.Name,
		Type:        c.Type,
		Mode:        c.Mode,
		Enabled:     c.Enabled,
		Description: c.Description,
		Endpoint:    c.Endpoint,
		Status:      c.Status,
		Tags:        c.Tags,
		Metadata:    c.Metadata,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) createConnector(w http.ResponseWriter, r *http.Request) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}
	var cfg connectors.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if cfg.ID == "" {
		http.Error(w, "Connector ID is required", http.StatusBadRequest)
		return
	}
	if cfg.Name == "" {
		http.Error(w, "Connector name is required", http.StatusBadRequest)
		return
	}
	if cfg.Host == "" {
		http.Error(w, "Connector host is required", http.StatusBadRequest)
		return
	}
	if cfg.Port == 0 {
		http.Error(w, "Connector port is required", http.StatusBadRequest)
		return
	}

	id, err := s.connectorService.AddConnector(r.Context(), cfg)
	if err != nil {
		http.Error(w, "Failed to create connector: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Connector created successfully",
		"id":      id,
	})
}

func (s *Server) updateConnector(w http.ResponseWriter, r *http.Request, id string) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}

	var cfg connectors.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg.ID = id
	if err := s.connectorService.UpdateConnector(r.Context(), id, cfg); err != nil {
		http.Error(w, "Failed to update connector: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Connector updated successfully",
	})
}

func (s *Server) deleteConnector(w http.ResponseWriter, r *http.Request, id string) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := s.connectorService.RemoveConnector(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete connector: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Connector deleted successfully",
	})
}

func (s *Server) checkConnectorHealth(w http.ResponseWriter, r *http.Request, id string) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}

	status, err := s.connectorService.HealthCheck(r.Context(), id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      id,
			"status":  status,
			"healthy": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"status":  status,
		"healthy": status == connectors.ConnectorStatusHealthy,
	})
}

func (s *Server) checkAllHealth(w http.ResponseWriter, r *http.Request) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}
	healthStatus := s.connectorService.GetHealthStatus()

	results := make(map[string]interface{})
	for id, status := range healthStatus {
		results[id] = map[string]interface{}{
			"status":  status,
			"healthy": status == connectors.ConnectorStatusHealthy,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
	})
}

func (s *Server) testConnector(w http.ResponseWriter, r *http.Request, id string) {
	if s.connectorService == nil {
		http.Error(w, "Connector service not initialized", http.StatusServiceUnavailable)
		return
	}

	status, endpoint, err := s.connectorService.TestConnector(r.Context(), id)

	response := map[string]interface{}{
		"id":       id,
		"status":   status,
		"endpoint": endpoint,
	}

	if err != nil {
		response["error"] = err.Error()
		response["success"] = false
	} else {
		response["success"] = status == connectors.ConnectorStatusHealthy
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
