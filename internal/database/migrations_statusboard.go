package database

import (
	"log"
)

// migrateStatusBoard adds the status_board_entries table
func (d *Database) migrateStatusBoard() error {
	// Skip migrations for PostgreSQL (schema is complete in initSchemaPostgres)

	// Status board entries table
	schema := `
	CREATE TABLE IF NOT EXISTS status_board_entries (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		author_agent_id TEXT NOT NULL,
		content TEXT NOT NULL,
		category TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_status_board_entries_project_id ON status_board_entries(project_id);
	CREATE INDEX IF NOT EXISTS idx_status_board_entries_author_agent_id ON status_board_entries(author_agent_id);
	CREATE INDEX IF NOT EXISTS idx_status_board_entries_category ON status_board_entries(category);
	CREATE INDEX IF NOT EXISTS idx_status_board_entries_created_at ON status_board_entries(created_at);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	log.Println("Status board entries table migrated successfully")
	return nil
}
