package database

import (
	"log"
)

// migrateMeetings adds the meeting_summaries table
func (d *Database) migrateMeetings() error {
	// Skip migrations for PostgreSQL (schema is complete in initSchemaPostgres)

	// Meeting summaries table
	schema := `
	CREATE TABLE IF NOT EXISTS meeting_summaries (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		title TEXT NOT NULL,
		participants_json TEXT NOT NULL,
		summary TEXT NOT NULL,
		action_item_bead_ids_json TEXT,
		created_at TIMESTAMP NOT NULL,
		completed_at TIMESTAMP,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_meeting_summaries_project_id ON meeting_summaries(project_id);
	CREATE INDEX IF NOT EXISTS idx_meeting_summaries_created_at ON meeting_summaries(created_at);
	CREATE INDEX IF NOT EXISTS idx_meeting_summaries_completed_at ON meeting_summaries(completed_at);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	log.Println("Meeting summaries table migrated successfully")
	return nil
}
