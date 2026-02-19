package database

import "fmt"

// Migration to add dynamic scoring columns to providers table.
// Uses ADD COLUMN IF NOT EXISTS so it is safe to run on databases that already
// have these columns from a previous deployment.
func (d *Database) migrateProviderScoring() error {
	alterations := []string{
		`ALTER TABLE providers ADD COLUMN IF NOT EXISTS model_params_b REAL`,
		`ALTER TABLE providers ADD COLUMN IF NOT EXISTS capability_score REAL`,
		`ALTER TABLE providers ADD COLUMN IF NOT EXISTS avg_latency_ms INTEGER`,
	}
	for _, sql := range alterations {
		if _, err := d.db.Exec(sql); err != nil {
			return fmt.Errorf("migrateProviderScoring: %w", err)
		}
	}
	return nil
}
