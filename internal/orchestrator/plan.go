package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/provider"
	"github.com/jordanhubbard/loom/pkg/messages"
)

// LLMPlanner uses an LLM provider to generate structured plans.
type LLMPlanner struct {
	provider provider.Protocol
	model    string
}

// NewLLMPlanner creates a planner that consults an LLM to produce plans.
func NewLLMPlanner(endpoint, apiKey, model string) *LLMPlanner {
	return &LLMPlanner{
		provider: provider.NewOpenAIProvider(endpoint, apiKey),
		model:    model,
	}
}

const plannerSystemPrompt = `You are a technical project planner for a multi-agent software team.

Given a task or issue, produce a structured plan that assigns work to specialized agent roles.

Available roles:
- "coder": Implements code changes, fixes bugs, adds features
- "reviewer": Reviews code changes for correctness, style, security
- "qa": Runs builds and tests, validates changes
- "pm": Manages project scope, priorities, and documentation
- "architect": Makes high-level design decisions

Each step should specify:
- A unique step_id (e.g., "step-1", "step-2")
- A role (from the list above)
- An action ("implement", "review", "test", "plan", "document")
- A description of what to do
- Dependencies on other step_ids (depends_on array)

For coding tasks, always include a review step after implementation and a QA step after review.

Respond with valid JSON matching this schema:
{
  "title": "string",
  "description": "string",
  "steps": [
    {
      "step_id": "string",
      "role": "string",
      "action": "string",
      "description": "string",
      "depends_on": ["string"]
    }
  ],
  "priority": number
}`

// GeneratePlan consults the LLM to produce a structured plan for a bead.
func (p *LLMPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (*messages.PlanData, error) {
	prompt := fmt.Sprintf("Task: %s\n\nDescription: %s", req.Title, req.Description)
	if len(req.Context) > 0 {
		ctxJSON, _ := json.Marshal(req.Context)
		prompt += fmt.Sprintf("\n\nAdditional context: %s", string(ctxJSON))
	}

	resp, err := p.provider.CreateChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model: p.model,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: plannerSystemPrompt},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM planning request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from planner LLM")
	}

	text := resp.Choices[0].Message.Content
	plan, err := parsePlanResponse(text)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	// Assign step IDs if missing
	for i := range plan.Steps {
		if plan.Steps[i].StepID == "" {
			plan.Steps[i].StepID = fmt.Sprintf("step-%d", i+1)
		}
		if plan.Steps[i].Status == "" {
			plan.Steps[i].Status = "pending"
		}
	}

	log.Printf("[Planner] Generated plan '%s' with %d steps", plan.Title, len(plan.Steps))
	return plan, nil
}

func parsePlanResponse(text string) (*messages.PlanData, error) {
	text = strings.TrimSpace(text)

	var plan messages.PlanData
	if err := json.Unmarshal([]byte(text), &plan); err == nil {
		return &plan, nil
	}

	// Try extracting JSON from markdown code block
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		end := strings.Index(text[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(text[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &plan); err == nil {
				return &plan, nil
			}
		}
	}

	// Try extracting any JSON object
	if idx := strings.Index(text, "{"); idx >= 0 {
		jsonStr := text[idx:]
		depth := 0
		for i, ch := range jsonStr {
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := jsonStr[:i+1]
					if err := json.Unmarshal([]byte(candidate), &plan); err == nil {
						return &plan, nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("no valid plan JSON found")
}

// StaticPlanner produces a fixed plan for testing or simple tasks.
type StaticPlanner struct{}

// GeneratePlan returns a standard implement -> review -> QA plan.
func (p *StaticPlanner) GeneratePlan(_ context.Context, req PlanRequest) (*messages.PlanData, error) {
	return &messages.PlanData{
		Title:       req.Title,
		Description: req.Description,
		Priority:    1,
		Steps: []messages.PlanStep{
			{
				StepID:      uuid.New().String()[:8],
				Role:        "coder",
				Action:      "implement",
				Description: req.Description,
				Status:      "pending",
			},
			{
				StepID:      uuid.New().String()[:8],
				Role:        "reviewer",
				Action:      "review",
				Description: fmt.Sprintf("Review implementation of: %s", req.Title),
				DependsOn:   []string{}, // Will be wired after creation
				Status:      "pending",
			},
			{
				StepID:      uuid.New().String()[:8],
				Role:        "qa",
				Action:      "test",
				Description: fmt.Sprintf("Build and test: %s", req.Title),
				DependsOn:   []string{}, // Will be wired after creation
				Status:      "pending",
			},
		},
	}, nil
}
