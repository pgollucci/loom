package messagebus

import (
	"testing"

	"github.com/jordanhubbard/loom/internal/temporal/eventbus"
)

func TestIsAgentMessageEvent(t *testing.T) {
	tests := []struct {
		eventType string
		want      bool
	}{
		{"agent.message.broadcast", true},
		{"agent.message.request", true},
		{"agent.message.x", true},
		{"agent.message.", false},  // Exactly 14 chars, no chars after prefix
		{"agent.messages", false},
		{"agent.messag", false},
		{"bead.created", false},
		{"", false},
		{"short", false},
	}

	for _, tc := range tests {
		event := &eventbus.Event{Type: eventbus.EventType(tc.eventType)}
		got := isAgentMessageEvent(event)
		if got != tc.want {
			t.Errorf("isAgentMessageEvent(%q) = %v, want %v", tc.eventType, got, tc.want)
		}
	}
}

func TestIsSignificantEvent(t *testing.T) {
	significant := []eventbus.EventType{
		eventbus.EventTypeBeadCreated,
		eventbus.EventTypeBeadCompleted,
		eventbus.EventTypeBeadStatusChange,
		eventbus.EventTypeAgentSpawned,
		eventbus.EventTypeAgentCompleted,
		eventbus.EventTypeProviderRegistered,
		eventbus.EventTypeProviderDeleted,
		eventbus.EventTypeDecisionCreated,
		eventbus.EventTypeDecisionResolved,
		eventbus.EventTypeWorkflowStarted,
		eventbus.EventTypeWorkflowCompleted,
	}

	for _, et := range significant {
		event := &eventbus.Event{Type: et}
		if !isSignificantEvent(event) {
			t.Errorf("isSignificantEvent(%q) should be true", et)
		}
	}

	insignificant := []eventbus.EventType{
		eventbus.EventTypeAgentHeartbeat,
		eventbus.EventTypeLogMessage,
		eventbus.EventTypeConfigUpdated,
		eventbus.EventType("custom.event"),
		eventbus.EventType(""),
	}

	for _, et := range insignificant {
		event := &eventbus.Event{Type: et}
		if isSignificantEvent(event) {
			t.Errorf("isSignificantEvent(%q) should be false", et)
		}
	}
}

func TestNewBridgedMessageBus(t *testing.T) {
	bridge := NewBridgedMessageBus(nil, nil, "container-1")
	if bridge == nil {
		t.Fatal("expected non-nil bridge")
	}
	if bridge.containerID != "container-1" {
		t.Errorf("got container ID %q", bridge.containerID)
	}
	if bridge.started {
		t.Error("should not be started")
	}
}

func TestBridgedMessageBus_NATS(t *testing.T) {
	bridge := NewBridgedMessageBus(nil, nil, "test")
	if bridge.NATS() != nil {
		t.Error("NATS should be nil when constructed with nil")
	}
}

func TestBridgedMessageBus_Close_NotStarted(t *testing.T) {
	bridge := NewBridgedMessageBus(nil, nil, "test")
	bridge.Close() // Should not panic even when not started
	if bridge.started {
		t.Error("should remain not-started after close")
	}
}
