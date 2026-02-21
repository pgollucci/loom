package messages

import "time"

// SwarmMessage represents a swarm protocol message for dynamic service discovery and federation.
type SwarmMessage struct {
	Type         string                 `json:"type"` // "swarm.announce", "swarm.heartbeat", "swarm.leave", "swarm.request", "swarm.response"
	ServiceID    string                 `json:"service_id"`
	ServiceType  string                 `json:"service_type"` // "control-plane", "agent-coder", "agent-reviewer", "agent-qa", "agent-pm", "loom-peer", "external"
	InstanceID   string                 `json:"instance_id"`
	Capabilities []string               `json:"capabilities,omitempty"`
	Roles        []string               `json:"roles,omitempty"`
	ProjectIDs   []string               `json:"project_ids,omitempty"`
	Endpoint     string                 `json:"endpoint,omitempty"`
	Status       string                 `json:"status"` // "online", "busy", "draining", "offline"
	Load         *ServiceLoad           `json:"load,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ServiceLoad reports the current load of a service
type ServiceLoad struct {
	ActiveTasks   int     `json:"active_tasks"`
	MaxTasks      int     `json:"max_tasks"`
	CPUPercent    float64 `json:"cpu_percent,omitempty"`
	MemoryPercent float64 `json:"memory_percent,omitempty"`
	UptimeSeconds int64   `json:"uptime_seconds"`
}

// SwarmRequest is a capability query sent to the swarm
type SwarmRequest struct {
	RequestID    string   `json:"request_id"`
	RequiredRole string   `json:"required_role,omitempty"`
	ProjectID    string   `json:"project_id,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// SwarmResponse is a reply to a capability query
type SwarmResponse struct {
	RequestID string `json:"request_id"`
	ServiceID string `json:"service_id"`
	Available bool   `json:"available"`
	Capacity  int    `json:"capacity"`
}

// NewSwarmAnnounce creates a swarm.announce message
func NewSwarmAnnounce(serviceID, serviceType, instanceID string, roles, projectIDs []string, endpoint string) *SwarmMessage {
	return &SwarmMessage{
		Type:        "swarm.announce",
		ServiceID:   serviceID,
		ServiceType: serviceType,
		InstanceID:  instanceID,
		Roles:       roles,
		ProjectIDs:  projectIDs,
		Endpoint:    endpoint,
		Status:      "online",
		Timestamp:   time.Now(),
	}
}

// NewSwarmHeartbeat creates a swarm.heartbeat message
func NewSwarmHeartbeat(serviceID, instanceID, status string, load *ServiceLoad) *SwarmMessage {
	return &SwarmMessage{
		Type:       "swarm.heartbeat",
		ServiceID:  serviceID,
		InstanceID: instanceID,
		Status:     status,
		Load:       load,
		Timestamp:  time.Now(),
	}
}

// NewSwarmLeave creates a swarm.leave message
func NewSwarmLeave(serviceID, instanceID string) *SwarmMessage {
	return &SwarmMessage{
		Type:       "swarm.leave",
		ServiceID:  serviceID,
		InstanceID: instanceID,
		Status:     "offline",
		Timestamp:  time.Now(),
	}
}
