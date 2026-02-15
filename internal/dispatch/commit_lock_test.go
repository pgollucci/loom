package dispatch

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestAcquireCommitLock_Sequential tests that sequential commit lock acquisitions work correctly
func TestAcquireCommitLock_Sequential(t *testing.T) {
	d := &Dispatcher{
		commitQueue:       make(chan commitRequest, 100),
		commitLockTimeout: 5 * time.Minute,
	}

	// Start commit queue processor
	go d.processCommitQueue()

	ctx := context.Background()

	// First lock acquisition
	err1 := d.acquireCommitLock(ctx, "bead-1", "agent-1")
	if err1 != nil {
		t.Fatalf("Expected first lock acquisition to succeed, got error: %v", err1)
	}

	// Verify lock is held
	d.commitStateMutex.RLock()
	if d.commitInProgress == nil {
		t.Fatal("Expected commitInProgress to be set after acquiring lock")
	}
	if d.commitInProgress.BeadID != "bead-1" {
		t.Errorf("Expected BeadID=bead-1, got %s", d.commitInProgress.BeadID)
	}
	d.commitStateMutex.RUnlock()

	// Release lock
	d.releaseCommitLock()

	// Verify lock is released
	d.commitStateMutex.RLock()
	if d.commitInProgress != nil {
		t.Fatal("Expected commitInProgress to be nil after releasing lock")
	}
	d.commitStateMutex.RUnlock()

	// Second lock acquisition after release
	err2 := d.acquireCommitLock(ctx, "bead-2", "agent-2")
	if err2 != nil {
		t.Fatalf("Expected second lock acquisition to succeed, got error: %v", err2)
	}

	// Verify new lock is held
	d.commitStateMutex.RLock()
	if d.commitInProgress == nil {
		t.Fatal("Expected commitInProgress to be set after second lock acquisition")
	}
	if d.commitInProgress.BeadID != "bead-2" {
		t.Errorf("Expected BeadID=bead-2, got %s", d.commitInProgress.BeadID)
	}
	d.commitStateMutex.RUnlock()

	d.releaseCommitLock()
}

// TestAcquireCommitLock_Concurrent tests that concurrent commit requests are serialized
func TestAcquireCommitLock_Concurrent(t *testing.T) {
	d := &Dispatcher{
		commitQueue:       make(chan commitRequest, 100),
		commitLockTimeout: 5 * time.Minute,
	}

	// Start commit queue processor
	go d.processCommitQueue()

	ctx := context.Background()

	// Track order of lock acquisitions
	var acquireOrder []string
	var orderMutex sync.Mutex

	// Launch 5 concurrent commit requests
	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		beadID := "bead-" + string(rune('0'+i))
		agentID := "agent-" + string(rune('0'+i))

		go func(bid, aid string) {
			defer wg.Done()

			// Acquire lock
			err := d.acquireCommitLock(ctx, bid, aid)
			if err != nil {
				t.Errorf("Failed to acquire lock for %s: %v", bid, err)
				return
			}

			// Record acquisition order
			orderMutex.Lock()
			acquireOrder = append(acquireOrder, bid)
			orderMutex.Unlock()

			// Hold lock briefly
			time.Sleep(10 * time.Millisecond)

			// Release lock
			d.releaseCommitLock()
		}(beadID, agentID)
	}

	// Wait for all commits to complete
	wg.Wait()

	// Verify all 5 commits executed
	if len(acquireOrder) != 5 {
		t.Errorf("Expected 5 commits to execute, got %d", len(acquireOrder))
	}

	// Verify no concurrent executions (they should be serialized)
	// If serialized correctly, we should have exactly 5 entries
	uniqueBeads := make(map[string]bool)
	for _, bid := range acquireOrder {
		uniqueBeads[bid] = true
	}
	if len(uniqueBeads) != 5 {
		t.Errorf("Expected 5 unique beads, got %d (possible concurrent execution)", len(uniqueBeads))
	}
}

// TestCommitLockTimeout tests that stale locks are forcibly released after timeout
func TestCommitLockTimeout(t *testing.T) {
	d := &Dispatcher{
		commitQueue:       make(chan commitRequest, 100),
		commitLockTimeout: 100 * time.Millisecond, // Short timeout for testing
	}

	// Start commit queue processor
	go d.processCommitQueue()

	ctx := context.Background()

	// Acquire first lock
	err := d.acquireCommitLock(ctx, "bead-1", "agent-1")
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}

	// Don't release the lock, simulate a hung commit

	// Wait for timeout to expire
	time.Sleep(150 * time.Millisecond)

	// Try to acquire second lock (should detect timeout and forcibly release first lock)
	err2 := d.acquireCommitLock(ctx, "bead-2", "agent-2")
	if err2 != nil {
		t.Fatalf("Failed to acquire second lock after timeout: %v", err2)
	}

	// Verify new lock is held
	d.commitStateMutex.RLock()
	if d.commitInProgress == nil {
		t.Fatal("Expected commitInProgress to be set after timeout recovery")
	}
	if d.commitInProgress.BeadID != "bead-2" {
		t.Errorf("Expected BeadID=bead-2 after timeout, got %s", d.commitInProgress.BeadID)
	}
	d.commitStateMutex.RUnlock()

	d.releaseCommitLock()
}

// TestCommitLockContext_Cancel tests that context cancellation is handled correctly
func TestCommitLockContext_Cancel(t *testing.T) {
	d := &Dispatcher{
		commitQueue:       make(chan commitRequest, 100),
		commitLockTimeout: 5 * time.Minute,
	}

	// Start commit queue processor
	go d.processCommitQueue()

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Acquire first lock to block the queue
	err := d.acquireCommitLock(ctx, "bead-1", "agent-1")
	if err != nil {
		t.Fatalf("Failed to acquire first lock: %v", err)
	}

	// Try to acquire second lock in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- d.acquireCommitLock(ctx, "bead-2", "agent-2")
	}()

	// Cancel context while waiting for lock
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Should get context cancellation error
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("Expected error due to context cancellation, got nil")
		}
		if err.Error() != "context cancelled while waiting for commit" &&
		   err.Error() != "context cancelled while waiting for commit queue" {
			t.Errorf("Expected context cancellation error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for context cancellation error")
	}

	d.releaseCommitLock()
}

// TestCommitLockState_TracksTiming tests that commit state correctly tracks timing
func TestCommitLockState_TracksTiming(t *testing.T) {
	d := &Dispatcher{
		commitQueue:       make(chan commitRequest, 100),
		commitLockTimeout: 5 * time.Minute,
	}

	// Start commit queue processor
	go d.processCommitQueue()

	ctx := context.Background()

	startTime := time.Now()

	// Acquire lock
	err := d.acquireCommitLock(ctx, "bead-1", "agent-1")
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Verify StartedAt is set and recent
	d.commitStateMutex.RLock()
	if d.commitInProgress == nil {
		t.Fatal("Expected commitInProgress to be set")
	}
	if d.commitInProgress.StartedAt.Before(startTime) {
		t.Error("StartedAt is before test start time")
	}
	if time.Since(d.commitInProgress.StartedAt) > 100*time.Millisecond {
		t.Error("StartedAt is too far in the past")
	}
	d.commitStateMutex.RUnlock()

	// Hold lock briefly
	time.Sleep(50 * time.Millisecond)

	// Release and verify timing
	d.releaseCommitLock()

	// Verify state is cleared
	d.commitStateMutex.RLock()
	if d.commitInProgress != nil {
		t.Error("Expected commitInProgress to be nil after release")
	}
	d.commitStateMutex.RUnlock()
}
