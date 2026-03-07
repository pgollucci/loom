package database

import (
	"log"
)

// migrateOrgChartAssignments adds the org_chart_assignments table
func (d *Database) migrateOrgChartAssignments() error {
	// Skip migrations for PostgreSQL (schema is complete in initSchemaPostgres)

	// Org chart assignments table
	schema := `
	CREATE TABLE IF NOT EXISTS org_chart_assignments (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		position_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		assigned_at TIMESTAMP NOT NULL,
		unassigned_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_org_chart_assignments_project_id ON org_chart_assignments(project_id);
	CREATE INDEX IF NOT EXISTS idx_org_chart_assignments_position_id ON org_chart_assignments(position_id);
	CREATE INDEX IF NOT EXISTS idx_org_chart_assignments_agent_id ON org_chart_assignments(agent_id);
	CREATE INDEX IF NOT EXISTS idx_org_chart_assignments_assigned_at ON org_chart_assignments(assigned_at);
	`

	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	log.Println("Org chart assignments table migrated successfully")
	return nil
}
