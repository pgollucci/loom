package taskexecutor

import (
	"fmt"
	"strings"

	"github.com/jordanhubbard/loom/internal/provider"
	"github.com/jordanhubbard/loom/pkg/models"
)

// ModelHint provides guidance to the worker about which model to use.
// It allows the executor to recommend a model based on task complexity,
// but the worker can override it if needed.
type ModelHint struct {
	RecommendedModel string // e.g., "gpt-4-turbo", "claude-3-opus"
	Reason           string // Why this model was chosen
	ComplexityScore  int    // 1-10 scale: 1=simple, 10=very complex
}

// selectModel analyzes the bead and returns a ModelHint recommending
// the best model for this task. Returns nil if no recommendation is needed.
func (e *Executor) selectModel(bead *models.Bead, availableProviders []*provider.RegisteredProvider) *ModelHint {
	if len(availableProviders) == 0 {
		return nil
	}

	// Analyze task complexity from bead properties
	complexityScore := analyzeComplexity(bead)

	// For simple tasks (score 1-3), use the fastest/cheapest model
	if complexityScore <= 3 {
		for _, prov := range availableProviders {
			if isSimpleModel(prov.Config.Model) {
				return &ModelHint{
					RecommendedModel: prov.Config.Model,
					Reason:           fmt.Sprintf("Simple task (complexity %d): using fast/cheap model", complexityScore),
					ComplexityScore:  complexityScore,
				}
			}
		}
	}

	// For medium tasks (score 4-6), use a balanced model
	if complexityScore <= 6 {
		for _, prov := range availableProviders {
			if isBalancedModel(prov.Config.Model) {
				return &ModelHint{
					RecommendedModel: prov.Config.Model,
					Reason:           fmt.Sprintf("Medium task (complexity %d): using balanced model", complexityScore),
					ComplexityScore:  complexityScore,
				}
			}
		}
	}

	// For complex tasks (score 7-10), use the most capable model
	for _, prov := range availableProviders {
		if isCapableModel(prov.Config.Model) {
			return &ModelHint{
				RecommendedModel: prov.Config.Model,
				Reason:           fmt.Sprintf("Complex task (complexity %d): using most capable model", complexityScore),
				ComplexityScore:  complexityScore,
			}
		}
	}

	// Fallback: use first available provider
	if len(availableProviders) > 0 {
		return &ModelHint{
			RecommendedModel: availableProviders[0].Config.Model,
			Reason:           "Using default provider (no specific model recommendation)",
			ComplexityScore:  complexityScore,
		}
	}

	return nil
}

// analyzeComplexity scores the task complexity from 1-10 based on bead properties.
func analyzeComplexity(bead *models.Bead) int {
	score := 3 // Default: medium-simple

	// Decision beads are complex (require reasoning)
	if bead.Type == "decision" {
		score += 3
	}

	// Epic/large tasks are complex
	if bead.Type == "epic" {
		score += 2
	}

	// High priority tasks may be complex
	if bead.Priority == models.BeadPriorityP0 || bead.Priority == models.BeadPriorityP1 {
		score += 1
	}

	// Check description length (longer = more complex)
	descLen := len(bead.Description)
	if descLen > 1000 {
		score += 2
	} else if descLen > 500 {
		score += 1
	}

	// Check for complexity keywords in description
	descLower := strings.ToLower(bead.Description)
	complexityKeywords := []string{
		"architecture", "design", "refactor", "optimize", "performance",
		"security", "compliance", "integration", "migration", "complex",
		"algorithm", "analysis", "investigation", "research", "prototype",
	}
	for _, keyword := range complexityKeywords {
		if strings.Contains(descLower, keyword) {
			score += 1
			break // Only count once
		}
	}

	// Check for complexity keywords in tags
	for _, tag := range bead.Tags {
		tagLower := strings.ToLower(tag)
		if tagLower == "complex" || tagLower == "research" || tagLower == "design" {
			score += 1
			break
		}
	}

	// Cap at 10
	if score > 10 {
		score = 10
	}

	return score
}

// isSimpleModel returns true if the model is fast/cheap (good for simple tasks).
func isSimpleModel(modelName string) bool {
	modelLower := strings.ToLower(modelName)
	simpleModels := []string{
		"gpt-3.5", "gpt-4o-mini", "claude-3-haiku", "llama-2-7b",
		"mistral-7b", "phi-2", "neural-chat",
	}
	for _, m := range simpleModels {
		if strings.Contains(modelLower, m) {
			return true
		}
	}
	return false
}

// isBalancedModel returns true if the model is balanced (good for medium tasks).
func isBalancedModel(modelName string) bool {
	modelLower := strings.ToLower(modelName)
	balancedModels := []string{
		"gpt-4", "gpt-4-turbo", "claude-3-sonnet", "llama-2-70b",
		"mistral-large", "neural-chat-7b",
	}
	for _, m := range balancedModels {
		if strings.Contains(modelLower, m) {
			return true
		}
	}
	return false
}

// isCapableModel returns true if the model is highly capable (good for complex tasks).
func isCapableModel(modelName string) bool {
	modelLower := strings.ToLower(modelName)
	capableModels := []string{
		"gpt-4-turbo", "gpt-4-32k", "claude-3-opus", "claude-opus",
		"llama-2-70b", "mistral-large", "yi-34b",
	}
	for _, m := range capableModels {
		if strings.Contains(modelLower, m) {
			return true
		}
	}
	return false
}
