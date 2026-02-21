package swarm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jordanhubbard/loom/pkg/messages"
)

type mockSwarmBus struct {
	mu           sync.Mutex
	published    []*messages.SwarmMessage
	subscribeErr error
	publishErr   error
	swarmHandler func(*messages.SwarmMessage)
}

func (m *mockSwarmBus) PublishSwarm(_ context.Context, msg *messages.SwarmMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, msg)
	return nil
}

func (m *mockSwarmBus) SubscribeSwarm(handler func(*messages.SwarmMessage)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.swarmHandler = handler
	return nil
}

func TestManager_HandleSwarmAnnounce(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	msg := &messages.SwarmMessage{
		Type:        "swarm.announce",
		ServiceID:   "svc-1",
		ServiceType: "agent-coder",
		InstanceID:  "inst-abc",
		Roles:       []string{"coder"},
		ProjectIDs:  []string{"proj-1"},
		Endpoint:    "http://coder:8090",
		Status:      "online",
	}

	m.handleSwarmMessage(msg)

	if m.MemberCount() != 1 {
		t.Fatalf("expected 1 member, got %d", m.MemberCount())
	}

	members := m.GetMembers()
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}

	member := members[0]
	if member.ServiceID != "svc-1" {
		t.Errorf("got service %q", member.ServiceID)
	}
	if member.ServiceType != "agent-coder" {
		t.Errorf("got type %q", member.ServiceType)
	}
	if len(member.Roles) != 1 || member.Roles[0] != "coder" {
		t.Errorf("got roles %v", member.Roles)
	}
}

func TestManager_HandleSwarmHeartbeat(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "inst-abc", Status: "online",
	})
	time.Sleep(10 * time.Millisecond)

	load := &messages.ServiceLoad{ActiveTasks: 5, MaxTasks: 10}
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.heartbeat", ServiceID: "svc-1", InstanceID: "inst-abc",
		Status: "busy", Load: load,
	})

	m.mu.RLock()
	member := m.members["inst-abc"]
	m.mu.RUnlock()

	if member.Status != "busy" {
		t.Errorf("expected busy, got %q", member.Status)
	}
	if member.Load == nil || member.Load.ActiveTasks != 5 {
		t.Error("load not updated")
	}
}

func TestManager_HandleSwarmLeave(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "inst-abc", Status: "online",
	})
	if m.MemberCount() != 1 {
		t.Fatal("member should exist")
	}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.leave", ServiceID: "svc-1", InstanceID: "inst-abc",
	})
	if m.MemberCount() != 0 {
		t.Error("member should have been removed")
	}
}

func TestManager_HeartbeatForUnknownInstance(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.heartbeat", ServiceID: "unknown", InstanceID: "unknown-inst", Status: "online",
	})
	if m.MemberCount() != 0 {
		t.Error("unknown heartbeat should not create member")
	}
}

func TestManager_Start_Success(t *testing.T) {
	bus := &mockSwarmBus{}
	m := NewManager(bus, "loom-cp", "control-plane")

	err := m.Start(context.Background(), []string{"orchestrator"}, []string{"proj-1"}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Close()

	bus.mu.Lock()
	hasHandler := bus.swarmHandler != nil
	published := len(bus.published)
	bus.mu.Unlock()

	if !hasHandler {
		t.Error("expected swarm handler to be registered")
	}
	if published < 1 {
		t.Errorf("expected at least 1 announce message, got %d", published)
	}

	// Self should be registered as a member
	if m.MemberCount() < 1 {
		t.Error("expected self to be registered as member")
	}
}

func TestManager_Start_SubscribeError(t *testing.T) {
	bus := &mockSwarmBus{subscribeErr: fmt.Errorf("subscribe failed")}
	m := NewManager(bus, "loom-cp", "control-plane")

	err := m.Start(context.Background(), nil, nil, "")
	if err == nil {
		t.Error("expected error from subscribe failure")
	}
}

func TestManager_HeartbeatLoop_SendsHeartbeats(t *testing.T) {
	bus := &mockSwarmBus{}
	m := NewManager(bus, "loom-cp", "control-plane")
	m.heartbeatInterval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go m.heartbeatLoop(ctx, "test-instance")

	time.Sleep(130 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	bus.mu.Lock()
	msgCount := len(bus.published)
	bus.mu.Unlock()

	// Should have sent at least 2 heartbeats + 1 leave
	if msgCount < 2 {
		t.Errorf("expected at least 2 messages (heartbeats + leave), got %d", msgCount)
	}

	// Last message should be a leave
	bus.mu.Lock()
	lastMsg := bus.published[msgCount-1]
	bus.mu.Unlock()
	if lastMsg.Type != "swarm.leave" {
		t.Errorf("expected last message to be swarm.leave, got %q", lastMsg.Type)
	}
}

func TestManager_ReapStaleMembers(t *testing.T) {
	bus := &mockSwarmBus{}
	m := NewManager(bus, "loom", "cp")
	m.staleThreshold = 500 * time.Millisecond
	m.reapInterval = 50 * time.Millisecond

	m.mu.Lock()
	m.members["stale"] = &Member{
		ServiceID: "old-svc", InstanceID: "stale", Status: "online",
		LastSeen: time.Now().Add(-2 * time.Second),
	}
	m.members["fresh"] = &Member{
		ServiceID: "new-svc", InstanceID: "fresh", Status: "online",
		LastSeen: time.Now(),
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go m.reapStaleMembers(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.members["stale"]; ok {
		t.Error("stale member should have been reaped")
	}
	if _, ok := m.members["fresh"]; !ok {
		t.Error("fresh member should still exist")
	}
}

func TestManager_GetMembersByRole(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "i1",
		Roles: []string{"coder"}, Status: "online",
	})
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-2", InstanceID: "i2",
		Roles: []string{"reviewer"}, Status: "online",
	})
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-3", InstanceID: "i3",
		Roles: []string{"coder", "reviewer"}, Status: "online",
	})

	coders := m.GetMembersByRole("coder")
	if len(coders) != 2 {
		t.Errorf("expected 2 coders, got %d", len(coders))
	}

	reviewers := m.GetMembersByRole("reviewer")
	if len(reviewers) != 2 {
		t.Errorf("expected 2 reviewers, got %d", len(reviewers))
	}

	qa := m.GetMembersByRole("qa")
	if len(qa) != 0 {
		t.Errorf("expected 0 qa, got %d", len(qa))
	}
}

func TestManager_GetMembersByProject(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "i1",
		ProjectIDs: []string{"proj-1"}, Status: "online",
	})
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-2", InstanceID: "i2",
		ProjectIDs: []string{"proj-2"}, Status: "online",
	})
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-3", InstanceID: "i3",
		ProjectIDs: []string{"proj-1", "proj-2"}, Status: "online",
	})

	proj1 := m.GetMembersByProject("proj-1")
	if len(proj1) != 2 {
		t.Errorf("expected 2 members for proj-1, got %d", len(proj1))
	}
	proj3 := m.GetMembersByProject("proj-3")
	if len(proj3) != 0 {
		t.Errorf("expected 0 members for proj-3, got %d", len(proj3))
	}
}

func TestManager_HasCapability(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "i1",
		Capabilities: []string{"golang", "docker"}, Status: "online",
	})

	if !m.HasCapability("golang") {
		t.Error("should have golang capability")
	}
	if !m.HasCapability("docker") {
		t.Error("should have docker capability")
	}
	if m.HasCapability("rust") {
		t.Error("should not have rust capability")
	}
}

func TestManager_Close(t *testing.T) {
	called := false
	m := &Manager{
		members: make(map[string]*Member),
		cancel:  func() { called = true },
	}
	m.Close()
	if !called {
		t.Error("cancel should be called")
	}
}

func TestManager_CloseNilCancel(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	m.Close()
}

func TestNewManager(t *testing.T) {
	bus := &mockSwarmBus{}
	m := NewManager(bus, "svc-1", "control-plane")
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.selfID != "svc-1" {
		t.Errorf("got self ID %q", m.selfID)
	}
	if m.selfType != "control-plane" {
		t.Errorf("got self type %q", m.selfType)
	}
	if m.bus == nil {
		t.Error("bus should be set")
	}
	if m.heartbeatInterval != 15*time.Second {
		t.Errorf("got heartbeat interval %v", m.heartbeatInterval)
	}
	if m.staleThreshold != 60*time.Second {
		t.Errorf("got stale threshold %v", m.staleThreshold)
	}
	if m.MemberCount() != 0 {
		t.Errorf("expected 0 members, got %d", m.MemberCount())
	}
}

func TestManager_MultipleAnnounces(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	for i := 0; i < 10; i++ {
		m.handleSwarmMessage(&messages.SwarmMessage{
			Type: "swarm.announce", ServiceID: fmt.Sprintf("svc-%d", i),
			InstanceID: fmt.Sprintf("inst-%d", i), Roles: []string{"coder"}, Status: "online",
		})
	}

	if m.MemberCount() != 10 {
		t.Errorf("expected 10 members, got %d", m.MemberCount())
	}
}

func TestManager_ReannounceUpdates(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}

	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "inst-1",
		Roles: []string{"coder"}, Status: "online",
	})
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.announce", ServiceID: "svc-1", InstanceID: "inst-1",
		Roles: []string{"coder", "reviewer"}, Status: "online",
	})

	if m.MemberCount() != 1 {
		t.Errorf("re-announce should not create duplicate, got %d", m.MemberCount())
	}

	members := m.GetMembersByRole("reviewer")
	if len(members) != 1 {
		t.Error("re-announced member should have reviewer role")
	}
}

func TestManager_LeaveNonexistent(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.leave", ServiceID: "nonexistent", InstanceID: "nonexistent",
	})
	if m.MemberCount() != 0 {
		t.Error("leaving nonexistent member should be no-op")
	}
}

func TestManager_HasCapabilityEmpty(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	if m.HasCapability("anything") {
		t.Error("empty swarm should not have any capability")
	}
}

func TestManager_GetMembersByRoleEmpty(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	if len(m.GetMembersByRole("coder")) != 0 {
		t.Error("expected 0 members")
	}
}

func TestManager_GetMembersByProjectEmpty(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	if len(m.GetMembersByProject("proj-1")) != 0 {
		t.Error("expected 0 members")
	}
}

func TestMember_Fields(t *testing.T) {
	m := Member{
		ServiceID: "svc-1", ServiceType: "agent-coder", InstanceID: "inst-1",
		Roles: []string{"coder"}, ProjectIDs: []string{"proj-1", "proj-2"},
		Endpoint: "http://coder:8090", Status: "online",
		JoinedAt: time.Now(), LastSeen: time.Now(),
	}
	if m.ServiceID != "svc-1" {
		t.Errorf("got service %q", m.ServiceID)
	}
	if len(m.ProjectIDs) != 2 {
		t.Errorf("got %d projects", len(m.ProjectIDs))
	}
}

func TestManager_Start_PublishAnnounceError(t *testing.T) {
	bus := &mockSwarmBus{publishErr: fmt.Errorf("publish failed")}
	m := NewManager(bus, "loom", "control-plane")

	err := m.Start(context.Background(), []string{"cp"}, []string{"p1"}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("Start should not fail on publish error (only logs): %v", err)
	}
	defer m.Close()
}

func TestManager_HandleSwarmMessage_UnknownType(t *testing.T) {
	m := &Manager{members: make(map[string]*Member)}
	m.handleSwarmMessage(&messages.SwarmMessage{
		Type: "swarm.unknown", ServiceID: "svc-1", InstanceID: "inst-1",
	})
	if m.MemberCount() != 0 {
		t.Error("unknown message type should not create members")
	}
}
