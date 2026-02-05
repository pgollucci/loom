package collaboration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContextStore(t *testing.T) {
	store := NewContextStore()
	assert.NotNil(t, store)
	defer store.Close()
}

func TestGetOrCreate(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	// Create new context
	beadCtx, err := store.GetOrCreate(ctx, "bead-1", "project-1")
	require.NoError(t, err)
	assert.Equal(t, "bead-1", beadCtx.BeadID)
	assert.Equal(t, "project-1", beadCtx.ProjectID)
	assert.Equal(t, int64(1), beadCtx.Version)
	assert.Empty(t, beadCtx.CollaboratingAgents)

	// Get existing context
	beadCtx2, err := store.GetOrCreate(ctx, "bead-1", "project-1")
	require.NoError(t, err)
	assert.Equal(t, beadCtx, beadCtx2) // Same instance
}

func TestJoinBead(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	beadCtx, _ := store.GetOrCreate(ctx, "bead-1", "project-1")

	// Agent 1 joins
	err := store.JoinBead(ctx, "bead-1", "agent-1")
	require.NoError(t, err)

	beadCtx.mu.RLock()
	assert.Contains(t, beadCtx.CollaboratingAgents, "agent-1")
	assert.Equal(t, int64(2), beadCtx.Version) // Version incremented
	assert.Len(t, beadCtx.ActivityLog, 1)
	assert.Equal(t, "joined", beadCtx.ActivityLog[0].ActivityType)
	beadCtx.mu.RUnlock()

	// Agent 2 joins
	err = store.JoinBead(ctx, "bead-1", "agent-2")
	require.NoError(t, err)

	beadCtx.mu.RLock()
	assert.Len(t, beadCtx.CollaboratingAgents, 2)
	assert.Contains(t, beadCtx.CollaboratingAgents, "agent-1")
	assert.Contains(t, beadCtx.CollaboratingAgents, "agent-2")
	beadCtx.mu.RUnlock()

	// Same agent joins again (should be idempotent)
	err = store.JoinBead(ctx, "bead-1", "agent-1")
	require.NoError(t, err)

	beadCtx.mu.RLock()
	assert.Len(t, beadCtx.CollaboratingAgents, 2) // Still 2 agents
	beadCtx.mu.RUnlock()
}

func TestLeaveBead(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")
	_ = store.JoinBead(ctx, "bead-1", "agent-1")
	_ = store.JoinBead(ctx, "bead-1", "agent-2")

	beadCtx, _ := store.Get(ctx, "bead-1")
	beadCtx.mu.RLock()
	assert.Len(t, beadCtx.CollaboratingAgents, 2)
	beadCtx.mu.RUnlock()

	// Agent 1 leaves
	err := store.LeaveBead(ctx, "bead-1", "agent-1")
	require.NoError(t, err)

	beadCtx.mu.RLock()
	assert.Len(t, beadCtx.CollaboratingAgents, 1)
	assert.Contains(t, beadCtx.CollaboratingAgents, "agent-2")
	assert.NotContains(t, beadCtx.CollaboratingAgents, "agent-1")

	// Check activity log
	found := false
	for _, entry := range beadCtx.ActivityLog {
		if entry.ActivityType == "left" && entry.AgentID == "agent-1" {
			found = true
			break
		}
	}
	assert.True(t, found, "Activity log should contain 'left' entry")
	beadCtx.mu.RUnlock()

	// Agent leaves again (should be idempotent)
	err = store.LeaveBead(ctx, "bead-1", "agent-1")
	require.NoError(t, err)
}

func TestUpdateData(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")
	_ = store.JoinBead(ctx, "bead-1", "agent-1")

	beadCtx, _ := store.Get(ctx, "bead-1")

	// Update data without version check
	err := store.UpdateData(ctx, "bead-1", "agent-1", "test_status", "passed", 0)
	require.NoError(t, err)

	beadCtx.mu.RLock()
	assert.Equal(t, "passed", beadCtx.Data["test_status"])
	version := beadCtx.Version
	beadCtx.mu.RUnlock()

	// Update with correct version
	err = store.UpdateData(ctx, "bead-1", "agent-1", "coverage", "85%", version)
	require.NoError(t, err)

	beadCtx.mu.RLock()
	assert.Equal(t, "85%", beadCtx.Data["coverage"])
	newVersion := beadCtx.Version
	beadCtx.mu.RUnlock()

	assert.Greater(t, newVersion, version)
}

func TestUpdateData_VersionConflict(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")
	_ = store.JoinBead(ctx, "bead-1", "agent-1")

	// Get initial version
	beadCtx, _ := store.Get(ctx, "bead-1")
	beadCtx.mu.RLock()
	initialVersion := beadCtx.Version
	beadCtx.mu.RUnlock()

	// Update without version check (version will change)
	_ = store.UpdateData(ctx, "bead-1", "agent-1", "key1", "value1", 0)

	// Try to update with old version (should fail)
	err := store.UpdateData(ctx, "bead-1", "agent-1", "key2", "value2", initialVersion)
	require.Error(t, err)

	conflictErr, ok := err.(*ConflictError)
	require.True(t, ok, "Error should be ConflictError")
	assert.Equal(t, "bead-1", conflictErr.BeadID)
	assert.Equal(t, initialVersion, conflictErr.ExpectedVersion)
	assert.Greater(t, conflictErr.ActualVersion, initialVersion)
}

func TestAddActivity(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")
	_ = store.JoinBead(ctx, "bead-1", "agent-1")

	// Add activity
	err := store.AddActivity(ctx, "bead-1", "agent-1", "file_modified",
		"Modified auth.go", map[string]interface{}{
			"file": "src/auth.go",
			"lines_changed": 25,
		})
	require.NoError(t, err)

	beadCtx, _ := store.Get(ctx, "bead-1")
	beadCtx.mu.RLock()
	defer beadCtx.mu.RUnlock()

	// Check activity log
	found := false
	for _, entry := range beadCtx.ActivityLog {
		if entry.ActivityType == "file_modified" &&
			entry.AgentID == "agent-1" &&
			entry.Description == "Modified auth.go" {
			assert.Equal(t, "src/auth.go", entry.Data["file"])
			assert.Equal(t, 25, entry.Data["lines_changed"])
			found = true
			break
		}
	}
	assert.True(t, found, "Activity log should contain file_modified entry")
}

func TestSubscribeAndNotify(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")

	// Subscribe to updates
	updateChan := store.Subscribe("bead-1")
	defer store.Unsubscribe("bead-1", updateChan)

	// Join bead (should trigger update)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = store.JoinBead(ctx, "bead-1", "agent-1")
	}()

	// Wait for update
	select {
	case update := <-updateChan:
		assert.Equal(t, "bead-1", update.BeadID)
		assert.Equal(t, "joined", update.UpdateType)
		assert.Equal(t, "agent-1", update.AgentID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update")
	}
}

func TestSubscribeMultipleUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")

	// Subscribe to updates
	updateChan := store.Subscribe("bead-1")
	defer store.Unsubscribe("bead-1", updateChan)

	// Trigger multiple updates
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = store.JoinBead(ctx, "bead-1", "agent-1")
		time.Sleep(50 * time.Millisecond)
		_ = store.UpdateData(ctx, "bead-1", "agent-1", "status", "running", 0)
		time.Sleep(50 * time.Millisecond)
		_ = store.AddActivity(ctx, "bead-1", "agent-1", "message", "Working on task", nil)
	}()

	// Collect updates
	updates := []ContextUpdate{}
	timeout := time.After(3 * time.Second)

	for len(updates) < 3 {
		select {
		case update := <-updateChan:
			updates = append(updates, update)
		case <-timeout:
			t.Fatalf("Timeout waiting for updates, got %d of 3", len(updates))
		}
	}

	// Verify update types
	assert.Equal(t, "joined", updates[0].UpdateType)
	assert.Equal(t, "data_changed", updates[1].UpdateType)
	assert.Equal(t, "activity", updates[2].UpdateType)
}

func TestExportContext(t *testing.T) {
	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")
	_ = store.JoinBead(ctx, "bead-1", "agent-1")
	_ = store.UpdateData(ctx, "bead-1", "agent-1", "status", "in_progress", 0)

	// Export context
	data, err := store.ExportContext(ctx, "bead-1")
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify it's valid JSON
	var exported SharedBeadContext
	err = json.Unmarshal(data, &exported)
	require.NoError(t, err)
	assert.Equal(t, "bead-1", exported.BeadID)
	assert.Equal(t, "project-1", exported.ProjectID)
	assert.Contains(t, exported.CollaboratingAgents, "agent-1")
	assert.Equal(t, "in_progress", exported.Data["status"])
}

func TestConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	store := NewContextStore()
	defer store.Close()

	ctx := context.Background()

	_, _ = store.GetOrCreate(ctx, "bead-1", "project-1")

	// Simulate multiple agents joining/leaving concurrently
	const numAgents = 10
	done := make(chan bool, numAgents)

	for i := 0; i < numAgents; i++ {
		agentID := fmt.Sprintf("agent-%d", i)
		go func(id string) {
			_ = store.JoinBead(ctx, "bead-1", id)
			_ = store.UpdateData(ctx, "bead-1", id, id+"_status", "active", 0)
			_ = store.AddActivity(ctx, "bead-1", id, "test", "Testing concurrent access", nil)
			_ = store.LeaveBead(ctx, "bead-1", id)
			done <- true
		}(agentID)
	}

	// Wait for all goroutines
	for i := 0; i < numAgents; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	// Verify context is consistent
	beadCtx, _ := store.Get(ctx, "bead-1")
	beadCtx.mu.RLock()
	defer beadCtx.mu.RUnlock()

	// All agents should have left
	assert.Empty(t, beadCtx.CollaboratingAgents)

	// Activity log should have entries
	assert.Greater(t, len(beadCtx.ActivityLog), numAgents)
}
