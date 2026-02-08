package dispatch

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/database"
	"github.com/jordanhubbard/loom/pkg/models"
)

// LessonsProvider retrieves and records lessons from the database.
// It implements the worker.LessonsProvider interface.
type LessonsProvider struct {
	db *database.Database
}

// NewLessonsProvider creates a new LessonsProvider backed by the given database.
func NewLessonsProvider(db *database.Database) *LessonsProvider {
	if db == nil {
		return nil
	}
	return &LessonsProvider{db: db}
}

// GetLessonsForPrompt retrieves lessons for a project and formats them as markdown
// suitable for injection into the system prompt.
func (lp *LessonsProvider) GetLessonsForPrompt(projectID string) string {
	if lp == nil || lp.db == nil || projectID == "" {
		return ""
	}

	lessons, err := lp.db.GetLessonsForProject(projectID, 15, 4000)
	if err != nil {
		log.Printf("[LessonsProvider] Failed to get lessons for project %s: %v", projectID, err)
		return ""
	}

	if len(lessons) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("The following lessons were learned from previous work on this project.\n")
	sb.WriteString("Avoid repeating these mistakes:\n\n")

	for _, l := range lessons {
		sb.WriteString(fmt.Sprintf("### %s: %s\n", strings.ToUpper(l.Category), l.Title))
		sb.WriteString(fmt.Sprintf("- %s\n", l.Detail))
		if l.RelevanceScore < 0.3 {
			sb.WriteString("- (older lesson, may be less relevant)\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// RecordLesson creates a new lesson from observed agent behavior.
func (lp *LessonsProvider) RecordLesson(projectID, category, title, detail, beadID, agentID string) error {
	if lp == nil || lp.db == nil {
		return nil
	}

	lesson := &models.Lesson{
		ID:             uuid.New().String(),
		ProjectID:      projectID,
		Category:       category,
		Title:          title,
		Detail:         detail,
		SourceBeadID:   beadID,
		SourceAgentID:  agentID,
		CreatedAt:      time.Now(),
		RelevanceScore: 1.0,
	}

	if err := lp.db.CreateLesson(lesson); err != nil {
		log.Printf("[LessonsProvider] Failed to record lesson: %v", err)
		return err
	}

	log.Printf("[LessonsProvider] Recorded lesson: [%s] %s", category, title)
	return nil
}
