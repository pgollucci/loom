package database

// Migration to add owner_id and is_shared to providers table
func (d *Database) migrateProviderOwnership() error {
	// Columns are already present in the PostgreSQL schema (initSchemaPostgres).
	return nil
}
