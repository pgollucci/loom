package database

import (
	"fmt"
	"math"
	"time"

	"github.com/jordanhubbard/loom/pkg/models"
)

// migrateLessons creates the lessons table if it doesn't exist.
func (d *Database) migrateLessons() error {
	schema := `
	CREATE TABLE IF NOT EXISTS lessons (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		category TEXT NOT NULL,
		title TEXT NOT NULL,
		detail TEXT NOT NULL,
		source_bead_id TEXT,
		source_agent_id TEXT,
		relevance_score REAL NOT NULL DEFAULT 1.0,
		created_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_lessons_project ON lessons(project_id);
	CREATE INDEX IF NOT EXISTS idx_lessons_category ON lessons(category);
	`
	_, err := d.db.Exec(schema)
	return err
}

// CreateLesson inserts a new lesson record.
func (d *Database) CreateLesson(lesson *models.Lesson) error {
	if lesson == nil {
		return fmt.Errorf("lesson cannot be nil")
	}
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = time.Now()
	}
	if lesson.RelevanceScore == 0 {
		lesson.RelevanceScore = 1.0
	}

	_, err := d.db.Exec(`
		INSERT INTO lessons (id, project_id, category, title, detail, source_bead_id, source_agent_id, relevance_score, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lesson.ID, lesson.ProjectID, lesson.Category, lesson.Title, lesson.Detail,
		lesson.SourceBeadID, lesson.SourceAgentID, lesson.RelevanceScore, lesson.CreatedAt,
	)
	return err
}

// GetLessonsForProject retrieves recent lessons for a project, up to limit count
// and maxChars total detail characters. Lessons are scored with time decay.
func (d *Database) GetLessonsForProject(projectID string, limit int, maxChars int) ([]*models.Lesson, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := d.db.Query(`
		SELECT id, project_id, category, title, detail, source_bead_id, source_agent_id, relevance_score, created_at
		FROM lessons
		WHERE project_id = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lessons []*models.Lesson
	totalChars := 0
	now := time.Now()

	for rows.Next() {
		l := &models.Lesson{}
		err := rows.Scan(&l.ID, &l.ProjectID, &l.Category, &l.Title, &l.Detail,
			&l.SourceBeadID, &l.SourceAgentID, &l.RelevanceScore, &l.CreatedAt)
		if err != nil {
			return lessons, err
		}

		// Apply time decay: halve relevance every 7 days
		ageDays := now.Sub(l.CreatedAt).Hours() / 24
		l.RelevanceScore = l.RelevanceScore * math.Pow(0.5, ageDays/7.0)

		totalChars += len(l.Detail)
		if maxChars > 0 && totalChars > maxChars {
			break
		}

		lessons = append(lessons, l)
	}

	return lessons, rows.Err()
}
