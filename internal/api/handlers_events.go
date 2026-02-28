package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jordanhubbard/loom/internal/eventbus"
)

// handleEventStream handles SSE endpoint for real-time event updates
// GET /api/v1/events/stream
func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	eventBus := s.app.GetEventBus()
	if eventBus == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Event bus not available")
		return
	}

	// Disable write timeout for SSE - the server's WriteTimeout (30s default)
	// would kill long-running streams.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Get optional filters from query params
	projectID := r.URL.Query().Get("project_id")
	eventType := r.URL.Query().Get("type")

	// Create subscriber with filter
	subscriberID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	filter := func(event *eventbus.Event) bool {
		if projectID != "" && event.ProjectID != projectID {
			return false
		}
		if eventType != "" && string(event.Type) != eventType {
			return false
		}
		return true
	}

	subscriber := eventBus.Subscribe(subscriberID, filter)
	defer eventBus.Unsubscribe(subscriberID)

	// Send initial connection event
	if _, err := fmt.Fprintf(w, "event: connected\n"); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "data: {\"message\": \"Connected to event stream\"}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	// Stream events to client
	ctx := r.Context()
	keepalive := time.NewTicker(10 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return
		case event, ok := <-subscriber.Channel:
			if !ok {
				// Channel closed
				return
			}

			// Send event to client
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			// Send keepalive ping
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleGetEvents handles GET requests for recent events
// GET /api/v1/events?project_id=xxx&type=xxx&limit=100
func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	eventBus := s.app.GetEventBus()
	if eventBus == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Event bus not available")
		return
	}

	projectID := r.URL.Query().Get("project_id")
	eventType := r.URL.Query().Get("type")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	events := eventBus.GetRecentEvents(limit, projectID, eventType)
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// handleGetEventStats returns statistics about events
// GET /api/v1/events/stats
func (s *Server) handleGetEventStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	eventBus := s.app.GetEventBus()
	if eventBus == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Event bus not available")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "active",
		"subscribers": eventBus.SubscriberCount(),
	})
}
