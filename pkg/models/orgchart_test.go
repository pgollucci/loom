package models

import (
	"testing"
	"time"
)

// TestOrgChart_VersionedEntityInterface tests OrgChart implements VersionedEntity
func TestOrgChart_VersionedEntityInterface(t *testing.T) {
	orgChart := &OrgChart{
		EntityMetadata: EntityMetadata{
			SchemaVersion: OrgChartSchemaVersion,
		},
		ID: "org-123",
	}

	if orgChart.GetEntityType() != EntityTypeOrgChart {
		t.Errorf("GetEntityType() = %v, want %v", orgChart.GetEntityType(), EntityTypeOrgChart)
	}

	if orgChart.GetSchemaVersion() != OrgChartSchemaVersion {
		t.Errorf("GetSchemaVersion() = %v, want %v", orgChart.GetSchemaVersion(), OrgChartSchemaVersion)
	}

	orgChart.SetSchemaVersion("2.0")
	if orgChart.GetSchemaVersion() != "2.0" {
		t.Errorf("After SetSchemaVersion, GetSchemaVersion() = %v, want %v", orgChart.GetSchemaVersion(), "2.0")
	}

	if orgChart.GetEntityMetadata() == nil {
		t.Error("GetEntityMetadata() should not return nil")
	}

	if orgChart.GetID() != "org-123" {
		t.Errorf("GetID() = %v, want %v", orgChart.GetID(), "org-123")
	}
}

// TestPosition_VersionedEntityInterface tests Position implements VersionedEntity
func TestPosition_VersionedEntityInterface(t *testing.T) {
	position := &Position{
		EntityMetadata: EntityMetadata{
			SchemaVersion: PositionSchemaVersion,
		},
		ID: "pos-123",
	}

	if position.GetEntityType() != EntityTypePosition {
		t.Errorf("GetEntityType() = %v, want %v", position.GetEntityType(), EntityTypePosition)
	}

	if position.GetSchemaVersion() != PositionSchemaVersion {
		t.Errorf("GetSchemaVersion() = %v, want %v", position.GetSchemaVersion(), PositionSchemaVersion)
	}

	position.SetSchemaVersion("2.0")
	if position.GetSchemaVersion() != "2.0" {
		t.Errorf("After SetSchemaVersion, GetSchemaVersion() = %v, want %v", position.GetSchemaVersion(), "2.0")
	}

	if position.GetEntityMetadata() == nil {
		t.Error("GetEntityMetadata() should not return nil")
	}

	if position.GetID() != "pos-123" {
		t.Errorf("GetID() = %v, want %v", position.GetID(), "pos-123")
	}
}

// TestPosition_Status tests Position.Status() method
func TestPosition_Status(t *testing.T) {
	tests := []struct {
		name         string
		agentIDs     []string
		maxInstances int
		want         string
	}{
		{
			name:         "vacant - no agents",
			agentIDs:     []string{},
			maxInstances: 1,
			want:         "vacant",
		},
		{
			name:         "filled - one agent, max one",
			agentIDs:     []string{"agent-1"},
			maxInstances: 1,
			want:         "filled",
		},
		{
			name:         "partial - one agent, max three",
			agentIDs:     []string{"agent-1"},
			maxInstances: 3,
			want:         "partial",
		},
		{
			name:         "filled - three agents, max three",
			agentIDs:     []string{"agent-1", "agent-2", "agent-3"},
			maxInstances: 3,
			want:         "filled",
		},
		{
			name:         "filled - unlimited (MaxInstances=0)",
			agentIDs:     []string{"agent-1"},
			maxInstances: 0,
			want:         "filled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Position{
				AgentIDs:     tt.agentIDs,
				MaxInstances: tt.maxInstances,
			}
			got := p.Status()
			if got != tt.want {
				t.Errorf("Status() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPosition_IsFilled tests Position.IsFilled() method
func TestPosition_IsFilled(t *testing.T) {
	tests := []struct {
		name     string
		agentIDs []string
		want     bool
	}{
		{
			name:     "not filled - empty",
			agentIDs: []string{},
			want:     false,
		},
		{
			name:     "not filled - nil",
			agentIDs: nil,
			want:     false,
		},
		{
			name:     "filled - one agent",
			agentIDs: []string{"agent-1"},
			want:     true,
		},
		{
			name:     "filled - multiple agents",
			agentIDs: []string{"agent-1", "agent-2", "agent-3"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Position{
				AgentIDs: tt.agentIDs,
			}
			got := p.IsFilled()
			if got != tt.want {
				t.Errorf("IsFilled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPosition_CanAddAgent tests Position.CanAddAgent() method
func TestPosition_CanAddAgent(t *testing.T) {
	tests := []struct {
		name         string
		agentIDs     []string
		maxInstances int
		want         bool
	}{
		{
			name:         "can add - unlimited",
			agentIDs:     []string{"agent-1"},
			maxInstances: 0,
			want:         true,
		},
		{
			name:         "can add - below max",
			agentIDs:     []string{"agent-1"},
			maxInstances: 3,
			want:         true,
		},
		{
			name:         "cannot add - at max",
			agentIDs:     []string{"agent-1", "agent-2", "agent-3"},
			maxInstances: 3,
			want:         false,
		},
		{
			name:         "cannot add - above max",
			agentIDs:     []string{"agent-1", "agent-2", "agent-3", "agent-4"},
			maxInstances: 3,
			want:         false,
		},
		{
			name:         "can add - empty",
			agentIDs:     []string{},
			maxInstances: 1,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Position{
				AgentIDs:     tt.agentIDs,
				MaxInstances: tt.maxInstances,
			}
			got := p.CanAddAgent()
			if got != tt.want {
				t.Errorf("CanAddAgent() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPosition_HasAgent tests Position.HasAgent() method
func TestPosition_HasAgent(t *testing.T) {
	p := &Position{
		AgentIDs: []string{"agent-1", "agent-2", "agent-3"},
	}

	tests := []struct {
		name    string
		agentID string
		want    bool
	}{
		{
			name:    "has agent-1",
			agentID: "agent-1",
			want:    true,
		},
		{
			name:    "has agent-2",
			agentID: "agent-2",
			want:    true,
		},
		{
			name:    "has agent-3",
			agentID: "agent-3",
			want:    true,
		},
		{
			name:    "does not have agent-4",
			agentID: "agent-4",
			want:    false,
		},
		{
			name:    "does not have empty string",
			agentID: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.HasAgent(tt.agentID)
			if got != tt.want {
				t.Errorf("HasAgent(%q) = %v, want %v", tt.agentID, got, tt.want)
			}
		})
	}
}

// TestPosition_HasAgent_EmptyList tests HasAgent with empty AgentIDs
func TestPosition_HasAgent_EmptyList(t *testing.T) {
	p := &Position{
		AgentIDs: []string{},
	}

	if p.HasAgent("agent-1") {
		t.Error("HasAgent should return false for empty AgentIDs list")
	}
}

// TestDefaultOrgChartPositions tests DefaultOrgChartPositions function
func TestDefaultOrgChartPositions(t *testing.T) {
	positions := DefaultOrgChartPositions()

	if len(positions) == 0 {
		t.Fatal("DefaultOrgChartPositions should return non-empty list")
	}

	// Check that all positions have IDs
	for i, p := range positions {
		if p.ID == "" {
			t.Errorf("Position %d has empty ID", i)
		}
		if p.RoleName == "" {
			t.Errorf("Position %d has empty RoleName", i)
		}
		if p.PersonaPath == "" {
			t.Errorf("Position %d has empty PersonaPath", i)
		}
	}

	// Check for expected key positions
	foundCEO := false
	foundPM := false
	foundEM := false

	for _, p := range positions {
		if p.RoleName == "ceo" {
			foundCEO = true
			if !p.Required {
				t.Error("CEO position should be required")
			}
			if p.MaxInstances != 1 {
				t.Errorf("CEO MaxInstances = %d, want 1", p.MaxInstances)
			}
		}
		if p.RoleName == "product-manager" {
			foundPM = true
		}
		if p.RoleName == "engineering-manager" {
			foundEM = true
		}
	}

	if !foundCEO {
		t.Error("Default positions should include CEO")
	}
	if !foundPM {
		t.Error("Default positions should include product-manager")
	}
	if !foundEM {
		t.Error("Default positions should include engineering-manager")
	}
}

// TestOrgChart_GetRequiredPositions tests OrgChart.GetRequiredPositions() method
func TestOrgChart_GetRequiredPositions(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{ID: "pos-1", RoleName: "ceo", Required: true},
			{ID: "pos-2", RoleName: "cfo", Required: false},
			{ID: "pos-3", RoleName: "pm", Required: true},
			{ID: "pos-4", RoleName: "dev", Required: false},
		},
	}

	required := orgChart.GetRequiredPositions()

	if len(required) != 2 {
		t.Fatalf("GetRequiredPositions() returned %d positions, want 2", len(required))
	}

	// Check that all returned positions are required
	for _, p := range required {
		if !p.Required {
			t.Errorf("GetRequiredPositions() returned non-required position %s", p.ID)
		}
	}

	// Check that we got the right positions
	if required[0].ID != "pos-1" && required[1].ID != "pos-1" {
		t.Error("GetRequiredPositions() should include pos-1")
	}
	if required[0].ID != "pos-3" && required[1].ID != "pos-3" {
		t.Error("GetRequiredPositions() should include pos-3")
	}
}

// TestOrgChart_GetRequiredPositions_Empty tests with no required positions
func TestOrgChart_GetRequiredPositions_Empty(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{ID: "pos-1", Required: false},
			{ID: "pos-2", Required: false},
		},
	}

	required := orgChart.GetRequiredPositions()

	if len(required) != 0 {
		t.Errorf("GetRequiredPositions() returned %d positions, want 0", len(required))
	}
}

// TestOrgChart_GetVacantPositions tests OrgChart.GetVacantPositions() method
func TestOrgChart_GetVacantPositions(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{ID: "pos-1", AgentIDs: []string{"agent-1"}},       // filled
			{ID: "pos-2", AgentIDs: []string{}},                // vacant
			{ID: "pos-3", AgentIDs: []string{"agent-2"}},       // filled
			{ID: "pos-4", AgentIDs: nil},                       // vacant
			{ID: "pos-5", AgentIDs: []string{"a1", "a2", "a3"}}, // filled
		},
	}

	vacant := orgChart.GetVacantPositions()

	if len(vacant) != 2 {
		t.Fatalf("GetVacantPositions() returned %d positions, want 2", len(vacant))
	}

	// Check that all returned positions are vacant
	for _, p := range vacant {
		if p.IsFilled() {
			t.Errorf("GetVacantPositions() returned filled position %s", p.ID)
		}
	}
}

// TestOrgChart_GetVacantPositions_AllFilled tests with all positions filled
func TestOrgChart_GetVacantPositions_AllFilled(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{ID: "pos-1", AgentIDs: []string{"agent-1"}},
			{ID: "pos-2", AgentIDs: []string{"agent-2"}},
		},
	}

	vacant := orgChart.GetVacantPositions()

	if len(vacant) != 0 {
		t.Errorf("GetVacantPositions() returned %d positions, want 0", len(vacant))
	}
}

// TestOrgChart_GetPositionByRole tests OrgChart.GetPositionByRole() method
func TestOrgChart_GetPositionByRole(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{ID: "pos-1", RoleName: "ceo"},
			{ID: "pos-2", RoleName: "cfo"},
			{ID: "pos-3", RoleName: "product-manager"},
		},
	}

	tests := []struct {
		name     string
		roleName string
		wantID   string
		wantNil  bool
	}{
		{
			name:     "find ceo",
			roleName: "ceo",
			wantID:   "pos-1",
			wantNil:  false,
		},
		{
			name:     "find cfo",
			roleName: "cfo",
			wantID:   "pos-2",
			wantNil:  false,
		},
		{
			name:     "find product-manager",
			roleName: "product-manager",
			wantID:   "pos-3",
			wantNil:  false,
		},
		{
			name:     "not found",
			roleName: "nonexistent",
			wantID:   "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orgChart.GetPositionByRole(tt.roleName)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetPositionByRole(%q) = %v, want nil", tt.roleName, got)
				}
			} else {
				if got == nil {
					t.Fatalf("GetPositionByRole(%q) = nil, want non-nil", tt.roleName)
				}
				if got.ID != tt.wantID {
					t.Errorf("GetPositionByRole(%q).ID = %q, want %q", tt.roleName, got.ID, tt.wantID)
				}
			}
		})
	}
}

// TestOrgChart_GetPositionByID tests OrgChart.GetPositionByID() method
func TestOrgChart_GetPositionByID(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{ID: "pos-1", RoleName: "ceo"},
			{ID: "pos-2", RoleName: "cfo"},
			{ID: "pos-3", RoleName: "product-manager"},
		},
	}

	tests := []struct {
		name       string
		positionID string
		wantRole   string
		wantNil    bool
	}{
		{
			name:       "find pos-1",
			positionID: "pos-1",
			wantRole:   "ceo",
			wantNil:    false,
		},
		{
			name:       "find pos-2",
			positionID: "pos-2",
			wantRole:   "cfo",
			wantNil:    false,
		},
		{
			name:       "find pos-3",
			positionID: "pos-3",
			wantRole:   "product-manager",
			wantNil:    false,
		},
		{
			name:       "not found",
			positionID: "pos-99",
			wantRole:   "",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orgChart.GetPositionByID(tt.positionID)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetPositionByID(%q) = %v, want nil", tt.positionID, got)
				}
			} else {
				if got == nil {
					t.Fatalf("GetPositionByID(%q) = nil, want non-nil", tt.positionID)
				}
				if got.RoleName != tt.wantRole {
					t.Errorf("GetPositionByID(%q).RoleName = %q, want %q", tt.positionID, got.RoleName, tt.wantRole)
				}
			}
		})
	}
}

// TestOrgChart_AllRequiredFilled tests OrgChart.AllRequiredFilled() method
func TestOrgChart_AllRequiredFilled(t *testing.T) {
	tests := []struct {
		name      string
		positions []Position
		want      bool
	}{
		{
			name: "all required filled",
			positions: []Position{
				{Required: true, AgentIDs: []string{"agent-1"}},
				{Required: true, AgentIDs: []string{"agent-2"}},
				{Required: false, AgentIDs: []string{}},
			},
			want: true,
		},
		{
			name: "one required unfilled",
			positions: []Position{
				{Required: true, AgentIDs: []string{"agent-1"}},
				{Required: true, AgentIDs: []string{}}, // unfilled
				{Required: false, AgentIDs: []string{}},
			},
			want: false,
		},
		{
			name: "no required positions",
			positions: []Position{
				{Required: false, AgentIDs: []string{}},
				{Required: false, AgentIDs: []string{"agent-1"}},
			},
			want: true,
		},
		{
			name: "all required unfilled",
			positions: []Position{
				{Required: true, AgentIDs: []string{}},
				{Required: true, AgentIDs: nil},
			},
			want: false,
		},
		{
			name:      "empty positions",
			positions: []Position{},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgChart := &OrgChart{
				Positions: tt.positions,
			}
			got := orgChart.AllRequiredFilled()
			if got != tt.want {
				t.Errorf("AllRequiredFilled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOrgChart_GetAllAgentIDs tests OrgChart.GetAllAgentIDs() method
func TestOrgChart_GetAllAgentIDs(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{AgentIDs: []string{"agent-1", "agent-2"}},
			{AgentIDs: []string{"agent-2", "agent-3"}}, // agent-2 is duplicate
			{AgentIDs: []string{"agent-4"}},
			{AgentIDs: []string{}}, // empty
		},
	}

	allAgents := orgChart.GetAllAgentIDs()

	// Should have unique agents only
	if len(allAgents) != 4 {
		t.Errorf("GetAllAgentIDs() returned %d agents, want 4", len(allAgents))
	}

	// Check that all expected agents are present
	agentMap := make(map[string]bool)
	for _, id := range allAgents {
		agentMap[id] = true
	}

	expectedAgents := []string{"agent-1", "agent-2", "agent-3", "agent-4"}
	for _, expectedID := range expectedAgents {
		if !agentMap[expectedID] {
			t.Errorf("GetAllAgentIDs() missing agent %q", expectedID)
		}
	}
}

// TestOrgChart_GetAllAgentIDs_Empty tests GetAllAgentIDs with no agents
func TestOrgChart_GetAllAgentIDs_Empty(t *testing.T) {
	orgChart := &OrgChart{
		Positions: []Position{
			{AgentIDs: []string{}},
			{AgentIDs: nil},
		},
	}

	allAgents := orgChart.GetAllAgentIDs()

	if len(allAgents) != 0 {
		t.Errorf("GetAllAgentIDs() returned %d agents, want 0", len(allAgents))
	}
}

// TestOrgChart_Struct tests OrgChart struct fields
func TestOrgChart_Struct(t *testing.T) {
	now := time.Now()
	orgChart := &OrgChart{
		ID:         "org-123",
		ProjectID:  "proj-456",
		Name:       "Engineering Team",
		Positions:  []Position{},
		IsTemplate: true,
		ParentID:   "org-parent",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if orgChart.ID != "org-123" {
		t.Errorf("ID = %q, want %q", orgChart.ID, "org-123")
	}

	if orgChart.ProjectID != "proj-456" {
		t.Errorf("ProjectID = %q, want %q", orgChart.ProjectID, "proj-456")
	}

	if orgChart.Name != "Engineering Team" {
		t.Errorf("Name = %q, want %q", orgChart.Name, "Engineering Team")
	}

	if !orgChart.IsTemplate {
		t.Error("IsTemplate should be true")
	}

	if orgChart.ParentID != "org-parent" {
		t.Errorf("ParentID = %q, want %q", orgChart.ParentID, "org-parent")
	}
}

// TestPosition_Struct tests Position struct fields
func TestPosition_Struct(t *testing.T) {
	now := time.Now()
	position := &Position{
		ID:           "pos-123",
		RoleName:     "ceo",
		PersonaPath:  "default/ceo",
		Required:     true,
		MaxInstances: 1,
		AgentIDs:     []string{"agent-1"},
		ReportsTo:    "pos-parent",
		CreatedAt:    now,
	}

	if position.ID != "pos-123" {
		t.Errorf("ID = %q, want %q", position.ID, "pos-123")
	}

	if position.RoleName != "ceo" {
		t.Errorf("RoleName = %q, want %q", position.RoleName, "ceo")
	}

	if position.PersonaPath != "default/ceo" {
		t.Errorf("PersonaPath = %q, want %q", position.PersonaPath, "default/ceo")
	}

	if !position.Required {
		t.Error("Required should be true")
	}

	if position.MaxInstances != 1 {
		t.Errorf("MaxInstances = %d, want 1", position.MaxInstances)
	}

	if len(position.AgentIDs) != 1 {
		t.Errorf("AgentIDs length = %d, want 1", len(position.AgentIDs))
	}

	if position.ReportsTo != "pos-parent" {
		t.Errorf("ReportsTo = %q, want %q", position.ReportsTo, "pos-parent")
	}
}
