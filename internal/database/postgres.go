package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

// rebind converts ? placeholders to $1, $2, ... for PostgreSQL.
// This is used throughout the database package for parameterized queries.
func rebind(query string) string {
	n := 1
	out := strings.Builder{}
	for _, ch := range query {
		if ch == '?' {
			out.WriteString(fmt.Sprintf("$%d", n))
			n++
		} else {
			out.WriteRune(ch)
		}
	}
	return out.String()
}

// NewPostgres creates a PostgreSQL database connection.
func NewPostgres(dsn string) (*Database, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	defer func() {
		if err != nil {
			db.Close()
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	d := &Database{
		db:         db,
		supportsHA: true,
	}

	// Initialize schema
	if err := d.initSchemaPostgres(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Run migrations
	if err := d.migrateProviderOwnership(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate provider ownership: %w", err)
	}

	if err := d.migrateProviderRouting(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate provider routing: %w", err)
	}

	return d, nil
}

// initSchemaPostgres creates PostgreSQL-specific tables.
func (d *Database) initSchemaPostgres() error {
	schema := `
	-- Global configuration key-value store
	CREATE TABLE IF NOT EXISTS config_kv (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Distributed locks table for HA
	CREATE TABLE IF NOT EXISTS distributed_locks (
		lock_name TEXT PRIMARY KEY,
		instance_id TEXT NOT NULL,
		acquired_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL,
		heartbeat_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Instance registry for tracking active instances
	CREATE TABLE IF NOT EXISTS instances (
		instance_id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_heartbeat TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'active',
		metadata JSONB
	);

	-- Global providers (shared across all projects)
	CREATE TABLE IF NOT EXISTS providers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		model TEXT,
		configured_model TEXT,
		selected_model TEXT,
		selection_reason TEXT,
		model_score REAL,
		selected_gpu TEXT,
		gpu_constraints_json TEXT,
		description TEXT,
		requires_key BOOLEAN NOT NULL DEFAULT false,
		key_id TEXT,
		owner_id TEXT,
		is_shared BOOLEAN NOT NULL DEFAULT true,
		status TEXT NOT NULL DEFAULT 'active',
		last_heartbeat_at TIMESTAMP,
		last_heartbeat_latency_ms INTEGER,
		last_heartbeat_error TEXT,
		metrics_json TEXT,
		schema_version TEXT NOT NULL DEFAULT '1.0',
		attributes_json TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		cost_per_mtoken REAL,
		context_window INTEGER,
		model_params_b REAL,
		capability_score REAL,
		avg_latency_ms INTEGER,
		supports_function BOOLEAN DEFAULT false,
		supports_vision BOOLEAN DEFAULT false,
		supports_streaming BOOLEAN DEFAULT false,
		tags TEXT[]
	);

	-- Request logs for analytics
	CREATE TABLE IF NOT EXISTS request_logs (
		id SERIAL PRIMARY KEY,
		timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		user_id TEXT,
		provider_id TEXT,
		model TEXT,
		endpoint TEXT,
		method TEXT,
		status_code INTEGER,
		latency_ms INTEGER,
		prompt_tokens INTEGER,
		completion_tokens INTEGER,
		total_tokens INTEGER,
		cost_usd REAL,
		error_message TEXT,
		request_body_hash TEXT,
		ip_address TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Projects with hierarchy support (parent_id for sub-projects)
	CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		git_repo TEXT NOT NULL,
		branch TEXT NOT NULL,
		beads_path TEXT NOT NULL,
		parent_id TEXT,
		is_perpetual BOOLEAN NOT NULL DEFAULT false,
		is_sticky BOOLEAN NOT NULL DEFAULT false,
		git_strategy TEXT NOT NULL DEFAULT 'direct',
		git_auth_method TEXT NOT NULL DEFAULT 'none',
		status TEXT NOT NULL DEFAULT 'open',
		context_json TEXT,
		schema_version TEXT NOT NULL DEFAULT '1.0',
		attributes_json TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		closed_at TIMESTAMP,
		FOREIGN KEY (parent_id) REFERENCES projects(id) ON DELETE SET NULL
	);

	-- Org charts define the team structure for each project
	CREATE TABLE IF NOT EXISTS org_charts (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		name TEXT NOT NULL,
		is_template BOOLEAN NOT NULL DEFAULT false,
		parent_id TEXT,
		schema_version TEXT NOT NULL DEFAULT '1.0',
		attributes_json TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (parent_id) REFERENCES org_charts(id) ON DELETE SET NULL
	);

	-- Positions within an org chart (role slots)
	CREATE TABLE IF NOT EXISTS org_chart_positions (
		id TEXT PRIMARY KEY,
		org_chart_id TEXT NOT NULL,
		role_name TEXT NOT NULL,
		persona_path TEXT NOT NULL,
		required BOOLEAN NOT NULL DEFAULT false,
		max_instances INTEGER NOT NULL DEFAULT 0,
		reports_to TEXT,
		schema_version TEXT NOT NULL DEFAULT '1.0',
		attributes_json TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (org_chart_id) REFERENCES org_charts(id) ON DELETE CASCADE,
		FOREIGN KEY (reports_to) REFERENCES org_chart_positions(id) ON DELETE SET NULL
	);

	-- Agent instances assigned to positions
	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		role TEXT,
		persona_name TEXT,
		provider_id TEXT,
		status TEXT NOT NULL DEFAULT 'idle',
		current_bead TEXT,
		project_id TEXT,
		position_id TEXT,
		schema_version TEXT NOT NULL DEFAULT '1.0',
		attributes_json TEXT,
		started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_active TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL,
		FOREIGN KEY (position_id) REFERENCES org_chart_positions(id) ON DELETE SET NULL,
		FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE SET NULL
	);

	-- Command Logs for agent shell command execution
	CREATE TABLE IF NOT EXISTS command_logs (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		bead_id TEXT,
		project_id TEXT,
		command TEXT NOT NULL,
		working_dir TEXT NOT NULL,
		exit_code INTEGER NOT NULL,
		stdout TEXT,
		stderr TEXT,
		duration_ms INTEGER NOT NULL,
		started_at TIMESTAMP NOT NULL,
		completed_at TIMESTAMP NOT NULL,
		context TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_request_logs_timestamp ON request_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_request_logs_user_id ON request_logs(user_id);
	CREATE INDEX IF NOT EXISTS idx_request_logs_provider_id ON request_logs(provider_id);
	CREATE INDEX IF NOT EXISTS idx_distributed_locks_expires_at ON distributed_locks(expires_at);
	CREATE INDEX IF NOT EXISTS idx_instances_last_heartbeat ON instances(last_heartbeat);
	CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
	CREATE INDEX IF NOT EXISTS idx_agents_project_id ON agents(project_id);
	CREATE INDEX IF NOT EXISTS idx_agents_position_id ON agents(position_id);
	CREATE INDEX IF NOT EXISTS idx_providers_status ON providers(status);
	CREATE INDEX IF NOT EXISTS idx_projects_parent_id ON projects(parent_id);
	CREATE INDEX IF NOT EXISTS idx_org_charts_project_id ON org_charts(project_id);
	CREATE INDEX IF NOT EXISTS idx_positions_org_chart_id ON org_chart_positions(org_chart_id);
	CREATE INDEX IF NOT EXISTS idx_command_logs_agent_id ON command_logs(agent_id);
	CREATE INDEX IF NOT EXISTS idx_command_logs_bead_id ON command_logs(bead_id);
	CREATE INDEX IF NOT EXISTS idx_command_logs_project_id ON command_logs(project_id);
	CREATE INDEX IF NOT EXISTS idx_command_logs_created_at ON command_logs(created_at);
	`

	_, err := d.db.Exec(schema)
	return err
}
