package database

// Migration to add routing metadata to providers table
func (d *Database) migrateProviderRouting() error {
	// Columns are already present in the PostgreSQL schema (initSchemaPostgres).
	return nil
}
