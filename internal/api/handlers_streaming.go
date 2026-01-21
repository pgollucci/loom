package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jordanhubbard/agenticorp/internal/provider"
)

// StreamChatCompletionRequest represents a request for streaming chat completion
type StreamChatCompletionRequest struct {
	ProviderID  string                 `json:"provider_id"`
	Model       string                 `json:"model,omitempty"`
	Messages    []provider.ChatMessage `json:"messages"`
	Temperature float64                `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
}

// handleStreamChatCompletion handles streaming chat completion requests
// POST /api/v1/chat/completions/stream
func (s *Server) handleStreamChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse request
	var req StreamChatCompletionRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ProviderID == "" || len(req.Messages) == 0 {
		s.respondError(w, http.StatusBadRequest, "provider_id and messages are required")
		return
	}

	// Get provider
	providerReg := s.agenticorp.GetProviderRegistry()
	if providerReg == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Provider registry not available")
		return
	}

	providerImpl, err := providerReg.Get(req.ProviderID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Provider not found: %s", req.ProviderID))
		return
	}

	// Check if provider supports streaming
	_, ok := providerImpl.Protocol.(provider.StreamingProtocol)
	if !ok {
		s.respondError(w, http.StatusBadRequest, "Provider does not support streaming")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\n")
	fmt.Fprintf(w, "data: {\"message\": \"Connected to stream\"}\n\n")
	flusher.Flush()

	// Create provider request
	providerReq := &provider.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// Stream response via registry
	err = providerReg.SendChatCompletionStream(ctx, req.ProviderID, providerReq, func(chunk *provider.StreamChunk) error {
		// Check if client disconnected
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Send chunk to client
		data, err := json.Marshal(chunk)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "event: chunk\n")
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		return nil
	})

	if err != nil {
		// Send error event
		errorData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "event: error\n")
		fmt.Fprintf(w, "data: %s\n\n", errorData)
		flusher.Flush()
		return
	}

	// Send completion event
	fmt.Fprintf(w, "event: done\n")
	fmt.Fprintf(w, "data: {\"message\": \"Stream complete\"}\n\n")
	flusher.Flush()
}

// handleChatCompletion handles non-streaming chat completion (with stream fallback)
// POST /api/v1/chat/completions
func (s *Server) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if streaming is requested
	if r.URL.Query().Get("stream") == "true" {
		s.handleStreamChatCompletion(w, r)
		return
	}

	// Parse request
	var req StreamChatCompletionRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ProviderID == "" || len(req.Messages) == 0 {
		s.respondError(w, http.StatusBadRequest, "provider_id and messages are required")
		return
	}

	// Get provider registry
	providerReg := s.agenticorp.GetProviderRegistry()
	if providerReg == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Provider registry not available")
		return
	}

	// Create provider request
	providerReq := &provider.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
	}

	// Send chat completion request
	resp, err := providerReg.SendChatCompletion(r.Context(), req.ProviderID, providerReq)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, resp)
}
