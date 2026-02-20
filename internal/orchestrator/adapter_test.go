package orchestrator

import (
	"testing"
)

func TestNewBeadManagerAdapter_Nil(t *testing.T) {
	adapter := NewBeadManagerAdapter(nil)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.manager != nil {
		t.Error("manager should be nil")
	}
}
