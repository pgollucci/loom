package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/internal/auth"
)

const (
	version             = "2.0.0"
	exportSchemaVersion = "1.0"
	maxImportSize       = 200 << 20 // 200MB
	rateLimit           = 5         // exports per hour
)

// ExportMetadata contains information about the exported data
type ExportMetadata struct {
	Version         string         `json:"version"`
	SchemaVersion   string         `json:"schema_version"`
	ExportedAt      time.Time      `json:"exported_at"`
	ServerVersion   string         `json:"server_version"`
	DatabaseType    string         `json:"database_type"`
	EncryptionKeyID string         `json:"encryption_key_id"`
	RecordCounts    map[string]int `json:"record_counts"`
}

// DatabaseExport represents the complete database export structure
type DatabaseExport struct {
	Metadata  ExportMetadata `json:"export_metadata"`
	Core      CoreData       `json:"core"`
	Workflow  WorkflowData   `json:"workflow"`
	Activity  ActivityData   `json:"activity"`
	Tracking  TrackingData   `json:"tracking"`
	Logging   LoggingData    `json:"logging"`
	Analytics AnalyticsData  `json:"analytics"`
	Config    ConfigData     `json:"config"`
}

// CoreData holds core system data
type CoreData struct {
	Providers         []map[string]interface{} `json:"providers"`
	Projects          []map[string]interface{} `json:"projects"`
	OrgCharts         []map[string]interface{} `json:"org_charts"`
	OrgChartPositions []map[string]interface{} `json:"org_chart_positions"`
	Agents            []map[string]interface{} `json:"agents"`
	Credentials       []map[string]interface{} `json:"credentials"`
}

// WorkflowData holds workflow-related data
type WorkflowData struct {
	Workflows                []map[string]interface{} `json:"workflows"`
	WorkflowNodes            []map[string]interface{} `json:"workflow_nodes"`
	WorkflowEdges            []map[string]interface{} `json:"workflow_edges"`
	WorkflowExecutions       []map[string]interface{} `json:"workflow_executions"`
	WorkflowExecutionHistory []map[string]interface{} `json:"workflow_execution_history"`
}

// ActivityData holds activity and notification data
type ActivityData struct {
	Users                   []map[string]interface{} `json:"users"`
	ActivityFeed            []map[string]interface{} `json:"activity_feed"`
	Notifications           []map[string]interface{} `json:"notifications"`
	NotificationPreferences []map[string]interface{} `json:"notification_preferences"`
	BeadComments            []map[string]interface{} `json:"bead_comments"`
	CommentMentions         []map[string]interface{} `json:"comment_mentions"`
	ConversationContexts    []map[string]interface{} `json:"conversation_contexts"`
}

// TrackingData holds motivation and milestone tracking data
type TrackingData struct {
	Motivations        []map[string]interface{} `json:"motivations"`
	MotivationTriggers []map[string]interface{} `json:"motivation_triggers"`
	Milestones         []map[string]interface{} `json:"milestones"`
	Lessons            []map[string]interface{} `json:"lessons"`
}

// LoggingData holds logging data
type LoggingData struct {
	Logs        []map[string]interface{} `json:"logs"`
	RequestLogs []map[string]interface{} `json:"request_logs"`
	CommandLogs []map[string]interface{} `json:"command_logs"`
}

// AnalyticsData holds analytics data
type AnalyticsData struct {
	UsagePatterns []map[string]interface{} `json:"usage_patterns"`
	Optimizations []map[string]interface{} `json:"optimizations"`
}

// ConfigData holds configuration data
type ConfigData struct {
	ConfigKV []map[string]interface{} `json:"config_kv"`
}

// ImportSummary holds the result of an import operation
type ImportSummary struct {
	Status     string                       `json:"status"`
	ImportedAt time.Time                    `json:"imported_at"`
	Validation ValidationResult             `json:"validation"`
	Summary    map[string]TableImportResult `json:"summary"`
	Errors     []string                     `json:"errors,omitempty"`
}

// ValidationResult holds validation results
type ValidationResult struct {
	SchemaVersionOK   bool   `json:"schema_version_ok"`
	EncryptionKeyOK   bool   `json:"encryption_key_ok"`
	ValidationMessage string `json:"validation_message,omitempty"`
}

// TableImportResult holds statistics for a single table import
type TableImportResult struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

// handleExport handles GET /api/v1/export
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check admin authentication
	if s.config.Security.EnableAuth {
		userID := auth.GetUserIDFromRequest(r)
		if userID == "" {
			http.Error(w, "Unauthorized - admin access required", http.StatusUnauthorized)
			return
		}
		// TODO: Add proper admin role check when role system is implemented
	}

	// Parse query parameters
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	includeStr := r.URL.Query().Get("include")
	excludeStr := r.URL.Query().Get("exclude")
	projectID := r.URL.Query().Get("project_id")
	sinceStr := r.URL.Query().Get("since")

	var since *time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid since parameter: %v", err), http.StatusBadRequest)
			return
		}
		since = &t
	}

	// Build include/exclude filters
	var includes, excludes map[string]bool
	if includeStr != "" {
		includes = make(map[string]bool)
		for _, s := range strings.Split(includeStr, ",") {
			includes[strings.TrimSpace(s)] = true
		}
	}
	if excludeStr != "" {
		excludes = make(map[string]bool)
		for _, s := range strings.Split(excludeStr, ",") {
			excludes[strings.TrimSpace(s)] = true
		}
	}

	// Check if group is included
	shouldInclude := func(group string) bool {
		if excludes != nil && excludes[group] {
			return false
		}
		if includes != nil {
			return includes[group]
		}
		return true
	}

	db := s.app.GetDatabase()
	if db == nil {
		http.Error(w, "Database not available", http.StatusServiceUnavailable)
		return
	}

	// Build export data
	exportData := DatabaseExport{
		Metadata: ExportMetadata{
			Version:         version,
			SchemaVersion:   exportSchemaVersion,
			ExportedAt:      time.Now().UTC(),
			ServerVersion:   version,
			DatabaseType:    db.Type(),
			EncryptionKeyID: s.getEncryptionKeyID(),
			RecordCounts:    make(map[string]int),
		},
	}

	// Export each group
	if shouldInclude("core") {
		core, counts, err := s.exportCore(db, projectID, since)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export core data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Core = core
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	if shouldInclude("workflow") {
		workflow, counts, err := s.exportWorkflow(db, projectID, since)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export workflow data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Workflow = workflow
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	if shouldInclude("activity") {
		activity, counts, err := s.exportActivity(db, since)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export activity data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Activity = activity
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	if shouldInclude("tracking") {
		tracking, counts, err := s.exportTracking(db, projectID, since)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export tracking data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Tracking = tracking
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	if shouldInclude("logging") {
		logging, counts, err := s.exportLogging(db, since)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export logging data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Logging = logging
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	if shouldInclude("analytics") {
		analytics, counts, err := s.exportAnalytics(db, since)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export analytics data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Analytics = analytics
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	if shouldInclude("config") {
		config, counts, err := s.exportConfig(db)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to export config data: %v", err), http.StatusInternalServerError)
			return
		}
		exportData.Config = config
		for k, v := range counts {
			exportData.Metadata.RecordCounts[k] = v
		}
	}

	// Set headers
	timestamp := time.Now().UTC().Format("2006-01-02")
	filename := fmt.Sprintf("loom-export-%s.json", timestamp)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	// Stream JSON response
	encoder := json.NewEncoder(w)
	if format == "json-pretty" {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(exportData); err != nil {
		// Can't send error response since we already started writing
		fmt.Fprintf(w, "\n{\"error\": \"Failed to encode export data: %v\"}\n", err)
	}
}

// handleImport handles POST /api/v1/import
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check admin authentication
	if s.config.Security.EnableAuth {
		userID := auth.GetUserIDFromRequest(r)
		if userID == "" {
			http.Error(w, "Unauthorized - admin access required", http.StatusUnauthorized)
			return
		}
		// TODO: Add proper admin role check when role system is implemented
	}

	// Parse query parameters
	strategy := r.URL.Query().Get("strategy")
	if strategy == "" {
		strategy = "merge"
	}
	dryRun := r.URL.Query().Get("dry_run") == "true"
	validateOnly := r.URL.Query().Get("validate_only") == "true"

	// Validate strategy
	validStrategies := map[string]bool{
		"merge":            true,
		"replace":          true,
		"fail-on-conflict": true,
	}
	if !validStrategies[strategy] {
		http.Error(w, fmt.Sprintf("Invalid strategy: %s. Valid options: merge, replace, fail-on-conflict", strategy), http.StatusBadRequest)
		return
	}

	// Read request body with size limit
	limitedReader := io.LimitReader(r.Body, maxImportSize)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Parse JSON
	var exportData DatabaseExport
	if err := json.Unmarshal(bodyBytes, &exportData); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate schema version
	validation := ValidationResult{
		SchemaVersionOK: exportData.Metadata.SchemaVersion == exportSchemaVersion,
		EncryptionKeyOK: true, // TODO: Implement encryption key validation
	}

	if !validation.SchemaVersionOK {
		validation.ValidationMessage = fmt.Sprintf("Schema version mismatch: export is %s, server expects %s",
			exportData.Metadata.SchemaVersion, exportSchemaVersion)
	}

	if validateOnly || !validation.SchemaVersionOK {
		s.respondJSON(w, http.StatusOK, ImportSummary{
			Status:     "validation_only",
			ImportedAt: time.Now().UTC(),
			Validation: validation,
		})
		return
	}

	// Perform import
	db := s.app.GetDatabase()
	if db == nil {
		http.Error(w, "Database not available", http.StatusServiceUnavailable)
		return
	}

	summary := ImportSummary{
		Status:     "completed",
		ImportedAt: time.Now().UTC(),
		Validation: validation,
		Summary:    make(map[string]TableImportResult),
		Errors:     []string{},
	}

	if !dryRun {
		// Start transaction
		tx, err := db.DB().Begin()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to start transaction: %v", err), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Import in dependency order
		if err := s.importData(tx, &exportData, strategy, &summary); err != nil {
			summary.Status = "failed"
			summary.Errors = append(summary.Errors, err.Error())
			s.respondJSON(w, http.StatusInternalServerError, summary)
			return
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			summary.Status = "failed"
			summary.Errors = append(summary.Errors, fmt.Sprintf("Failed to commit transaction: %v", err))
			s.respondJSON(w, http.StatusInternalServerError, summary)
			return
		}
	} else {
		summary.Status = "dry_run"
	}

	s.respondJSON(w, http.StatusOK, summary)
}

// Helper functions for exporting different data groups

func (s *Server) exportCore(db interface{ DB() *sql.DB }, projectID string, since *time.Time) (CoreData, map[string]int, error) {
	core := CoreData{}
	counts := make(map[string]int)

	// Export providers
	providers, err := s.queryTable(db.DB(), "providers", "", projectID, since)
	if err != nil {
		return core, counts, fmt.Errorf("providers: %w", err)
	}
	core.Providers = providers
	counts["providers"] = len(providers)

	// Export projects
	projects, err := s.queryTable(db.DB(), "projects", "", projectID, since)
	if err != nil {
		return core, counts, fmt.Errorf("projects: %w", err)
	}
	core.Projects = projects
	counts["projects"] = len(projects)

	// Export org_charts
	orgCharts, err := s.queryTable(db.DB(), "org_charts", "project_id", projectID, since)
	if err != nil {
		return core, counts, fmt.Errorf("org_charts: %w", err)
	}
	core.OrgCharts = orgCharts
	counts["org_charts"] = len(orgCharts)

	// Export org_chart_positions
	orgChartPositions, err := s.queryTable(db.DB(), "org_chart_positions", "", "", since)
	if err != nil {
		return core, counts, fmt.Errorf("org_chart_positions: %w", err)
	}
	core.OrgChartPositions = orgChartPositions
	counts["org_chart_positions"] = len(orgChartPositions)

	// Export agents
	agents, err := s.queryTable(db.DB(), "agents", "project_id", projectID, since)
	if err != nil {
		return core, counts, fmt.Errorf("agents: %w", err)
	}
	core.Agents = agents
	counts["agents"] = len(agents)

	// Export credentials
	credentials, err := s.queryTable(db.DB(), "credentials", "project_id", projectID, since)
	if err != nil {
		return core, counts, fmt.Errorf("credentials: %w", err)
	}
	core.Credentials = credentials
	counts["credentials"] = len(credentials)

	return core, counts, nil
}

func (s *Server) exportWorkflow(db interface{ DB() *sql.DB }, projectID string, since *time.Time) (WorkflowData, map[string]int, error) {
	workflow := WorkflowData{}
	counts := make(map[string]int)

	workflows, err := s.queryTable(db.DB(), "workflows", "project_id", projectID, since)
	if err != nil {
		return workflow, counts, fmt.Errorf("workflows: %w", err)
	}
	workflow.Workflows = workflows
	counts["workflows"] = len(workflows)

	workflowNodes, err := s.queryTable(db.DB(), "workflow_nodes", "", "", since)
	if err != nil {
		return workflow, counts, fmt.Errorf("workflow_nodes: %w", err)
	}
	workflow.WorkflowNodes = workflowNodes
	counts["workflow_nodes"] = len(workflowNodes)

	workflowEdges, err := s.queryTable(db.DB(), "workflow_edges", "", "", since)
	if err != nil {
		return workflow, counts, fmt.Errorf("workflow_edges: %w", err)
	}
	workflow.WorkflowEdges = workflowEdges
	counts["workflow_edges"] = len(workflowEdges)

	workflowExecutions, err := s.queryTable(db.DB(), "workflow_executions", "project_id", projectID, since)
	if err != nil {
		return workflow, counts, fmt.Errorf("workflow_executions: %w", err)
	}
	workflow.WorkflowExecutions = workflowExecutions
	counts["workflow_executions"] = len(workflowExecutions)

	workflowExecutionHistory, err := s.queryTable(db.DB(), "workflow_execution_history", "", "", since)
	if err != nil {
		return workflow, counts, fmt.Errorf("workflow_execution_history: %w", err)
	}
	workflow.WorkflowExecutionHistory = workflowExecutionHistory
	counts["workflow_execution_history"] = len(workflowExecutionHistory)

	return workflow, counts, nil
}

func (s *Server) exportActivity(db interface{ DB() *sql.DB }, since *time.Time) (ActivityData, map[string]int, error) {
	activity := ActivityData{}
	counts := make(map[string]int)

	users, err := s.queryTable(db.DB(), "users", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("users: %w", err)
	}
	activity.Users = users
	counts["users"] = len(users)

	activityFeed, err := s.queryTable(db.DB(), "activity_feed", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("activity_feed: %w", err)
	}
	activity.ActivityFeed = activityFeed
	counts["activity_feed"] = len(activityFeed)

	notifications, err := s.queryTable(db.DB(), "notifications", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("notifications: %w", err)
	}
	activity.Notifications = notifications
	counts["notifications"] = len(notifications)

	notificationPreferences, err := s.queryTable(db.DB(), "notification_preferences", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("notification_preferences: %w", err)
	}
	activity.NotificationPreferences = notificationPreferences
	counts["notification_preferences"] = len(notificationPreferences)

	beadComments, err := s.queryTable(db.DB(), "bead_comments", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("bead_comments: %w", err)
	}
	activity.BeadComments = beadComments
	counts["bead_comments"] = len(beadComments)

	commentMentions, err := s.queryTable(db.DB(), "comment_mentions", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("comment_mentions: %w", err)
	}
	activity.CommentMentions = commentMentions
	counts["comment_mentions"] = len(commentMentions)

	conversationContexts, err := s.queryTable(db.DB(), "conversation_contexts", "", "", since)
	if err != nil {
		return activity, counts, fmt.Errorf("conversation_contexts: %w", err)
	}
	activity.ConversationContexts = conversationContexts
	counts["conversation_contexts"] = len(conversationContexts)

	return activity, counts, nil
}

func (s *Server) exportTracking(db interface{ DB() *sql.DB }, projectID string, since *time.Time) (TrackingData, map[string]int, error) {
	tracking := TrackingData{}
	counts := make(map[string]int)

	motivations, err := s.queryTable(db.DB(), "motivations", "project_id", projectID, since)
	if err != nil {
		return tracking, counts, fmt.Errorf("motivations: %w", err)
	}
	tracking.Motivations = motivations
	counts["motivations"] = len(motivations)

	motivationTriggers, err := s.queryTable(db.DB(), "motivation_triggers", "", "", since)
	if err != nil {
		return tracking, counts, fmt.Errorf("motivation_triggers: %w", err)
	}
	tracking.MotivationTriggers = motivationTriggers
	counts["motivation_triggers"] = len(motivationTriggers)

	milestones, err := s.queryTable(db.DB(), "milestones", "project_id", projectID, since)
	if err != nil {
		return tracking, counts, fmt.Errorf("milestones: %w", err)
	}
	tracking.Milestones = milestones
	counts["milestones"] = len(milestones)

	lessons, err := s.queryTable(db.DB(), "lessons", "project_id", projectID, since)
	if err != nil {
		return tracking, counts, fmt.Errorf("lessons: %w", err)
	}
	tracking.Lessons = lessons
	counts["lessons"] = len(lessons)

	return tracking, counts, nil
}

func (s *Server) exportLogging(db interface{ DB() *sql.DB }, since *time.Time) (LoggingData, map[string]int, error) {
	logging := LoggingData{}
	counts := make(map[string]int)

	logs, err := s.queryTable(db.DB(), "logs", "", "", since)
	if err != nil {
		return logging, counts, fmt.Errorf("logs: %w", err)
	}
	logging.Logs = logs
	counts["logs"] = len(logs)

	requestLogs, err := s.queryTable(db.DB(), "request_logs", "", "", since)
	if err != nil {
		return logging, counts, fmt.Errorf("request_logs: %w", err)
	}
	logging.RequestLogs = requestLogs
	counts["request_logs"] = len(requestLogs)

	commandLogs, err := s.queryTable(db.DB(), "command_logs", "", "", since)
	if err != nil {
		return logging, counts, fmt.Errorf("command_logs: %w", err)
	}
	logging.CommandLogs = commandLogs
	counts["command_logs"] = len(commandLogs)

	return logging, counts, nil
}

func (s *Server) exportAnalytics(db interface{ DB() *sql.DB }, since *time.Time) (AnalyticsData, map[string]int, error) {
	analytics := AnalyticsData{}
	counts := make(map[string]int)

	usagePatterns, err := s.queryTable(db.DB(), "usage_patterns", "", "", since)
	if err != nil {
		return analytics, counts, fmt.Errorf("usage_patterns: %w", err)
	}
	analytics.UsagePatterns = usagePatterns
	counts["usage_patterns"] = len(usagePatterns)

	optimizations, err := s.queryTable(db.DB(), "optimizations", "", "", since)
	if err != nil {
		return analytics, counts, fmt.Errorf("optimizations: %w", err)
	}
	analytics.Optimizations = optimizations
	counts["optimizations"] = len(optimizations)

	return analytics, counts, nil
}

func (s *Server) exportConfig(db interface{ DB() *sql.DB }) (ConfigData, map[string]int, error) {
	config := ConfigData{}
	counts := make(map[string]int)

	configKV, err := s.queryTable(db.DB(), "config_kv", "", "", nil)
	if err != nil {
		return config, counts, fmt.Errorf("config_kv: %w", err)
	}
	config.ConfigKV = configKV
	counts["config_kv"] = len(configKV)

	return config, counts, nil
}

// queryTable queries a table and returns rows as maps
func (s *Server) queryTable(db *sql.DB, tableName, projectIDColumn, projectID string, since *time.Time) ([]map[string]interface{}, error) {
	// Build query with filters
	query := fmt.Sprintf("SELECT * FROM %s", tableName)
	args := []interface{}{}
	conditions := []string{}

	if projectID != "" && projectIDColumn != "" {
		conditions = append(conditions, fmt.Sprintf("%s = ?", projectIDColumn))
		args = append(args, projectID)
	}

	if since != nil {
		// Try common timestamp column names
		for _, col := range []string{"created_at", "updated_at", "timestamp"} {
			conditions = append(conditions, fmt.Sprintf("%s >= ?", col))
			args = append(args, since.Format(time.RFC3339))
			break // Only use first matching column
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		// Table might not exist, return empty array
		if strings.Contains(err.Error(), "no such table") {
			return []map[string]interface{}{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Scan rows
	results := []map[string]interface{}{}
	for rows.Next() {
		// Create a slice of interface{} to hold each column
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		// Create map for this row
		rowMap := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string
			if b, ok := val.([]byte); ok {
				rowMap[col] = string(b)
			} else {
				rowMap[col] = val
			}
		}
		results = append(results, rowMap)
	}

	return results, rows.Err()
}

// importData imports all data groups
func (s *Server) importData(tx *sql.Tx, exportData *DatabaseExport, strategy string, summary *ImportSummary) error {
	// Import config first (no dependencies)
	if err := s.importConfig(tx, &exportData.Config, strategy, summary); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Import core data
	if err := s.importCore(tx, &exportData.Core, strategy, summary); err != nil {
		return fmt.Errorf("core: %w", err)
	}

	// Import workflow data
	if err := s.importWorkflow(tx, &exportData.Workflow, strategy, summary); err != nil {
		return fmt.Errorf("workflow: %w", err)
	}

	// Import activity data
	if err := s.importActivity(tx, &exportData.Activity, strategy, summary); err != nil {
		return fmt.Errorf("activity: %w", err)
	}

	// Import tracking data
	if err := s.importTracking(tx, &exportData.Tracking, strategy, summary); err != nil {
		return fmt.Errorf("tracking: %w", err)
	}

	// Import logging data
	if err := s.importLogging(tx, &exportData.Logging, strategy, summary); err != nil {
		return fmt.Errorf("logging: %w", err)
	}

	// Import analytics data
	if err := s.importAnalytics(tx, &exportData.Analytics, strategy, summary); err != nil {
		return fmt.Errorf("analytics: %w", err)
	}

	return nil
}

func (s *Server) importConfig(tx *sql.Tx, config *ConfigData, strategy string, summary *ImportSummary) error {
	return s.importTableData(tx, "config_kv", config.ConfigKV, strategy, summary)
}

func (s *Server) importCore(tx *sql.Tx, core *CoreData, strategy string, summary *ImportSummary) error {
	// Import in dependency order
	if err := s.importTableData(tx, "providers", core.Providers, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "projects", core.Projects, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "org_charts", core.OrgCharts, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "org_chart_positions", core.OrgChartPositions, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "agents", core.Agents, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "credentials", core.Credentials, strategy, summary); err != nil {
		return err
	}
	return nil
}

func (s *Server) importWorkflow(tx *sql.Tx, workflow *WorkflowData, strategy string, summary *ImportSummary) error {
	if err := s.importTableData(tx, "workflows", workflow.Workflows, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "workflow_nodes", workflow.WorkflowNodes, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "workflow_edges", workflow.WorkflowEdges, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "workflow_executions", workflow.WorkflowExecutions, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "workflow_execution_history", workflow.WorkflowExecutionHistory, strategy, summary); err != nil {
		return err
	}
	return nil
}

func (s *Server) importActivity(tx *sql.Tx, activity *ActivityData, strategy string, summary *ImportSummary) error {
	if err := s.importTableData(tx, "users", activity.Users, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "activity_feed", activity.ActivityFeed, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "notifications", activity.Notifications, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "notification_preferences", activity.NotificationPreferences, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "bead_comments", activity.BeadComments, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "comment_mentions", activity.CommentMentions, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "conversation_contexts", activity.ConversationContexts, strategy, summary); err != nil {
		return err
	}
	return nil
}

func (s *Server) importTracking(tx *sql.Tx, tracking *TrackingData, strategy string, summary *ImportSummary) error {
	if err := s.importTableData(tx, "motivations", tracking.Motivations, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "motivation_triggers", tracking.MotivationTriggers, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "milestones", tracking.Milestones, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "lessons", tracking.Lessons, strategy, summary); err != nil {
		return err
	}
	return nil
}

func (s *Server) importLogging(tx *sql.Tx, logging *LoggingData, strategy string, summary *ImportSummary) error {
	if err := s.importTableData(tx, "logs", logging.Logs, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "request_logs", logging.RequestLogs, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "command_logs", logging.CommandLogs, strategy, summary); err != nil {
		return err
	}
	return nil
}

func (s *Server) importAnalytics(tx *sql.Tx, analytics *AnalyticsData, strategy string, summary *ImportSummary) error {
	if err := s.importTableData(tx, "usage_patterns", analytics.UsagePatterns, strategy, summary); err != nil {
		return err
	}
	if err := s.importTableData(tx, "optimizations", analytics.Optimizations, strategy, summary); err != nil {
		return err
	}
	return nil
}

// importTableData imports data for a single table
func (s *Server) importTableData(tx *sql.Tx, tableName string, rows []map[string]interface{}, strategy string, summary *ImportSummary) error {
	if len(rows) == 0 {
		return nil
	}

	result := TableImportResult{}

	// Get column names from first row
	columns := []string{}
	for col := range rows[0] {
		columns = append(columns, col)
	}

	// Build PostgreSQL $N placeholders
	phParts := make([]string, len(columns))
	for i := range columns {
		phParts[i] = fmt.Sprintf("$%d", i+1)
	}
	placeholders := strings.Join(phParts, ",")

	// Build INSERT statement based on strategy
	var query string
	if strategy == "merge" {
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
			tableName, strings.Join(columns, ","), placeholders)
	} else if strategy == "fail-on-conflict" {
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			tableName, strings.Join(columns, ","), placeholders)
	} else if strategy == "replace" {
		// Delete all existing data first
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", tableName)); err != nil {
			return fmt.Errorf("failed to clear table %s: %w", tableName, err)
		}
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			tableName, strings.Join(columns, ","), placeholders)
	}

	// Batch insert rows
	for _, row := range rows {
		values := make([]interface{}, len(columns))
		for i, col := range columns {
			values[i] = row[col]
		}

		_, err := tx.Exec(query, values...)
		if err != nil {
			if strategy == "fail-on-conflict" {
				result.Failed++
				summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", tableName, err))
			} else {
				result.Failed++
			}
		} else {
			if strategy == "merge" {
				result.Updated++
			} else {
				result.Inserted++
			}
		}
	}

	summary.Summary[tableName] = result
	return nil
}

// getEncryptionKeyID returns the current encryption key ID
func (s *Server) getEncryptionKeyID() string {
	if s.keyManager == nil {
		return ""
	}
	return "master-key-v1" // TODO: Get actual key ID from keyManager
}
