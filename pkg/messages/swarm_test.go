package messages

import (
	"testing"
)

func TestNewSwarmAnnounce(t *testing.T) {
	msg := NewSwarmAnnounce("svc-1", "agent-coder", "inst-abc", []string{"coder"}, []string{"proj-1"}, "http://coder:8090")

	if msg.Type != "swarm.announce" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ServiceID != "svc-1" {
		t.Errorf("got service %q", msg.ServiceID)
	}
	if msg.ServiceType != "agent-coder" {
		t.Errorf("got service type %q", msg.ServiceType)
	}
	if msg.InstanceID != "inst-abc" {
		t.Errorf("got instance %q", msg.InstanceID)
	}
	if msg.Status != "online" {
		t.Errorf("got status %q", msg.Status)
	}
	if msg.Timestamp.IsZero() {
		t.Error("timestamp not set")
	}
	if len(msg.Roles) != 1 || msg.Roles[0] != "coder" {
		t.Errorf("got roles %v", msg.Roles)
	}
	if len(msg.ProjectIDs) != 1 || msg.ProjectIDs[0] != "proj-1" {
		t.Errorf("got projects %v", msg.ProjectIDs)
	}
	if msg.Endpoint != "http://coder:8090" {
		t.Errorf("got endpoint %q", msg.Endpoint)
	}
}

func TestNewSwarmHeartbeat(t *testing.T) {
	load := &ServiceLoad{ActiveTasks: 3, MaxTasks: 10, UptimeSeconds: 3600}
	msg := NewSwarmHeartbeat("svc-1", "inst-abc", "busy", load)

	if msg.Type != "swarm.heartbeat" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.ServiceID != "svc-1" {
		t.Errorf("got service %q", msg.ServiceID)
	}
	if msg.Status != "busy" {
		t.Errorf("got status %q", msg.Status)
	}
	if msg.Load == nil {
		t.Fatal("load is nil")
	}
	if msg.Load.ActiveTasks != 3 || msg.Load.MaxTasks != 10 {
		t.Errorf("got load %+v", msg.Load)
	}
}

func TestNewSwarmHeartbeatNilLoad(t *testing.T) {
	msg := NewSwarmHeartbeat("svc-2", "inst-def", "online", nil)
	if msg.Load != nil {
		t.Error("expected nil load")
	}
}

func TestNewSwarmLeave(t *testing.T) {
	msg := NewSwarmLeave("svc-1", "inst-abc")

	if msg.Type != "swarm.leave" {
		t.Errorf("got type %q", msg.Type)
	}
	if msg.Status != "offline" {
		t.Errorf("got status %q", msg.Status)
	}
	if msg.ServiceID != "svc-1" {
		t.Errorf("got service %q", msg.ServiceID)
	}
	if msg.InstanceID != "inst-abc" {
		t.Errorf("got instance %q", msg.InstanceID)
	}
}

func TestServiceLoadFields(t *testing.T) {
	load := ServiceLoad{
		ActiveTasks:   5,
		MaxTasks:      20,
		CPUPercent:    75.5,
		MemoryPercent: 60.2,
		UptimeSeconds: 7200,
	}
	if load.ActiveTasks != 5 {
		t.Errorf("got active tasks %d", load.ActiveTasks)
	}
	if load.CPUPercent != 75.5 {
		t.Errorf("got CPU %f", load.CPUPercent)
	}
}

func TestSwarmRequestAndResponse(t *testing.T) {
	req := SwarmRequest{
		RequestID:    "req-1",
		RequiredRole: "coder",
		ProjectID:    "proj-1",
		Capabilities: []string{"golang", "docker"},
	}
	if req.RequestID != "req-1" {
		t.Error("request ID mismatch")
	}
	if len(req.Capabilities) != 2 {
		t.Errorf("got %d capabilities", len(req.Capabilities))
	}

	resp := SwarmResponse{
		RequestID: "req-1",
		ServiceID: "svc-1",
		Available: true,
		Capacity:  5,
	}
	if !resp.Available {
		t.Error("should be available")
	}
	if resp.Capacity != 5 {
		t.Errorf("got capacity %d", resp.Capacity)
	}
}
