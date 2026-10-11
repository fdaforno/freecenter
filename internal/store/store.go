package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"hostwarden/internal/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrBadCreds = errors.New("invalid credentials")
)

// Roles. A user without a role (created before roles existed) is an administrator.
const (
	RoleAdmin    = "admin"
	RoleReadOnly = "readonly"
)

// MinPasswordLength applies to passwords set through the API.
const MinPasswordLength = 8

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Role         string    `json:"role,omitempty"`
	OIDCSub      string    `json:"oidc_sub,omitempty"` // linked OIDC subject (no local password when set)
	CreatedAt    time.Time `json:"created_at"`
}

// IsAdmin reports whether the user may change things.
func (u *User) IsAdmin() bool { return u.Role == "" || u.Role == RoleAdmin }

// RoleName is the role as shown to clients (never empty).
func (u *User) RoleName() string {
	if u.IsAdmin() {
		return RoleAdmin
	}
	return u.Role
}

type Token struct {
	Hash      string    `json:"hash"` // sha256 hex of the bearer token
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

type data struct {
	Hosts     []model.HostRecord `json:"hosts"`
	Users     []User             `json:"users"`
	Tokens    []Token            `json:"tokens"`
	Schedules []model.Schedule   `json:"schedules,omitempty"`
	OIDC      *OIDCConfig        `json:"oidc,omitempty"`
	Roles     []Role             `json:"roles,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	d    data
	box  *secretBox // encrypts host passwords at rest
	// tokensDirty is set when only token "last used" times changed: those are
	// flushed with the next real change instead of rewriting the file per request.
	tokensDirty bool
}

// Open loads the store. Host passwords are kept encrypted in the file with a
// key held outside it (see loadSecretBox); a file from an older version, with
// plain-text passwords, is rewritten encrypted right away.
func Open(path string) (*Store, error) {
	box, err := loadSecretBox(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, box: box}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, err
	}
	plain := false
	for i := range s.d.Hosts {
		pw, wasEncrypted, err := box.open(s.d.Hosts[i].Password)
		if err != nil {
			return nil, fmt.Errorf("decrypt the password of host %s: %w (is this the right secret key?)", s.d.Hosts[i].Name, err)
		}
		s.d.Hosts[i].Password = pw
		plain = plain || (!wasEncrypted && pw != "")
	}
	if plain {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	// Decrypt the OIDC client secret, if any.
	if s.d.OIDC != nil && s.d.OIDC.ClientSecret != "" {
		pw, wasEncrypted, err := box.open(s.d.OIDC.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("decrypt the OIDC client secret: %w (is this the right secret key?)", err)
		}
		s.d.OIDC.ClientSecret = pw
		if !wasEncrypted && pw != "" {
			plain = true
		}
	}
	if plain {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	s.ensureRoles()
	return s, nil
}

func (s *Store) saveLocked() error {
	// Encrypt on a copy: in memory the passwords stay usable.
	onDisk := s.d
	onDisk.Hosts = make([]model.HostRecord, len(s.d.Hosts))
	for i, h := range s.d.Hosts {
		enc, err := s.box.seal(h.Password)
		if err != nil {
			return err
		}
		h.Password = enc
		onDisk.Hosts[i] = h
	}
	// Encrypt the OIDC client secret on the copy too.
	if s.d.OIDC != nil && s.d.OIDC.ClientSecret != "" {
		enc, err := s.box.seal(s.d.OIDC.ClientSecret)
		if err != nil {
			return err
		}
		oidcCopy := *s.d.OIDC
		oidcCopy.ClientSecret = enc
		onDisk.OIDC = &oidcCopy
	}
	s.tokensDirty = false
	b, err := json.MarshalIndent(&onDisk, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// ---- hosts ----

func (s *Store) Hosts() []model.HostRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.HostRecord, len(s.d.Hosts))
	copy(out, s.d.Hosts)
	return out
}

func (s *Store) Host(id string) (*model.HostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Hosts {
		if s.d.Hosts[i].ID == id {
			h := s.d.Hosts[i]
			return &h, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) HostByAddress(addr string) (*model.HostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Hosts {
		if s.d.Hosts[i].Address == addr {
			h := s.d.Hosts[i]
			return &h, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) AddHost(h model.HostRecord) (*model.HostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, err := s.hostByAddressLocked(h.Address); err == nil {
		_ = existing
		return nil, ErrConflict
	}
	h.ID = newID()
	h.CreatedAt = time.Now().UTC()
	s.d.Hosts = append(s.d.Hosts, h)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Store) hostByAddressLocked(addr string) (*model.HostRecord, error) {
	for i := range s.d.Hosts {
		if s.d.Hosts[i].Address == addr {
			h := s.d.Hosts[i]
			return &h, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) hostIndexLocked(id string) (int, error) {
	for i := range s.d.Hosts {
		if s.d.Hosts[i].ID == id {
			return i, nil
		}
	}
	return -1, ErrNotFound
}

func (s *Store) UpdateHost(id string, upd func(*model.HostRecord)) (*model.HostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.hostIndexLocked(id)
	if err != nil {
		return nil, err
	}
	upd(&s.d.Hosts[i])
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	h := s.d.Hosts[i]
	return &h, nil
}

func (s *Store) DeleteHost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.hostIndexLocked(id)
	if err != nil {
		return err
	}
	s.d.Hosts = append(s.d.Hosts[:i], s.d.Hosts[i+1:]...)
	return s.saveLocked()
}

// ---- schedules ----

func (s *Store) Schedules() []model.Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Schedule, len(s.d.Schedules))
	copy(out, s.d.Schedules)
	return out
}

// PutSchedule adds a schedule (empty ID) or replaces the one with the same ID.
func (s *Store) PutSchedule(sc model.Schedule) (*model.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc.ID == "" {
		sc.ID = newID()
		s.d.Schedules = append(s.d.Schedules, sc)
	} else {
		found := false
		for i := range s.d.Schedules {
			if s.d.Schedules[i].ID == sc.ID {
				s.d.Schedules[i], found = sc, true
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &sc, nil
}

// UpdateSchedule applies fn to a stored schedule (run bookkeeping).
func (s *Store) UpdateSchedule(id string, fn func(*model.Schedule)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Schedules {
		if s.d.Schedules[i].ID == id {
			fn(&s.d.Schedules[i])
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Schedules {
		if s.d.Schedules[i].ID == id {
			s.d.Schedules = append(s.d.Schedules[:i], s.d.Schedules[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// ---- users & tokens ----

func (s *Store) Users() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, len(s.d.Users))
	copy(out, s.d.Users)
	return out
}

func (s *Store) CreateUser(username, password, role string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.d.Users {
		if u.Username == username {
			return nil, ErrConflict
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := User{ID: newID(), Username: username, PasswordHash: string(hash), Role: role, CreatedAt: time.Now().UTC()}
	s.d.Users = append(s.d.Users, u)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &u, nil
}

// EnsureAdmin creates the default admin user when no users exist.
func (s *Store) EnsureAdmin(username, password string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.d.Users) > 0 {
		return false, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	s.d.Users = append(s.d.Users, User{ID: newID(), Username: username, PasswordHash: string(hash), CreatedAt: time.Now().UTC()})
	if err := s.saveLocked(); err != nil {
		return false, err
	}
	return true, nil
}

// ErrLastAdmin protects the last administrator from being removed or demoted.
var ErrLastAdmin = errors.New("at least one administrator is required")

func (s *Store) userIndexLocked(id string) (int, error) {
	for i := range s.d.Users {
		if s.d.Users[i].ID == id {
			return i, nil
		}
	}
	return -1, ErrNotFound
}

func (s *Store) otherAdminsLocked(exceptID string) int {
	n := 0
	for i := range s.d.Users {
		if s.d.Users[i].ID != exceptID && s.d.Users[i].IsAdmin() {
			n++
		}
	}
	return n
}

// dropTokensLocked signs a user out everywhere.
func (s *Store) dropTokensLocked(userID string) {
	kept := s.d.Tokens[:0]
	for _, t := range s.d.Tokens {
		if t.UserID != userID {
			kept = append(kept, t)
		}
	}
	s.d.Tokens = kept
}

// SetUserRole changes a user's role.
func (s *Store) SetUserRole(id, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.userIndexLocked(id)
	if err != nil {
		return err
	}
	if role != RoleAdmin && s.d.Users[i].IsAdmin() && s.otherAdminsLocked(id) == 0 {
		return ErrLastAdmin
	}
	s.d.Users[i].Role = role
	return s.saveLocked()
}

// SetUserPassword replaces a user's password and signs them out of every
// session except keepToken (their current one, when they change it themselves).
func (s *Store) SetUserPassword(id, password, keepToken string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.userIndexLocked(id)
	if err != nil {
		return err
	}
	s.d.Users[i].PasswordHash = string(hash)
	keep := ""
	if keepToken != "" {
		keep = hashToken(keepToken)
	}
	kept := s.d.Tokens[:0]
	for _, t := range s.d.Tokens {
		if t.UserID != id || t.Hash == keep {
			kept = append(kept, t)
		}
	}
	s.d.Tokens = kept
	return s.saveLocked()
}

// CheckPassword reports whether password is the user's current one.
func (s *Store) CheckPassword(id, password string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.userIndexLocked(id)
	return err == nil && bcrypt.CompareHashAndPassword([]byte(s.d.Users[i].PasswordHash), []byte(password)) == nil
}

func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.userIndexLocked(id)
	if err != nil {
		return err
	}
	if s.d.Users[i].IsAdmin() && s.otherAdminsLocked(id) == 0 {
		return ErrLastAdmin
	}
	s.d.Users = append(s.d.Users[:i], s.d.Users[i+1:]...)
	s.dropTokensLocked(id)
	return s.saveLocked()
}

// tokenIdleLimit is how long an unused session token stays valid.
const tokenIdleLimit = 7 * 24 * time.Hour

func (s *Store) Login(username, password string) (string, *User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var user *User
	for i := range s.d.Users {
		if s.d.Users[i].Username == username {
			user = &s.d.Users[i]
			break
		}
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", nil, ErrBadCreds
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	tok := hex.EncodeToString(raw)
	// Forget sessions nobody has used for a long time.
	live := s.d.Tokens[:0]
	for _, t := range s.d.Tokens {
		if time.Since(t.LastUsed) < tokenIdleLimit {
			live = append(live, t)
		}
	}
	t := Token{Hash: hashToken(tok), UserID: user.ID, CreatedAt: time.Now().UTC(), LastUsed: time.Now().UTC()}
	s.d.Tokens = append(live, t)
	if err := s.saveLocked(); err != nil {
		return "", nil, err
	}
	return tok, user, nil
}

func (s *Store) UserByToken(tok string) (*User, error) {
	if tok == "" {
		return nil, ErrBadCreds
	}
	h := hashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Tokens {
		if s.d.Tokens[i].Hash == h {
			if time.Since(s.d.Tokens[i].LastUsed) > tokenIdleLimit {
				return nil, ErrBadCreds
			}
			for j := range s.d.Users {
				if s.d.Users[j].ID == s.d.Tokens[i].UserID {
					// Kept in memory and written with the next real change: not a file write per request.
					s.d.Tokens[i].LastUsed = time.Now().UTC()
					s.tokensDirty = true
					u := s.d.Users[j]
					return &u, nil
				}
			}
		}
	}
	return nil, ErrBadCreds
}

func (s *Store) DeleteToken(tok string) {
	h := hashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Tokens {
		if s.d.Tokens[i].Hash == h {
			s.d.Tokens = append(s.d.Tokens[:i], s.d.Tokens[i+1:]...)
			_ = s.saveLocked()
			return
		}
	}
}

// IssueToken creates a local session token for an existing user (used by
// the OIDC flow after the external identity provider authenticated them).
func (s *Store) IssueToken(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.userIndexLocked(userID); err != nil {
		return ""
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	tok := hex.EncodeToString(raw)
	live := s.d.Tokens[:0]
	for _, t := range s.d.Tokens {
		if time.Since(t.LastUsed) < tokenIdleLimit {
			live = append(live, t)
		}
	}
	s.d.Tokens = append(live, Token{Hash: hashToken(tok), UserID: userID, CreatedAt: time.Now().UTC(), LastUsed: time.Now().UTC()})
	_ = s.saveLocked()
	return tok
}

func (s *Store) DataDir() string { return filepath.Dir(s.path) }
