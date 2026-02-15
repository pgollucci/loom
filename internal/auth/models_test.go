package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestClaims_GetIssuedAt tests Claims.GetIssuedAt() method
func TestClaims_GetIssuedAt(t *testing.T) {
	now := time.Now()
	numericDate := jwt.NewNumericDate(now)

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: numericDate,
		},
	}

	issuedAt, err := claims.GetIssuedAt()
	if err != nil {
		t.Fatalf("GetIssuedAt() error = %v", err)
	}

	if issuedAt != numericDate {
		t.Errorf("GetIssuedAt() = %v, want %v", issuedAt, numericDate)
	}
}

// TestClaims_GetIssuedAt_Nil tests GetIssuedAt with nil IssuedAt
func TestClaims_GetIssuedAt_Nil(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: nil,
		},
	}

	issuedAt, err := claims.GetIssuedAt()
	if err != nil {
		t.Fatalf("GetIssuedAt() error = %v", err)
	}

	if issuedAt != nil {
		t.Errorf("GetIssuedAt() = %v, want nil", issuedAt)
	}
}

// TestClaims_GetIssuer tests Claims.GetIssuer() method
func TestClaims_GetIssuer(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "loom-auth-service",
		},
	}

	issuer, err := claims.GetIssuer()
	if err != nil {
		t.Fatalf("GetIssuer() error = %v", err)
	}

	if issuer != "loom-auth-service" {
		t.Errorf("GetIssuer() = %q, want %q", issuer, "loom-auth-service")
	}
}

// TestClaims_GetIssuer_Empty tests GetIssuer with empty issuer
func TestClaims_GetIssuer_Empty(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "",
		},
	}

	issuer, err := claims.GetIssuer()
	if err != nil {
		t.Fatalf("GetIssuer() error = %v", err)
	}

	if issuer != "" {
		t.Errorf("GetIssuer() = %q, want empty string", issuer)
	}
}

// TestClaims_GetSubject tests Claims.GetSubject() method
func TestClaims_GetSubject(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
		},
	}

	subject, err := claims.GetSubject()
	if err != nil {
		t.Fatalf("GetSubject() error = %v", err)
	}

	if subject != "user-123" {
		t.Errorf("GetSubject() = %q, want %q", subject, "user-123")
	}
}

// TestClaims_GetSubject_Empty tests GetSubject with empty subject
func TestClaims_GetSubject_Empty(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "",
		},
	}

	subject, err := claims.GetSubject()
	if err != nil {
		t.Fatalf("GetSubject() error = %v", err)
	}

	if subject != "" {
		t.Errorf("GetSubject() = %q, want empty string", subject)
	}
}

// TestClaims_GetAudience tests Claims.GetAudience() method
func TestClaims_GetAudience(t *testing.T) {
	expectedAudience := jwt.ClaimStrings{"loom-api", "loom-web"}
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: expectedAudience,
		},
	}

	audience, err := claims.GetAudience()
	if err != nil {
		t.Fatalf("GetAudience() error = %v", err)
	}

	if len(audience) != len(expectedAudience) {
		t.Fatalf("GetAudience() length = %d, want %d", len(audience), len(expectedAudience))
	}

	for i, aud := range expectedAudience {
		if audience[i] != aud {
			t.Errorf("GetAudience()[%d] = %q, want %q", i, audience[i], aud)
		}
	}
}

// TestClaims_GetAudience_Empty tests GetAudience with empty audience
func TestClaims_GetAudience_Empty(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: jwt.ClaimStrings{},
		},
	}

	audience, err := claims.GetAudience()
	if err != nil {
		t.Fatalf("GetAudience() error = %v", err)
	}

	if len(audience) != 0 {
		t.Errorf("GetAudience() length = %d, want 0", len(audience))
	}
}

// TestClaims_GetAudience_Nil tests GetAudience with nil audience
func TestClaims_GetAudience_Nil(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: nil,
		},
	}

	audience, err := claims.GetAudience()
	if err != nil {
		t.Fatalf("GetAudience() error = %v", err)
	}

	if audience != nil {
		t.Errorf("GetAudience() = %v, want nil", audience)
	}
}

// TestClaims_CustomFields tests custom fields in Claims
func TestClaims_CustomFields(t *testing.T) {
	claims := &Claims{
		UserID:      "user-123",
		Username:    "john.doe",
		Role:        "admin",
		Permissions: []string{"agents:read", "beads:write"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:  "loom",
			Subject: "user-123",
		},
	}

	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-123")
	}

	if claims.Username != "john.doe" {
		t.Errorf("Username = %q, want %q", claims.Username, "john.doe")
	}

	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}

	if len(claims.Permissions) != 2 {
		t.Errorf("Permissions length = %d, want 2", len(claims.Permissions))
	}
}

// TestUser_Struct tests User struct
func TestUser_Struct(t *testing.T) {
	now := time.Now()
	user := User{
		ID:        "user-123",
		Username:  "testuser",
		Email:     "test@example.com",
		Role:      "admin",
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if user.ID != "user-123" {
		t.Errorf("ID = %q, want %q", user.ID, "user-123")
	}

	if user.Username != "testuser" {
		t.Errorf("Username = %q, want %q", user.Username, "testuser")
	}

	if user.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", user.Email, "test@example.com")
	}

	if user.Role != "admin" {
		t.Errorf("Role = %q, want %q", user.Role, "admin")
	}

	if !user.IsActive {
		t.Error("IsActive should be true")
	}
}

// TestToken_Struct tests Token struct
func TestToken_Struct(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(1 * time.Hour)

	token := Token{
		ID:        "tok-123",
		UserID:    "user-123",
		Token:     "secret-token",
		ExpiresAt: expiresAt,
		CreatedAt: now,
		LastUsed:  now,
	}

	if token.ID != "tok-123" {
		t.Errorf("ID = %q, want %q", token.ID, "tok-123")
	}

	if token.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", token.UserID, "user-123")
	}

	if token.Token != "secret-token" {
		t.Errorf("Token = %q, want %q", token.Token, "secret-token")
	}

	if !token.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", token.ExpiresAt, expiresAt)
	}
}

// TestAPIKey_Struct tests APIKey struct
func TestAPIKey_Struct(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)

	apiKey := APIKey{
		ID:          "key-123",
		Name:        "Test API Key",
		UserID:      "user-123",
		KeyPrefix:   "loom_abc",
		KeyHash:     "hashed-secret",
		Permissions: []string{"agents:read", "beads:write"},
		IsActive:    true,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		LastUsed:    now,
	}

	if apiKey.ID != "key-123" {
		t.Errorf("ID = %q, want %q", apiKey.ID, "key-123")
	}

	if apiKey.Name != "Test API Key" {
		t.Errorf("Name = %q, want %q", apiKey.Name, "Test API Key")
	}

	if apiKey.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", apiKey.UserID, "user-123")
	}

	if apiKey.KeyPrefix != "loom_abc" {
		t.Errorf("KeyPrefix = %q, want %q", apiKey.KeyPrefix, "loom_abc")
	}

	if !apiKey.IsActive {
		t.Error("IsActive should be true")
	}

	if len(apiKey.Permissions) != 2 {
		t.Errorf("Permissions length = %d, want 2", len(apiKey.Permissions))
	}
}

// TestRole_Struct tests Role struct
func TestRole_Struct(t *testing.T) {
	role := Role{
		Name:        "admin",
		Description: "Administrator role",
		Permissions: []string{"*:*"},
	}

	if role.Name != "admin" {
		t.Errorf("Name = %q, want %q", role.Name, "admin")
	}

	if role.Description != "Administrator role" {
		t.Errorf("Description = %q, want %q", role.Description, "Administrator role")
	}

	if len(role.Permissions) != 1 {
		t.Errorf("Permissions length = %d, want 1", len(role.Permissions))
	}
}

// TestPermission_Struct tests Permission struct
func TestPermission_Struct(t *testing.T) {
	perm := Permission{
		Name:        "agents:read",
		Description: "Read agent information",
		Resource:    "agents",
		Action:      "read",
	}

	if perm.Name != "agents:read" {
		t.Errorf("Name = %q, want %q", perm.Name, "agents:read")
	}

	if perm.Resource != "agents" {
		t.Errorf("Resource = %q, want %q", perm.Resource, "agents")
	}

	if perm.Action != "read" {
		t.Errorf("Action = %q, want %q", perm.Action, "read")
	}
}

// TestPreDefinedRoles tests predefined roles
func TestPreDefinedRoles(t *testing.T) {
	expectedRoles := []string{"admin", "user", "viewer", "service"}

	for _, roleName := range expectedRoles {
		role, ok := PreDefinedRoles[roleName]
		if !ok {
			t.Errorf("PreDefinedRoles missing %q", roleName)
			continue
		}

		if role.Name != roleName {
			t.Errorf("Role %q has Name = %q, want %q", roleName, role.Name, roleName)
		}

		if role.Description == "" {
			t.Errorf("Role %q has empty Description", roleName)
		}
	}

	// Check admin role has all permissions
	admin := PreDefinedRoles["admin"]
	if len(admin.Permissions) != 1 || admin.Permissions[0] != "*:*" {
		t.Error("Admin role should have '*:*' permission")
	}
}

// TestPreDefinedPermissions tests predefined permissions
func TestPreDefinedPermissions(t *testing.T) {
	if len(PreDefinedPermissions) == 0 {
		t.Fatal("PreDefinedPermissions should not be empty")
	}

	// Check that all permissions have required fields
	for i, perm := range PreDefinedPermissions {
		if perm.Name == "" {
			t.Errorf("Permission %d has empty Name", i)
		}

		if perm.Resource == "" {
			t.Errorf("Permission %d (%s) has empty Resource", i, perm.Name)
		}

		if perm.Action == "" {
			t.Errorf("Permission %d (%s) has empty Action", i, perm.Name)
		}
	}

	// Check for expected permissions
	expectedPerms := map[string]bool{
		"agents:read":   false,
		"beads:write":   false,
		"projects:read": false,
		"*:*":           false,
	}

	for _, perm := range PreDefinedPermissions {
		if _, ok := expectedPerms[perm.Name]; ok {
			expectedPerms[perm.Name] = true
		}
	}

	for permName, found := range expectedPerms {
		if !found {
			t.Errorf("Expected permission %q not found in PreDefinedPermissions", permName)
		}
	}
}

// TestLoginRequest_Struct tests LoginRequest struct
func TestLoginRequest_Struct(t *testing.T) {
	req := LoginRequest{
		Username: "testuser",
		Password: "testpass",
	}

	if req.Username != "testuser" {
		t.Errorf("Username = %q, want %q", req.Username, "testuser")
	}

	if req.Password != "testpass" {
		t.Errorf("Password = %q, want %q", req.Password, "testpass")
	}
}

// TestLoginResponse_Struct tests LoginResponse struct
func TestLoginResponse_Struct(t *testing.T) {
	user := User{
		ID:       "user-123",
		Username: "testuser",
		Role:     "admin",
	}

	resp := LoginResponse{
		Token:     "jwt-token",
		ExpiresIn: 3600,
		User:      user,
	}

	if resp.Token != "jwt-token" {
		t.Errorf("Token = %q, want %q", resp.Token, "jwt-token")
	}

	if resp.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d, want 3600", resp.ExpiresIn)
	}

	if resp.User.ID != "user-123" {
		t.Errorf("User.ID = %q, want %q", resp.User.ID, "user-123")
	}
}

// TestCreateAPIKeyRequest_Struct tests CreateAPIKeyRequest struct
func TestCreateAPIKeyRequest_Struct(t *testing.T) {
	req := CreateAPIKeyRequest{
		Name:        "Test Key",
		Permissions: []string{"agents:read"},
		ExpiresIn:   86400,
	}

	if req.Name != "Test Key" {
		t.Errorf("Name = %q, want %q", req.Name, "Test Key")
	}

	if len(req.Permissions) != 1 {
		t.Errorf("Permissions length = %d, want 1", len(req.Permissions))
	}

	if req.ExpiresIn != 86400 {
		t.Errorf("ExpiresIn = %d, want 86400", req.ExpiresIn)
	}
}

// TestCreateAPIKeyResponse_Struct tests CreateAPIKeyResponse struct
func TestCreateAPIKeyResponse_Struct(t *testing.T) {
	now := time.Now()
	resp := CreateAPIKeyResponse{
		ID:        "key-123",
		Name:      "Test Key",
		Key:       "loom_abc123xyz",
		ExpiresAt: &now,
	}

	if resp.ID != "key-123" {
		t.Errorf("ID = %q, want %q", resp.ID, "key-123")
	}

	if resp.Name != "Test Key" {
		t.Errorf("Name = %q, want %q", resp.Name, "Test Key")
	}

	if resp.Key != "loom_abc123xyz" {
		t.Errorf("Key = %q, want %q", resp.Key, "loom_abc123xyz")
	}

	if resp.ExpiresAt == nil {
		t.Error("ExpiresAt should not be nil")
	}
}

// TestChangePasswordRequest_Struct tests ChangePasswordRequest struct
func TestChangePasswordRequest_Struct(t *testing.T) {
	req := ChangePasswordRequest{
		CurrentPassword: "oldpass",
		NewPassword:     "newpass",
	}

	if req.CurrentPassword != "oldpass" {
		t.Errorf("CurrentPassword = %q, want %q", req.CurrentPassword, "oldpass")
	}

	if req.NewPassword != "newpass" {
		t.Errorf("NewPassword = %q, want %q", req.NewPassword, "newpass")
	}
}
