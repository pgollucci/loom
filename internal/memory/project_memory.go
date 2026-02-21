package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryCategory classifies a project memory entry.
type MemoryCategory string

const (
	MemCategoryConvention  MemoryCategory = "convention"   // commit format, code style
	MemCategoryBuildSystem MemoryCategory = "build_system" // build/test/lint commands
	MemCategoryGitHub      MemoryCategory = "github"       // repo URL, default branch, issue templates
	MemCategoryFailure     MemoryCategory = "failure"      // known flaky tests, broken paths
	MemCategoryLesson      MemoryCategory = "lesson"       // general lessons learned
)

// ProjectMemory is a structured key-value entry scoped to a project.
// Multiple entries form a knowledge base that agents can query.
type ProjectMemory struct {
	ProjectID  string         `json:"project_id"`
	Category   MemoryCategory `json:"category"`
	Key        string         `json:"key"`        // e.g. "build_command", "repo_url"
	Value      string         `json:"value"`      // plain text or markdown
	Confidence float64        `json:"confidence"` // 0-1; increases with repeated confirmation
	UpdatedAt  time.Time      `json:"updated_at"`
	SourceBead string         `json:"source_bead,omitempty"` // bead that produced this entry
}

// MemoryStore persists and retrieves ProjectMemory entries.
// The concrete implementation lives in internal/database/project_memory.go.
type MemoryStore interface {
	UpsertMemory(ctx context.Context, m *ProjectMemory) error
	GetMemory(ctx context.Context, projectID string, category MemoryCategory, key string) (*ProjectMemory, error)
	ListMemory(ctx context.Context, projectID string) ([]*ProjectMemory, error)
	ListMemoryByCategory(ctx context.Context, projectID string, category MemoryCategory) ([]*ProjectMemory, error)
	DeleteMemory(ctx context.Context, projectID string, category MemoryCategory, key string) error
}

// MemoryManager provides high-level operations on project memory.
type MemoryManager struct {
	store MemoryStore
}

// NewMemoryManager creates a MemoryManager backed by store.
func NewMemoryManager(store MemoryStore) *MemoryManager {
	return &MemoryManager{store: store}
}

// Set upserts a memory entry with confidence 1.0 if not specified.
func (m *MemoryManager) Set(ctx context.Context, mem *ProjectMemory) error {
	if mem.Confidence == 0 {
		mem.Confidence = 1.0
	}
	if mem.UpdatedAt.IsZero() {
		mem.UpdatedAt = time.Now().UTC()
	}
	return m.store.UpsertMemory(ctx, mem)
}

// Get retrieves a single memory entry. Returns nil, nil if not found.
func (m *MemoryManager) Get(ctx context.Context, projectID string, category MemoryCategory, key string) (*ProjectMemory, error) {
	return m.store.GetMemory(ctx, projectID, category, key)
}

// GetByCategory returns all memory entries for a project in a category.
func (m *MemoryManager) GetByCategory(ctx context.Context, projectID string, category MemoryCategory) ([]*ProjectMemory, error) {
	return m.store.ListMemoryByCategory(ctx, projectID, category)
}

// All returns all memory entries for a project.
func (m *MemoryManager) All(ctx context.Context, projectID string) ([]*ProjectMemory, error) {
	return m.store.ListMemory(ctx, projectID)
}

// Delete removes a memory entry.
func (m *MemoryManager) Delete(ctx context.Context, projectID string, category MemoryCategory, key string) error {
	return m.store.DeleteMemory(ctx, projectID, category, key)
}

// BuildContextSummary generates a markdown block summarising the project's
// accumulated memory for injection into agent system prompts.
func (m *MemoryManager) BuildContextSummary(ctx context.Context, projectID string) (string, error) {
	entries, err := m.store.ListMemory(ctx, projectID)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}

	// Group by category.
	grouped := make(map[MemoryCategory][]*ProjectMemory)
	for _, e := range entries {
		grouped[e.Category] = append(grouped[e.Category], e)
	}

	var sb strings.Builder
	sb.WriteString("## Project Context (auto-learned)\n")

	order := []MemoryCategory{
		MemCategoryBuildSystem,
		MemCategoryGitHub,
		MemCategoryConvention,
		MemCategoryFailure,
		MemCategoryLesson,
	}
	for _, cat := range order {
		items, ok := grouped[cat]
		if !ok {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n### %s\n", cat))
		for _, item := range items {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", item.Key, item.Value))
		}
	}

	return sb.String(), nil
}
