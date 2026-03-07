package database

import (
	"log"
)

// migrateFeedback creates the feedback tables
func (d *Database) migrateFeedback() error {
	// Feedback table
	feedbackSchema := `
	CREATE TABLE IF NOT EXISTS feedback (
		id TEXT PRIMARY KEY,
		bead_id TEXT,
		agent_id TEXT,
		author_id TEXT NOT NULL,
		author TEXT NOT NULL,
		rating INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
		category TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata JSONB,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_feedback_bead_id ON feedback(bead_id);
	CREATE INDEX IF NOT EXISTS idx_feedback_agent_id ON feedback(agent_id);
	CREATE INDEX IF NOT EXISTS idx_feedback_author_id ON feedback(author_id);
	CREATE INDEX IF NOT EXISTS idx_feedback_created_at ON feedback(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(rating);
	CREATE INDEX IF NOT EXISTS idx_feedback_category ON feedback(category);
	`

	if _, err := d.db.Exec(feedbackSchema); err != nil {
		return err
	}

	log.Println("Feedback tables migrated successfully")
	return nil
}
