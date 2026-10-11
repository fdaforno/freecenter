package store

import (
	"errors"
	"time"
)

// ---- OIDC configuration ----

// OIDCConfig is the connection to an external identity provider
// (Keycloak in the example). Stored encrypted in store.json; the
// ClientSecret is sealed at rest like host passwords.
type OIDCConfig struct {
	Enabled      bool              `json:"enabled"`
	Issuer       string            `json:"issuer"`              // e.g. http://192.168.251.20:30081/realms/hostwarden
	ClientID     string            `json:"client_id"`           // encrypted at rest in saveLocked
	ClientSecret string            `json:"client_secret"`       // encrypted at rest
	Scopes       []string          `json:"scopes,omitempty"`    // default: ["openid","profile","email"]
	ClaimName    string            `json:"claim_name,omitempty"` // default: "groups"
	GroupRoles   map[string]string `json:"group_roles,omitempty"` // Keycloak group → Role ID
	DefaultRole  string            `json:"default_role,omitempty"` // role for authenticated users with no mapping
}

// DefaultScopes is used when the config has no explicit scopes.
var DefaultScopes = []string{"openid", "profile", "email"}

// EffectiveScopes returns the scopes to request, defaulting when empty.
func (c *OIDCConfig) EffectiveScopes() []string {
	if len(c.Scopes) > 0 {
		return c.Scopes
	}
	return DefaultScopes
}

// EffectiveClaimName returns the claim that carries group memberships.
func (c *OIDCConfig) EffectiveClaimName() string {
	if c.ClaimName != "" {
		return c.ClaimName
	}
	return "groups"
}

// ---- Roles with granular permissions ----

// Role defines what a user assigned to it may do. The two built-in
// roles ("admin" and "readonly") are created automatically on first
// load and cannot be deleted.
type Role struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Builtin     bool   `json:"builtin,omitempty"`

	ManageVMs     bool `json:"manage_vms"`
	ManageHosts   bool `json:"manage_hosts"`
	ManageStorage bool `json:"manage_storage"`
	ManageUsers   bool `json:"manage_users"`
	ConsoleAccess bool `json:"console_access"`
}

// Permissions is the resolved set of capabilities for a user.
type Permissions struct {
	ManageVMs     bool
	ManageHosts   bool
	ManageStorage bool
	ManageUsers   bool
	ConsoleAccess bool
}

// All returns a permission set with everything enabled (for admins).
func AllPermissions() Permissions {
	return Permissions{ManageVMs: true, ManageHosts: true, ManageStorage: true, ManageUsers: true, ConsoleAccess: true}
}

// NoPermissions returns a permission set with nothing enabled.
func NoPermissions() Permissions {
	return Permissions{}
}

// defaultRoles are created on first load.
var defaultRoles = []Role{
	{ID: RoleAdmin, Name: "Administrator", Description: "Full access to all features", Builtin: true, ManageVMs: true, ManageHosts: true, ManageStorage: true, ManageUsers: true, ConsoleAccess: true},
	{ID: RoleReadOnly, Name: "Read-only", Description: "Can view everything but change nothing", Builtin: true},
}

// ensureRoles creates the built-in roles when none exist.
func (s *Store) ensureRoles() {
	if len(s.d.Roles) > 0 {
		return
	}
	s.d.Roles = make([]Role, len(defaultRoles))
	copy(s.d.Roles, defaultRoles)
}

// ---- Store methods: OIDC ----

// OIDC returns the current OIDC configuration (or nil when unset).
func (s *Store) OIDC() *OIDCConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.OIDC == nil {
		return nil
	}
	c := *s.d.OIDC
	return &c
}

// SetOIDC replaces the OIDC configuration.
func (s *Store) SetOIDC(c *OIDCConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.OIDC = c
	return s.saveLocked()
}

// ---- Store methods: Roles ----

// Roles returns all role definitions.
func (s *Store) Roles() []Role {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Role, len(s.d.Roles))
	copy(out, s.d.Roles)
	return out
}

// Role returns the role with the given ID.
func (s *Store) Role(id string) (Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.d.Roles {
		if r.ID == id {
			return r, nil
		}
	}
	return Role{}, ErrNotFound
}

// ErrRoleBuiltin is returned when trying to delete a built-in role.
var ErrRoleBuiltin = errors.New("built-in roles cannot be deleted")

// PutRole creates (empty ID) or replaces (existing ID) a role.
func (s *Store) PutRole(r Role) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
		s.d.Roles = append(s.d.Roles, r)
	} else {
		found := false
		for i := range s.d.Roles {
			if s.d.Roles[i].ID == r.ID {
				r.Builtin = s.d.Roles[i].Builtin // preserve builtin flag
				s.d.Roles[i] = r
				found = true
				break
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteRole removes a role. Built-in roles cannot be deleted.
func (s *Store) DeleteRole(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.d.Roles {
		if r.ID == id {
			if r.Builtin {
				return ErrRoleBuiltin
			}
			s.d.Roles = append(s.d.Roles[:i], s.d.Roles[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// PermissionsFor resolves the permissions of a user based on their role.
// Legacy roles ("admin", "", "readonly") are mapped to the built-in
// definitions; custom roles are looked up by ID.
func (s *Store) PermissionsFor(u *User) Permissions {
	s.mu.Lock()
	defer s.mu.Unlock()
	roleID := u.Role
	if roleID == "" {
		roleID = RoleAdmin // legacy: no role = admin
	}
	for _, r := range s.d.Roles {
		if r.ID == roleID {
			return Permissions{
				ManageVMs:     r.ManageVMs,
				ManageHosts:   r.ManageHosts,
				ManageStorage: r.ManageStorage,
				ManageUsers:   r.ManageUsers,
				ConsoleAccess: r.ConsoleAccess,
			}
		}
	}
	// Unknown role → read-only.
	return NoPermissions()
}

// ---- Store methods: OIDC users ----

// UserByOIDCSub finds a user linked to an OIDC subject.
func (s *Store) UserByOIDCSub(sub string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].OIDCSub == sub {
			u := s.d.Users[i]
			return &u, nil
		}
	}
	return nil, ErrNotFound
}

// CreateOIDCUser creates a user authenticated via OIDC (no local password).
// If the username already exists (local account), it links the OIDC sub.
func (s *Store) CreateOIDCUser(username, sub, role string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].Username == username {
			s.d.Users[i].OIDCSub = sub
			if role != "" {
				s.d.Users[i].Role = role
			}
			u := s.d.Users[i]
			_ = s.saveLocked()
			return &u, nil
		}
	}
	u := User{ID: newID(), Username: username, OIDCSub: sub, Role: role, CreatedAt: time.Now().UTC()}
	s.d.Users = append(s.d.Users, u)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &u, nil
}

// SetOIDCSub links an existing user to an OIDC subject.
func (s *Store) SetOIDCSub(id, sub string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.userIndexLocked(id)
	if err != nil {
		return err
	}
	s.d.Users[i].OIDCSub = sub
	return s.saveLocked()
}
