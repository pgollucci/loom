package swarm

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/pkg/messages"
)

// SwarmBus abstracts message publishing/subscription for the swarm manager.
type SwarmBus interface {
	PublishSwarm(ctx context.Context, msg *messages.SwarmMessage) error
	SubscribeSwarm(handler func(*messages.SwarmMessage)) error
}

// Member represents a service that has joined the swarm
type Member struct {
	ServiceID    string
	ServiceType  string
	InstanceID   string
	Capabilities []string
	Roles        []string
	ProjectIDs   []string
	Endpoint     string
	Status       string
	Load         *messages.ServiceLoad
	LastSeen     time.Time
	JoinedAt     time.Time
}

// Manager tracks all connected services (agents, other looms, external tools).
type Manager struct {
	bus        SwarmBus
	members    map[string]*Member // instanceID -> member
	mu         sync.RWMutex
	selfID     string
	selfType   string
	cancel     context.CancelFunc

	heartbeatInterval time.Duration
	staleThreshold    time.Duration
	reapInterval      time.Duration
}

// NewManager creates a new swarm manager.
func NewManager(bus SwarmBus, selfID, selfType string) *Manager {
	return &Manager{
		bus:               bus,
		members:           make(map[string]*Member),
		selfID:            selfID,
		selfType:          selfType,
		heartbeatInterval: 15 * time.Second,
		staleThreshold:    60 * time.Second,
		reapInterval:      30 * time.Second,
	}
}

// Start begins swarm management: announce self, subscribe to swarm messages, send heartbeats.
func (m *Manager) Start(ctx context.Context, roles, projectIDs []string, endpoint string) error {
	ctx, m.cancel = context.WithCancel(ctx)

	instanceID := m.selfID + "-" + uuid.New().String()[:8]

	// Subscribe to swarm messages
	if err := m.bus.SubscribeSwarm(func(msg *messages.SwarmMessage) {
		m.handleSwarmMessage(msg)
	}); err != nil {
		return err
	}

	// Announce ourselves
	announce := messages.NewSwarmAnnounce(m.selfID, m.selfType, instanceID, roles, projectIDs, endpoint)
	if err := m.bus.PublishSwarm(ctx, announce); err != nil {
		log.Printf("[Swarm] Warning: Failed to publish announce: %v", err)
	}

	// Register self
	m.mu.Lock()
	m.members[instanceID] = &Member{
		ServiceID:   m.selfID,
		ServiceType: m.selfType,
		InstanceID:  instanceID,
		Roles:       roles,
		ProjectIDs:  projectIDs,
		Endpoint:    endpoint,
		Status:      "online",
		JoinedAt:    time.Now(),
		LastSeen:    time.Now(),
	}
	m.mu.Unlock()

	// Background heartbeat
	go m.heartbeatLoop(ctx, instanceID)

	// Background stale member reaper
	go m.reapStaleMembers(ctx)

	log.Printf("[Swarm] Manager started (id=%s type=%s instance=%s)", m.selfID, m.selfType, instanceID)
	return nil
}

func (m *Manager) handleSwarmMessage(msg *messages.SwarmMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch msg.Type {
	case "swarm.announce":
		m.members[msg.InstanceID] = &Member{
			ServiceID:    msg.ServiceID,
			ServiceType:  msg.ServiceType,
			InstanceID:   msg.InstanceID,
			Capabilities: msg.Capabilities,
			Roles:        msg.Roles,
			ProjectIDs:   msg.ProjectIDs,
			Endpoint:     msg.Endpoint,
			Status:       msg.Status,
			Load:         msg.Load,
			LastSeen:     time.Now(),
			JoinedAt:     time.Now(),
		}
		log.Printf("[Swarm] New member: %s (type=%s, roles=%v)", msg.ServiceID, msg.ServiceType, msg.Roles)

	case "swarm.heartbeat":
		if member, ok := m.members[msg.InstanceID]; ok {
			member.Status = msg.Status
			member.Load = msg.Load
			member.LastSeen = time.Now()
		}

	case "swarm.leave":
		if _, ok := m.members[msg.InstanceID]; ok {
			delete(m.members, msg.InstanceID)
			log.Printf("[Swarm] Member left: %s (instance=%s)", msg.ServiceID, msg.InstanceID)
		}
	}
}

func (m *Manager) heartbeatLoop(ctx context.Context, instanceID string) {
	ticker := time.NewTicker(m.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Send leave message
			leave := messages.NewSwarmLeave(m.selfID, instanceID)
			_ = m.bus.PublishSwarm(context.Background(), leave)
			return
		case <-ticker.C:
			hb := messages.NewSwarmHeartbeat(m.selfID, instanceID, "online", nil)
			if err := m.bus.PublishSwarm(ctx, hb); err != nil {
				log.Printf("[Swarm] Failed to send heartbeat: %v", err)
			}
		}
	}
}

func (m *Manager) reapStaleMembers(ctx context.Context) {
	ticker := time.NewTicker(m.reapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			cutoff := time.Now().Add(-m.staleThreshold)
			for id, member := range m.members {
				if member.LastSeen.Before(cutoff) {
					log.Printf("[Swarm] Reaping stale member: %s (last seen: %v)", member.ServiceID, member.LastSeen)
					delete(m.members, id)
				}
			}
			m.mu.Unlock()
		}
	}
}

// GetMembers returns a snapshot of all current swarm members.
func (m *Manager) GetMembers() []Member {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Member, 0, len(m.members))
	for _, member := range m.members {
		result = append(result, *member)
	}
	return result
}

// GetMembersByRole returns members that have a specific role.
func (m *Manager) GetMembersByRole(role string) []Member {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Member
	for _, member := range m.members {
		for _, r := range member.Roles {
			if r == role {
				result = append(result, *member)
				break
			}
		}
	}
	return result
}

// GetMembersByProject returns members serving a specific project.
func (m *Manager) GetMembersByProject(projectID string) []Member {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Member
	for _, member := range m.members {
		for _, p := range member.ProjectIDs {
			if p == projectID {
				result = append(result, *member)
				break
			}
		}
	}
	return result
}

// MemberCount returns the total number of swarm members.
func (m *Manager) MemberCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.members)
}

// HasCapability checks if any swarm member has the given capability.
func (m *Manager) HasCapability(capability string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, member := range m.members {
		for _, c := range member.Capabilities {
			if c == capability {
				return true
			}
		}
	}
	return false
}

// Close shuts down the swarm manager.
func (m *Manager) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	log.Printf("[Swarm] Manager stopped")
}
