package database

// Migration to add dynamic scoring columns to providers table
func (d *Database) migrateProviderScoring() error {
	// Columns are already present in the PostgreSQL schema (initSchemaPostgres).
	return nil
}
