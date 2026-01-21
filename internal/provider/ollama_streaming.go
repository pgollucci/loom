package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CreateChatCompletionStream implements streaming for Ollama provider
func (p *OllamaProvider) CreateChatCompletionStream(ctx context.Context, req *ChatCompletionRequest, handler StreamHandler) error {
	url := fmt.Sprintf("%s/api/chat", p.endpoint)

	if req.Model == "" {
		return fmt.Errorf("model is required")
	}

	// Build Ollama request with streaming enabled
	ollamaReq := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream  bool `json:"stream"`
		Options struct {
			Temperature float64 `json:"temperature,omitempty"`
		} `json:"options,omitempty"`
	}{
		Model:  req.Model,
		Stream: true, // Enable streaming
	}
	ollamaReq.Options.Temperature = req.Temperature

	for _, msg := range req.Messages {
		ollamaReq.Messages = append(ollamaReq.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: msg.Role, Content: msg.Content})
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
	}

	// Read streaming response (Ollama uses newline-delimited JSON, not SSE)
	return p.readOllamaStream(ctx, resp.Body, handler)
}

// readOllamaStream reads Ollama's streaming format (newline-delimited JSON)
func (p *OllamaProvider) readOllamaStream(ctx context.Context, reader io.Reader, handler StreamHandler) error {
	scanner := bufio.NewScanner(reader)
	chunkIndex := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse Ollama chunk
		var ollamaChunk struct {
			Model   string `json:"model"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}

		if err := json.Unmarshal(line, &ollamaChunk); err != nil {
			// Log error but continue
			continue
		}

		// Convert to OpenAI-compatible StreamChunk format
		chunk := &StreamChunk{
			ID:      fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   ollamaChunk.Model,
			Choices: []struct {
				Index int `json:"index"`
				Delta struct {
					Role    string `json:"role,omitempty"`
					Content string `json:"content,omitempty"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason,omitempty"`
			}{
				{
					Index: 0,
					Delta: struct {
						Role    string `json:"role,omitempty"`
						Content string `json:"content,omitempty"`
					}{
						Role:    ollamaChunk.Message.Role,
						Content: ollamaChunk.Message.Content,
					},
				},
			},
		}

		// Set finish reason on last chunk
		if ollamaChunk.Done {
			chunk.Choices[0].FinishReason = "stop"
		}

		// Call handler
		if err := handler(chunk); err != nil {
			return fmt.Errorf("handler error: %w", err)
		}

		// Stop if done
		if ollamaChunk.Done {
			return nil
		}

		chunkIndex++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}
