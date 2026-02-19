package database

import (
	"log"
)

// migrateWorkflows adds the workflow system tables
func (d *Database) migrateWorkflows() error {
	// Skip migrations for PostgreSQL (schema is complete in initSchemaPostgres)

	// Workflows table
	workflowsSchema := `
	CREATE TABLE IF NOT EXISTS workflows (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		workflow_type TEXT NOT NULL,
		is_default BOOLEAN NOT NULL DEFAULT 0,
		project_id TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_workflows_type ON workflows(workflow_type);
	CREATE INDEX IF NOT EXISTS idx_workflows_project_id ON workflows(project_id);
	CREATE INDEX IF NOT EXISTS idx_workflows_is_default ON workflows(is_default);
	`

	if _, err := d.db.Exec(workflowsSchema); err != nil {
		return err
	}

	// Workflow nodes table
	nodesSchema := `
	CREATE TABLE IF NOT EXISTS workflow_nodes (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		node_key TEXT NOT NULL,
		node_type TEXT NOT NULL,
		role_required TEXT,
		persona_hint TEXT,
		max_attempts INTEGER NOT NULL DEFAULT 0,
		timeout_minutes INTEGER NOT NULL DEFAULT 0,
		instructions TEXT,
		metadata_json TEXT,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE,
		UNIQUE(workflow_id, node_key)
	);

	CREATE INDEX IF NOT EXISTS idx_workflow_nodes_workflow_id ON workflow_nodes(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_nodes_node_key ON workflow_nodes(node_key);
	CREATE INDEX IF NOT EXISTS idx_workflow_nodes_role ON workflow_nodes(role_required);
	`

	if _, err := d.db.Exec(nodesSchema); err != nil {
		return err
	}

	// Workflow edges table
	edgesSchema := `
	CREATE TABLE IF NOT EXISTS workflow_edges (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		from_node_key TEXT,
		to_node_key TEXT,
		condition TEXT NOT NULL,
		priority INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_workflow_edges_workflow_id ON workflow_edges(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_edges_from_node ON workflow_edges(from_node_key);
	CREATE INDEX IF NOT EXISTS idx_workflow_edges_condition ON workflow_edges(condition);
	`

	if _, err := d.db.Exec(edgesSchema); err != nil {
		return err
	}

	// Workflow executions table
	executionsSchema := `
	CREATE TABLE IF NOT EXISTS workflow_executions (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		bead_id TEXT NOT NULL,
		project_id TEXT NOT NULL,
		current_node_key TEXT,
		status TEXT NOT NULL,
		cycle_count INTEGER NOT NULL DEFAULT 0,
		node_attempt_count INTEGER NOT NULL DEFAULT 0,
		started_at DATETIME NOT NULL,
		completed_at DATETIME,
		escalated_at DATETIME,
		last_node_at DATETIME NOT NULL,
		FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		UNIQUE(bead_id)
	);

	CREATE INDEX IF NOT EXISTS idx_workflow_executions_workflow_id ON workflow_executions(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_bead_id ON workflow_executions(bead_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_status ON workflow_executions(status);
	CREATE INDEX IF NOT EXISTS idx_workflow_executions_project_id ON workflow_executions(project_id);
	`

	if _, err := d.db.Exec(executionsSchema); err != nil {
		return err
	}

	// Workflow execution history table
	historySchema := `
	CREATE TABLE IF NOT EXISTS workflow_execution_history (
		id TEXT PRIMARY KEY,
		execution_id TEXT NOT NULL,
		node_key TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		condition TEXT NOT NULL,
		result_data TEXT,
		attempt_number INTEGER NOT NULL,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (execution_id) REFERENCES workflow_executions(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_workflow_history_execution_id ON workflow_execution_history(execution_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_history_node_key ON workflow_execution_history(node_key);
	CREATE INDEX IF NOT EXISTS idx_workflow_history_agent_id ON workflow_execution_history(agent_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_history_created_at ON workflow_execution_history(created_at);
	`

	if _, err := d.db.Exec(historySchema); err != nil {
		return err
	}

	log.Println("Workflow tables migrated successfully")
	return nil
}
