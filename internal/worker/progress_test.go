package worker

import (
	"strings"
	"testing"

	"github.com/jordanhubbard/loom/internal/actions"
)

func TestProgressTracker_Empty(t *testing.T) {
	pt := NewProgressTracker(25)
	s := pt.Summary(1)
	if !strings.Contains(s, "Iteration 1/25") {
		t.Errorf("expected iteration header, got: %s", s)
	}
	if strings.Contains(s, "Files modified") {
		t.Errorf("should not list files when none written")
	}
}

func TestProgressTracker_TracksReads(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionReadCode, Status: "executed", Metadata: map[string]interface{}{"path": "main.go"}},
		{ActionType: actions.ActionReadFile, Status: "executed", Metadata: map[string]interface{}{"path": "go.mod"}},
	})
	s := pt.Summary(1)
	if !strings.Contains(s, "read 2 files") {
		t.Errorf("expected 'read 2 files', got: %s", s)
	}
}

func TestProgressTracker_TracksWrites(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionWriteFile, Status: "executed", Metadata: map[string]interface{}{"path": "foo.go"}},
		{ActionType: actions.ActionEditCode, Status: "executed", Metadata: map[string]interface{}{"path": "bar.go"}},
		{ActionType: actions.ActionApplyPatch, Status: "executed", Metadata: map[string]interface{}{"path": "baz.go"}},
	})
	s := pt.Summary(1)
	if !strings.Contains(s, "wrote 3 files") {
		t.Errorf("expected 'wrote 3 files', got: %s", s)
	}
	if !strings.Contains(s, "Files modified:") {
		t.Errorf("expected files list, got: %s", s)
	}
}

func TestProgressTracker_DeduplicatesFiles(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionWriteFile, Status: "executed", Metadata: map[string]interface{}{"path": "same.go"}},
	})
	pt.Update(2, []actions.Result{
		{ActionType: actions.ActionEditCode, Status: "executed", Metadata: map[string]interface{}{"path": "same.go"}},
	})
	s := pt.Summary(2)
	if !strings.Contains(s, "wrote 1 files") {
		t.Errorf("expected deduplicated count 1, got: %s", s)
	}
}

func TestProgressTracker_BuildAndTest(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionBuildProject, Status: "executed", Metadata: map[string]interface{}{"success": true}},
	})
	s := pt.Summary(1)
	if !strings.Contains(s, "build: pass") {
		t.Errorf("expected build pass, got: %s", s)
	}

	pt.Update(2, []actions.Result{
		{ActionType: actions.ActionRunTests, Status: "error", Message: "tests failed"},
	})
	s = pt.Summary(2)
	if !strings.Contains(s, "tests: fail") {
		t.Errorf("expected tests fail, got: %s", s)
	}
}

func TestProgressTracker_GitAndBeads(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionGitCommit, Status: "executed"},
		{ActionType: actions.ActionGitPush, Status: "executed"},
		{ActionType: actions.ActionCreateBead, Status: "executed"},
		{ActionType: actions.ActionCloseBead, Status: "executed"},
	})
	s := pt.Summary(1)
	if !strings.Contains(s, "committed") {
		t.Errorf("expected committed, got: %s", s)
	}
	if !strings.Contains(s, "pushed") {
		t.Errorf("expected pushed, got: %s", s)
	}
	if !strings.Contains(s, "1 beads created") {
		t.Errorf("expected 1 beads created, got: %s", s)
	}
	if !strings.Contains(s, "1 beads closed") {
		t.Errorf("expected 1 beads closed, got: %s", s)
	}
}

func TestProgressTracker_ErrorCounting(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionBuildProject, Status: "error"},
		{ActionType: actions.ActionRunCommand, Status: "error"},
	})
	s := pt.Summary(1)
	if !strings.Contains(s, "2 errors") {
		t.Errorf("expected 2 errors, got: %s", s)
	}
}

func TestProgressTracker_IgnoresErroredGit(t *testing.T) {
	pt := NewProgressTracker(10)
	pt.Update(1, []actions.Result{
		{ActionType: actions.ActionGitCommit, Status: "error"},
	})
	s := pt.Summary(1)
	if strings.Contains(s, "committed") {
		t.Errorf("should not show committed on error, got: %s", s)
	}
}

func TestIsProgressStagnant_ReadTreeEarlyDetection(t *testing.T) {
	pt := NewProgressTracker(25)

	// read_tree should trigger before the iteration-15 gate
	counts := map[string]int{"read_tree": 6}
	stagnant, reason := pt.IsProgressStagnant(3, counts)
	if !stagnant {
		t.Fatal("expected stagnant for 6 read_tree calls at iteration 3")
	}
	if !strings.Contains(reason, "read_tree") {
		t.Errorf("expected reason to mention read_tree, got: %s", reason)
	}
}

func TestIsProgressStagnant_ReadTreeBelowThreshold(t *testing.T) {
	pt := NewProgressTracker(25)

	// 3 read_tree calls should not trigger (threshold is >5)
	counts := map[string]int{"read_tree": 3}
	stagnant, _ := pt.IsProgressStagnant(3, counts)
	if stagnant {
		t.Fatal("should not be stagnant for only 3 read_tree calls")
	}
}

func TestIsProgressStagnant_OtherActionsNeedIteration15(t *testing.T) {
	pt := NewProgressTracker(25)

	// 20 search_text calls at iteration 5 should NOT trigger (below iteration gate)
	counts := map[string]int{"search_text": 20}
	stagnant, _ := pt.IsProgressStagnant(5, counts)
	if stagnant {
		t.Fatal("should not trigger before iteration 15 for non-read_tree actions")
	}

	// Same counts at iteration 16 SHOULD trigger
	stagnant, reason := pt.IsProgressStagnant(16, counts)
	if !stagnant {
		t.Fatal("expected stagnant for 20 search_text at iteration 16")
	}
	if !strings.Contains(reason, "search_text") {
		t.Errorf("expected reason to mention search_text, got: %s", reason)
	}
}
