package database

import "fmt"

// migrateProviderAPIKey adds an api_key column to the providers table so that
// API keys survive loom restarts instead of being lost when the in-memory
// registry is cleared.
func (d *Database) migrateProviderAPIKey() error {
	_, err := d.db.Exec(`ALTER TABLE providers ADD COLUMN IF NOT EXISTS api_key TEXT`)
	if err != nil {
		return fmt.Errorf("migrateProviderAPIKey: %w", err)
	}
	return nil
}
