package database

import (
	"log"
)

// migratePerformanceReviews adds the performance_reviews table
func (d *Database) migratePerformanceReviews() error {
	// Skip migrations for PostgreSQL (schema is complete in initSchemaPostgres)

	// Performance reviews table
	schema := `
	CREATE TABLE IF NOT EXISTS performance_reviews (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		review_date TIMESTAMP NOT NULL,
		grade TEXT NOT NULL,
		score REAL NOT NULL,
		completion_pct REAL NOT NULL DEFAULT 0,
		efficiency_pct REAL NOT NULL DEFAULT 0,
		assist_pct REAL NOT NULL DEFAULT 0,
		action TEXT,
		notes TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_performance_reviews_agent_id ON performance_reviews(agent_id);
	CREATE INDEX IF NOT EXISTS idx_performance_reviews_review_date ON performance_reviews(review_date);
	CREATE INDEX IF NOT EXISTS idx_performance_reviews_grade ON performance_reviews(grade);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	log.Println("Performance reviews table migrated successfully")
	return nil
}
