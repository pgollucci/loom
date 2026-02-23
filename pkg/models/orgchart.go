package models

import "time"

// OrgChart defines the agent team structure for a project
type OrgChart struct {
	EntityMetadata `json:",inline"`

	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Name       string     `json:"name"`        // e.g., "Default", "Custom"
	Positions  []Position `json:"positions"`   // Role slots in the org
	IsTemplate bool       `json:"is_template"` // If true, this is a reusable template
	ParentID   string     `json:"parent_id"`   // For inherited org charts (sub-projects)
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// VersionedEntity interface implementation for OrgChart
func (o *OrgChart) GetEntityType() EntityType          { return EntityTypeOrgChart }
func (o *OrgChart) GetSchemaVersion() SchemaVersion    { return o.EntityMetadata.SchemaVersion }
func (o *OrgChart) SetSchemaVersion(v SchemaVersion)   { o.EntityMetadata.SchemaVersion = v }
func (o *OrgChart) GetEntityMetadata() *EntityMetadata { return &o.EntityMetadata }
func (o *OrgChart) GetID() string                      { return o.ID }

// Position represents a role slot in an org chart
type Position struct {
	EntityMetadata `json:",inline"`

	ID           string    `json:"id"`
	RoleName     string    `json:"role_name"`     // e.g., "ceo", "product-manager"
	PersonaPath  string    `json:"persona_path"`  // e.g., "default/ceo"
	Required     bool      `json:"required"`      // Must be filled for project to be active
	MaxInstances int       `json:"max_instances"` // 0 = unlimited agents can fill this role
	AgentIDs     []string  `json:"agent_ids"`     // Currently assigned agent IDs
	ReportsTo    string    `json:"reports_to"`    // Position ID of manager (for hierarchy)
	CreatedAt    time.Time `json:"created_at"`
}

// VersionedEntity interface implementation for Position
func (p *Position) GetEntityType() EntityType          { return EntityTypePosition }
func (p *Position) GetSchemaVersion() SchemaVersion    { return p.EntityMetadata.SchemaVersion }
func (p *Position) SetSchemaVersion(v SchemaVersion)   { p.EntityMetadata.SchemaVersion = v }
func (p *Position) GetEntityMetadata() *EntityMetadata { return &p.EntityMetadata }
func (p *Position) GetID() string                      { return p.ID }

// PositionStatus returns whether a position is filled, partially filled, or vacant
func (p *Position) Status() string {
	if len(p.AgentIDs) == 0 {
		return "vacant"
	}
	if p.MaxInstances > 0 && len(p.AgentIDs) < p.MaxInstances {
		return "partial"
	}
	return "filled"
}

// IsFilled returns true if the position has at least one agent
func (p *Position) IsFilled() bool {
	return len(p.AgentIDs) > 0
}

// CanAddAgent returns true if another agent can be added to this position
func (p *Position) CanAddAgent() bool {
	if p.MaxInstances == 0 {
		return true // unlimited
	}
	return len(p.AgentIDs) < p.MaxInstances
}

// HasAgent checks if a specific agent is assigned to this position
func (p *Position) HasAgent(agentID string) bool {
	for _, id := range p.AgentIDs {
		if id == agentID {
			return true
		}
	}
	return false
}

// DefaultOrgChartPositions returns the standard positions for a new project
func DefaultOrgChartPositions() []Position {
	return []Position{
		{ID: "pos-ceo", RoleName: "ceo", PersonaPath: "default/ceo", Required: true, MaxInstances: 1},
		{ID: "pos-cfo", RoleName: "cfo", PersonaPath: "default/cfo", Required: false, MaxInstances: 1, ReportsTo: "pos-ceo"},
		{ID: "pos-pm", RoleName: "product-manager", PersonaPath: "default/product-manager", Required: true, MaxInstances: 0, ReportsTo: "pos-ceo"},
		{ID: "pos-em", RoleName: "engineering-manager", PersonaPath: "default/engineering-manager", Required: true, MaxInstances: 0, ReportsTo: "pos-ceo"},
		{ID: "pos-projm", RoleName: "project-manager", PersonaPath: "default/project-manager", Required: false, MaxInstances: 0, ReportsTo: "pos-em"},
		{ID: "pos-qa", RoleName: "qa-engineer", PersonaPath: "default/qa-engineer", Required: false, MaxInstances: 0, ReportsTo: "pos-em"},
		{ID: "pos-devops", RoleName: "devops-engineer", PersonaPath: "default/devops-engineer", Required: false, MaxInstances: 0, ReportsTo: "pos-em"},
		{ID: "pos-reviewer", RoleName: "code-reviewer", PersonaPath: "default/code-reviewer", Required: false, MaxInstances: 0, ReportsTo: "pos-em"},
		{ID: "pos-docs", RoleName: "documentation-manager", PersonaPath: "default/documentation-manager", Required: false, MaxInstances: 1, ReportsTo: "pos-pm"},
		{ID: "pos-web", RoleName: "web-designer", PersonaPath: "default/web-designer", Required: false, MaxInstances: 0, ReportsTo: "pos-pm"},
		{ID: "pos-webeng", RoleName: "web-designer-engineer", PersonaPath: "default/web-designer-engineer", Required: false, MaxInstances: 0, ReportsTo: "pos-em"},
		{ID: "pos-pr", RoleName: "public-relations-manager", PersonaPath: "default/public-relations-manager", Required: false, MaxInstances: 1, ReportsTo: "pos-ceo"},
		{ID: "pos-dm", RoleName: "decision-maker", PersonaPath: "default/decision-maker", Required: false, MaxInstances: 1, ReportsTo: "pos-ceo"},
		{ID: "pos-hk", RoleName: "housekeeping-bot", PersonaPath: "default/housekeeping-bot", Required: false, MaxInstances: 1},
		{ID: "pos-cto", RoleName: "cto", PersonaPath: "default/cto", Required: false, MaxInstances: 1, ReportsTo: "pos-ceo"},
		{ID: "pos-remspec", RoleName: "remediation-specialist", PersonaPath: "default/remediation-specialist", Required: false, MaxInstances: 0, ReportsTo: "pos-em"},
	}
}

// GetRequiredPositions returns only the positions marked as required
func (o *OrgChart) GetRequiredPositions() []Position {
	var required []Position
	for _, p := range o.Positions {
		if p.Required {
			required = append(required, p)
		}
	}
	return required
}

// GetVacantPositions returns positions that have no agents assigned
func (o *OrgChart) GetVacantPositions() []Position {
	var vacant []Position
	for _, p := range o.Positions {
		if !p.IsFilled() {
			vacant = append(vacant, p)
		}
	}
	return vacant
}

// GetPositionByRole finds a position by role name
func (o *OrgChart) GetPositionByRole(roleName string) *Position {
	for i := range o.Positions {
		if o.Positions[i].RoleName == roleName {
			return &o.Positions[i]
		}
	}
	return nil
}

// GetPositionByID finds a position by ID
func (o *OrgChart) GetPositionByID(positionID string) *Position {
	for i := range o.Positions {
		if o.Positions[i].ID == positionID {
			return &o.Positions[i]
		}
	}
	return nil
}

// AllRequiredFilled returns true if all required positions have at least one agent
func (o *OrgChart) AllRequiredFilled() bool {
	for _, p := range o.Positions {
		if p.Required && !p.IsFilled() {
			return false
		}
	}
	return true
}

// GetAllAgentIDs returns all agent IDs across all positions
func (o *OrgChart) GetAllAgentIDs() []string {
	seen := make(map[string]struct{})
	var agents []string
	for _, p := range o.Positions {
		for _, id := range p.AgentIDs {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				agents = append(agents, id)
			}
		}
	}
	return agents
}
