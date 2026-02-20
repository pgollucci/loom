package messagebus

import (
	"testing"
	"time"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}
	if cfg.URL != "" {
		t.Error("URL should default to empty")
	}
	if cfg.StreamName != "" {
		t.Error("StreamName should default to empty")
	}
}

func TestConfig_Fields(t *testing.T) {
	cfg := Config{
		URL:        "nats://custom:4222",
		StreamName: "CUSTOM",
		Timeout:    30 * time.Second,
	}
	if cfg.URL != "nats://custom:4222" {
		t.Errorf("got URL %q", cfg.URL)
	}
	if cfg.StreamName != "CUSTOM" {
		t.Errorf("got stream %q", cfg.StreamName)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("got timeout %v", cfg.Timeout)
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		s, prefix, want string
	}{
		{"swarm.announce", "swarm.", "announce"},
		{"swarm.heartbeat", "swarm.", "heartbeat"},
		{"swarm.leave", "swarm.", "leave"},
		{"other.thing", "swarm.", "other.thing"},
		{"", "swarm.", ""},
		{"swarm.", "swarm.", ""},
	}

	for _, tc := range tests {
		got := stripPrefix(tc.s, tc.prefix)
		if got != tc.want {
			t.Errorf("stripPrefix(%q, %q) = %q, want %q", tc.s, tc.prefix, got, tc.want)
		}
	}
}

func TestNewNatsMessageBus_BadURL(t *testing.T) {
	_, err := NewNatsMessageBus(Config{
		URL:     "nats://nonexistent-host:99999",
		Timeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Error("expected error connecting to nonexistent NATS")
	}
}
