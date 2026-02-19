package database

import (
	"log"
)

// migrateComments creates the bead comments and mentions tables
func (d *Database) migrateComments() error {
	// Skip migrations for PostgreSQL (schema is complete in initSchemaPostgres)

	// Bead comments table
	commentsSchema := `
	CREATE TABLE IF NOT EXISTS bead_comments (
		id TEXT PRIMARY KEY,
		bead_id TEXT NOT NULL,
		parent_id TEXT,
		author_id TEXT NOT NULL,
		author_username TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		edited BOOLEAN NOT NULL DEFAULT false,
		deleted BOOLEAN NOT NULL DEFAULT false,
		FOREIGN KEY (parent_id) REFERENCES bead_comments(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_bead_comments_bead_id ON bead_comments(bead_id);
	CREATE INDEX IF NOT EXISTS idx_bead_comments_parent_id ON bead_comments(parent_id);
	CREATE INDEX IF NOT EXISTS idx_bead_comments_created_at ON bead_comments(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_bead_comments_author ON bead_comments(author_id);
	`

	if _, err := d.db.Exec(commentsSchema); err != nil {
		return err
	}

	// Comment mentions table
	mentionsSchema := `
	CREATE TABLE IF NOT EXISTS comment_mentions (
		id TEXT PRIMARY KEY,
		comment_id TEXT NOT NULL,
		mentioned_user_id TEXT NOT NULL,
		mentioned_username TEXT NOT NULL,
		notified_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL,
		FOREIGN KEY (comment_id) REFERENCES bead_comments(id) ON DELETE CASCADE,
		FOREIGN KEY (mentioned_user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_comment_mentions_comment_id ON comment_mentions(comment_id);
	CREATE INDEX IF NOT EXISTS idx_comment_mentions_user_id ON comment_mentions(mentioned_user_id);
	CREATE INDEX IF NOT EXISTS idx_comment_mentions_notified ON comment_mentions(notified_at);
	`

	if _, err := d.db.Exec(mentionsSchema); err != nil {
		return err
	}

	log.Println("Comment tables migrated successfully")
	return nil
}
