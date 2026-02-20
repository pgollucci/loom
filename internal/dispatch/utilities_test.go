package dispatch

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jordanhubbard/loom/pkg/models"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"hello world", 5, "hello... (truncated)"},
		{"exact", 5, "exact"},
		{"", 5, ""},
		{"a", 1, "a"},
		{"ab", 1, "a... (truncated)"},
	}
	for _, tc := range tests {
		got := truncateString(tc.input, tc.maxLen)
		if got != tc.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"hello world", "hello", true},
		{"hello world", "world", true},
		{"hello world", "lo wo", true},
		{"hello world", "xyz", false},
		{"hello", "hello", true},
		{"hello", "hello!", false},
		{"", "", true},
		{"abc", "", true},
		{"", "a", false},
		{"401 Unauthorized", "401", true},
		{"Rate limit exceeded", "rate limit", false},
		{"Rate limit exceeded", "Rate limit", true},
	}
	for _, tc := range tests {
		got := contains(tc.s, tc.substr)
		if got != tc.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tc.s, tc.substr, got, tc.want)
		}
	}
}

func TestFindSubstring(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"hello world", "lo wo", true},
		{"hello", "xyz", false},
		{"abc", "abc", true},
		{"abc", "abcd", false},
		{"aabaa", "ab", true},
	}
	for _, tc := range tests {
		got := findSubstring(tc.s, tc.substr)
		if got != tc.want {
			t.Errorf("findSubstring(%q, %q) = %v, want %v", tc.s, tc.substr, got, tc.want)
		}
	}
}

func TestLoopDetector_GetErrorHistory_NilContext(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{}
	history := ld.getErrorHistory(bead)
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d records", len(history))
	}
}

func TestLoopDetector_GetErrorHistory_EmptyJSON(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{Context: map[string]string{"error_history": ""}}
	history := ld.getErrorHistory(bead)
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d records", len(history))
	}
}

func TestLoopDetector_GetErrorHistory_ValidJSON(t *testing.T) {
	ld := NewLoopDetector()
	records := []ErrorRecord{
		{Timestamp: time.Now(), Error: "auth error", Dispatch: 1},
		{Timestamp: time.Now(), Error: "timeout", Dispatch: 2},
	}
	data, _ := json.Marshal(records)
	bead := &models.Bead{Context: map[string]string{"error_history": string(data)}}

	history := ld.getErrorHistory(bead)
	if len(history) != 2 {
		t.Errorf("expected 2 records, got %d", len(history))
	}
}

func TestLoopDetector_GetErrorHistory_InvalidJSON(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{Context: map[string]string{"error_history": "{invalid"}}
	history := ld.getErrorHistory(bead)
	if len(history) != 0 {
		t.Errorf("expected empty history for invalid JSON, got %d", len(history))
	}
}

func TestLoopDetector_SaveErrorHistory(t *testing.T) {
	ld := NewLoopDetector()

	// Test with nil context
	bead := &models.Bead{}
	records := []ErrorRecord{{Timestamp: time.Now(), Error: "test", Dispatch: 1}}
	ld.saveErrorHistory(bead, records)
	if bead.Context == nil {
		t.Fatal("context should be initialized")
	}
	if bead.Context["error_history"] == "" {
		t.Error("error_history should be set")
	}

	// Verify the stored JSON is valid
	var parsed []ErrorRecord
	if err := json.Unmarshal([]byte(bead.Context["error_history"]), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) != 1 {
		t.Errorf("expected 1 record, got %d", len(parsed))
	}
}

func TestLoopDetector_SaveErrorHistory_ExistingContext(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{Context: map[string]string{"other_key": "value"}}
	records := []ErrorRecord{{Timestamp: time.Now(), Error: "err", Dispatch: 1}}
	ld.saveErrorHistory(bead, records)

	if bead.Context["other_key"] != "value" {
		t.Error("existing context should be preserved")
	}
	if bead.Context["error_history"] == "" {
		t.Error("error_history should be set")
	}
}

func TestLoopDetector_CheckRepeatedErrors_NilContext(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{}
	stuck, _ := ld.checkRepeatedErrors(bead)
	if stuck {
		t.Error("nil context should not be stuck")
	}
}

func TestLoopDetector_CheckRepeatedErrors_LowDispatchCount(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{Context: map[string]string{
		"dispatch_count": "3",
		"last_run_error": "some error",
	}}
	stuck, _ := ld.checkRepeatedErrors(bead)
	if stuck {
		t.Error("low dispatch count should not be stuck")
	}
}

func TestLoopDetector_CheckRepeatedErrors_NoLastError(t *testing.T) {
	ld := NewLoopDetector()
	bead := &models.Bead{Context: map[string]string{
		"dispatch_count": "10",
	}}
	stuck, _ := ld.checkRepeatedErrors(bead)
	if stuck {
		t.Error("no last error should not be stuck")
	}
}

func TestLoopDetector_CheckRepeatedErrors_AuthErrors(t *testing.T) {
	ld := NewLoopDetector()

	// Build 5+ auth errors in history
	var records []ErrorRecord
	for i := 0; i < 6; i++ {
		records = append(records, ErrorRecord{
			Timestamp: time.Now(),
			Error:     "401 Unauthorized: No api key provided",
			Dispatch:  i + 1,
		})
	}
	histJSON, _ := json.Marshal(records)

	bead := &models.Bead{Context: map[string]string{
		"dispatch_count": "10",
		"last_run_error": "401 Unauthorized: No api key provided",
		"error_history":  string(histJSON),
	}}

	stuck, reason := ld.checkRepeatedErrors(bead)
	if !stuck {
		t.Error("repeated auth errors should be detected as stuck")
	}
	if reason == "" {
		t.Error("expected a reason string")
	}
}

func TestLoopDetector_CheckRepeatedErrors_ProviderErrors(t *testing.T) {
	ld := NewLoopDetector()

	var records []ErrorRecord
	for i := 0; i < 6; i++ {
		records = append(records, ErrorRecord{
			Timestamp: time.Now(),
			Error:     "502 Bad Gateway",
			Dispatch:  i + 1,
		})
	}
	histJSON, _ := json.Marshal(records)

	bead := &models.Bead{Context: map[string]string{
		"dispatch_count": "10",
		"last_run_error": "502 Bad Gateway",
		"error_history":  string(histJSON),
	}}

	stuck, _ := ld.checkRepeatedErrors(bead)
	if !stuck {
		t.Error("repeated provider errors should be detected as stuck")
	}
}

func TestLoopDetector_CheckRepeatedErrors_RateLimitErrors(t *testing.T) {
	ld := NewLoopDetector()

	var records []ErrorRecord
	for i := 0; i < 6; i++ {
		records = append(records, ErrorRecord{
			Timestamp: time.Now(),
			Error:     "429 Rate limit exceeded",
			Dispatch:  i + 1,
		})
	}
	histJSON, _ := json.Marshal(records)

	bead := &models.Bead{Context: map[string]string{
		"dispatch_count": "10",
		"last_run_error": "429 Rate limit exceeded",
		"error_history":  string(histJSON),
	}}

	stuck, _ := ld.checkRepeatedErrors(bead)
	if !stuck {
		t.Error("repeated rate limit errors should be detected as stuck")
	}
}

func TestLoopDetector_CheckRepeatedErrors_SameError(t *testing.T) {
	ld := NewLoopDetector()

	var records []ErrorRecord
	for i := 0; i < 6; i++ {
		records = append(records, ErrorRecord{
			Timestamp: time.Now(),
			Error:     "some unknown error",
			Dispatch:  i + 1,
		})
	}
	histJSON, _ := json.Marshal(records)

	bead := &models.Bead{Context: map[string]string{
		"dispatch_count": "10",
		"last_run_error": "some unknown error",
		"error_history":  string(histJSON),
	}}

	stuck, _ := ld.checkRepeatedErrors(bead)
	if !stuck {
		t.Error("identical repeated errors should be detected as stuck")
	}
}

func TestNewResultHandler(t *testing.T) {
	rh := NewResultHandler()
	if rh == nil {
		t.Fatal("expected non-nil result handler")
	}
	if rh.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", rh.PendingCount())
	}
}

func TestSetContainerOrchestrator(t *testing.T) {
	d := &Dispatcher{}
	d.SetContainerOrchestrator(nil)
	if d.containerOrch != nil {
		t.Error("expected nil container orchestrator")
	}
}
