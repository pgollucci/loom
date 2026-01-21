package provider

import (
	"context"
	"fmt"
	"time"
)

// CreateChatCompletionStream implements streaming for MockProvider
// Simulates streaming by sending the response in chunks
func (p *MockProvider) CreateChatCompletionStream(ctx context.Context, req *ChatCompletionRequest, handler StreamHandler) error {
	// Build mock response content
	content := "mock response"
	if len(req.Messages) > 0 {
		content = req.Messages[len(req.Messages)-1].Content
		if content == "" {
			content = "mock response"
		}
	}

	fullContent := "[mock streaming] " + content

	// Simulate streaming by sending content word-by-word
	words := []rune(fullContent)
	chunkSize := 5 // Characters per chunk

	for i := 0; i < len(words); i += chunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}

		chunkContent := string(words[i:end])

		// Create chunk
		chunk := &StreamChunk{
			ID:      fmt.Sprintf("mock-stream-%d", i),
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   req.Model,
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
						Content: chunkContent,
					},
				},
			},
		}

		// Add role to first chunk
		if i == 0 {
			chunk.Choices[0].Delta.Role = "assistant"
		}

		// Add finish reason to last chunk
		if end >= len(words) {
			chunk.Choices[0].FinishReason = "stop"
		}

		// Call handler
		if err := handler(chunk); err != nil {
			return fmt.Errorf("handler error: %w", err)
		}

		// Simulate network delay
		time.Sleep(50 * time.Millisecond)
	}

	return nil
}
