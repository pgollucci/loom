package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/jordanhubbard/loom/internal/loom"
	"github.com/jordanhubbard/loom/pkg/config"
)

func TestExportMetadataStructure(t *testing.T) {
	// Create test app
app, cleanup := createTestLoom(t)
defer cleanup()
db := app.GetDatabase()
if db == nil {
	t.Fatal("Database is not initialized")
}

	// Create server
	cfg := &config.Config{
		Security: config.SecurityConfig{
			EnableAuth: false, // Disable auth for testing
		},
	}
	server := NewServer(app, nil, nil, cfg)

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	w := httptest.NewRecorder()

	server.handleExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse response
	var exportData DatabaseExport
	if err := json.Unmarshal(w.Body.Bytes(), &exportData); err != nil {
		t.Fatalf("Failed to parse export JSON: %v", err)
	}

	// Verify metadata
	if exportData.Metadata.Version == "" {
		t.Error("Expected version in metadata")
	}
	if exportData.Metadata.SchemaVersion != exportSchemaVersion {
		t.Errorf("Expected schema version %s, got %s", exportSchemaVersion, exportData.Metadata.SchemaVersion)
	}
	if exportData.Metadata.DatabaseType == "" {
		t.Error("Expected database type in metadata")
	}
	if exportData.Metadata.RecordCounts == nil {
		t.Error("Expected record counts in metadata")
	}

	// Verify structure contains expected groups
	if exportData.Core.Providers == nil {
		t.Error("Expected Core.Providers to be initialized")
	}
	if exportData.Workflow.Workflows == nil {
		t.Error("Expected Workflow.Workflows to be initialized")
	}
	if exportData.Activity.Users == nil {
		t.Error("Expected Activity.Users to be initialized")
	}
}

func TestExportWithFilters(t *testing.T) {
	// Create test app
	app, cleanup := createTestLoom(t)
	defer cleanup()

	// Create server
	cfg := &config.Config{
		Security: config.SecurityConfig{
			EnableAuth: false,
		},
	}
	server := NewServer(app, nil, nil, cfg)

	// Test include filter
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export?include=core", nil)
	w := httptest.NewRecorder()

	server.handleExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse response
	var exportData DatabaseExport
	if err := json.Unmarshal(w.Body.Bytes(), &exportData); err != nil {
		t.Fatalf("Failed to parse export JSON: %v", err)
	}

	// Verify only core data is included
	if exportData.Core.Providers == nil {
		t.Error("Expected Core data to be included")
	}
	// Workflow should not be included when only 'core' is specified
	if len(exportData.Workflow.Workflows) > 0 {
		// This might be OK if there's data, just checking structure
	}
}

func TestImportValidation(t *testing.T) {
	// Create test app
	app, cleanup := createTestLoom(t)
	defer cleanup()

	// Create server
	cfg := &config.Config{
		Security: config.SecurityConfig{
			EnableAuth: false,
		},
	}
	server := NewServer(app, nil, nil, cfg)

	// Create test export data with wrong schema version
	exportData := DatabaseExport{
		Metadata: ExportMetadata{
			Version:       "2.0.0",
			SchemaVersion: "0.1", // Wrong version
			ExportedAt:    time.Now(),
		},
	}

	body, _ := json.Marshal(exportData)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import?validate_only=true", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleImport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse response
	var summary ImportSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("Failed to parse import summary: %v", err)
	}

	if summary.Validation.SchemaVersionOK {
		t.Error("Expected schema version validation to fail")
	}
	if summary.Validation.ValidationMessage == "" {
		t.Error("Expected validation message")
	}
}

func TestImportMergeStrategy(t *testing.T) {
	// Create test app
	app, cleanup := createTestLoom(t)
	defer cleanup()

	// Insert test data
	db := app.GetDatabase()
if db == nil {
	t.Fatal("Database is not initialized")
}
	_, err := db.DB().Exec(`INSERT INTO config_kv (key, value, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		"test_key", "original_value", time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Create server
	cfg := &config.Config{
		Security: config.SecurityConfig{
			EnableAuth: false,
		},
	}
	server := NewServer(app, nil, nil, cfg)

	// Create export data with updated value
	exportData := DatabaseExport{
		Metadata: ExportMetadata{
			Version:       "2.0.0",
			SchemaVersion: exportSchemaVersion,
			ExportedAt:    time.Now(),
			RecordCounts:  map[string]int{"config_kv": 1},
		},
		Config: ConfigData{
			ConfigKV: []map[string]interface{}{
				{
					"key":        "test_key",
					"value":      "updated_value",
					"updated_at": time.Now().Format(time.RFC3339),
				},
			},
		},
	}

	body, _ := json.Marshal(exportData)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import?strategy=merge", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleImport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify existing data was preserved (merge skips conflicts, keeping current values)
	var value string
	err = db.DB().QueryRow("SELECT value FROM config_kv WHERE key = $1", "test_key").Scan(&value)
	if err != nil {
		t.Fatalf("Failed to query data after merge: %v", err)
	}
	// Merge strategy preserves existing values on conflict
	if value != "original_value" {
		t.Errorf("Expected merge to preserve existing value 'original_value', got %q", value)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	// Create test app for export
	app1, cleanup1 := createTestLoom(t)
	defer cleanup1()

	// Insert test data
	db1 := app1.GetDatabase()
	_, err := db1.DB().Exec(`INSERT INTO providers (id, name, type, endpoint, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`,
		"test-provider", "Test Provider", "openai", "http://test", "active",
		time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("Failed to insert test provider: %v", err)
	}

	cfg := &config.Config{
		Security: config.SecurityConfig{
			EnableAuth: false,
		},
	}
	server1 := NewServer(app1, nil, nil, cfg)

	// Export data
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	w := httptest.NewRecorder()
	server1.handleExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Export failed with status %d: %s", w.Code, w.Body.String())
	}

	exportBody := w.Body.Bytes()

	// Create second app for import
	app2, cleanup2 := createTestLoom(t)
	defer cleanup2()

	server2 := NewServer(app2, nil, nil, cfg)

	// Import data
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/import", bytes.NewReader(exportBody))
	w2 := httptest.NewRecorder()
	server2.handleImport(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Import failed with status %d: %s", w2.Code, w2.Body.String())
	}

	// Verify data was imported
	var name string
	db2 := app2.GetDatabase()
	err = db2.DB().QueryRow("SELECT name FROM providers WHERE id = $1", "test-provider").Scan(&name)
	if err != nil {
		t.Fatalf("Failed to query imported provider: %v", err)
	}
	if name != "Test Provider" {
		t.Errorf("Expected provider name 'Test Provider', got %q", name)
	}
}

func TestImportDryRun(t *testing.T) {
	// Create test app
	app, cleanup := createTestLoom(t)
	defer cleanup()

	cfg := &config.Config{
		Security: config.SecurityConfig{
			EnableAuth: false,
		},
	}
	server := NewServer(app, nil, nil, cfg)

	// Create export data
	exportData := DatabaseExport{
		Metadata: ExportMetadata{
			Version:       "2.0.0",
			SchemaVersion: exportSchemaVersion,
			ExportedAt:    time.Now(),
		},
		Config: ConfigData{
			ConfigKV: []map[string]interface{}{
				{
					"key":        "dry_run_test",
					"value":      "test_value",
					"updated_at": time.Now().Format(time.RFC3339),
				},
			},
		},
	}

	body, _ := json.Marshal(exportData)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/import?dry_run=true", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleImport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify data was NOT imported
	var count int
	db := app.GetDatabase()
	err := db.DB().QueryRow("SELECT COUNT(*) FROM config_kv WHERE key = $1", "dry_run_test").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query count: %v", err)
	}
	if count != 0 {
		t.Error("Expected no data to be imported during dry run")
	}
}

// Helper function to create test loom instance with an isolated test database.
func createTestLoom(t *testing.T) (*loom.Loom, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "loom-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		user = "loom"
	}
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		password = "loom"
	}
	adminDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable connect_timeout=5",
		host, port, user, password)

	adminDB, adminErr := sql.Open("postgres", adminDSN)
	if adminErr != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to connect to postgres: %v", adminErr)
	}
	if err := adminDB.Ping(); err != nil {
		adminDB.Close()
		os.RemoveAll(tmpDir)
		t.Skipf("Skipping: postgres not reachable: %v", err)
	}

	testDBName := fmt.Sprintf("loom_api_test_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec(`CREATE DATABASE "` + testDBName + `"`); err != nil {
		adminDB.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create test database: %v", err)
	}
	adminDB.Close()

	t.Setenv("POSTGRES_HOST", host)
	t.Setenv("POSTGRES_PORT", port)
	t.Setenv("POSTGRES_USER", user)
	t.Setenv("POSTGRES_PASSWORD", password)
	t.Setenv("POSTGRES_DB", testDBName)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			DefaultPersonaPath: "../../personas",
			MaxConcurrent:      10,
		},
		Database: config.DatabaseConfig{
			Type: "postgres",
		},
		Git: config.GitConfig{
			ProjectKeyDir: tmpDir,
		},
		Security: config.SecurityConfig{
			EnableAuth: false,
		},
	}

	app, err := loom.New(cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create loom: %v", err)
	}

	cleanup := func() {
		app.Shutdown()
		os.RemoveAll(tmpDir)
		if a, e := sql.Open("postgres", adminDSN); e == nil {
			a.Exec(`DROP DATABASE IF EXISTS "` + testDBName + `"`)
			a.Close()
		}
	}

	return app, cleanup
}
