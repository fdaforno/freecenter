package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"freecenter/internal/model"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestHostPasswordEncryptedAtRest(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.AddHost(model.HostRecord{Name: "h1", Address: "10.0.0.1", Username: "root", Password: "S3cret!pw"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "S3cret!pw") {
		t.Fatal("password is stored in plain text")
	}
	if !strings.Contains(string(raw), sealedPrefix) {
		t.Fatal("password is not stored encrypted")
	}
	// A fresh process reads it back with the key file.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Hosts()[0].Password; got != "S3cret!pw" {
		t.Errorf("password after reload = %q", got)
	}
	// Without the right key the store must refuse to open rather than use garbage.
	t.Setenv(secretEnv, "another key")
	if _, err := Open(path); err == nil {
		t.Error("store opened with the wrong key")
	}
}

func TestPlainTextStoreIsMigrated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	legacy := `{"hosts":[{"id":"a","name":"old","address":"10.0.0.2","username":"root","password":"legacy-pw"}],"users":[],"tokens":[]}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hosts()[0].Password != "legacy-pw" {
		t.Error("legacy password not readable")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "legacy-pw") {
		t.Error("legacy plain-text password was not rewritten encrypted")
	}
}

func TestUsersAndRoles(t *testing.T) {
	s, _ := openTemp(t)
	admin, err := s.CreateUser("admin", "admin-password", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("deleting the last admin: %v", err)
	}
	if err := s.SetUserRole(admin.ID, RoleReadOnly); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demoting the last admin: %v", err)
	}
	viewer, err := s.CreateUser("viewer", "viewer-password", RoleReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	if viewer.IsAdmin() {
		t.Error("read-only user is admin")
	}
	if (&User{}).RoleName() != RoleAdmin {
		t.Error("a user without role must be an administrator (pre-role data)")
	}

	tok1, _, err := s.Login("viewer", "viewer-password")
	if err != nil {
		t.Fatal(err)
	}
	tok2, _, _ := s.Login("viewer", "viewer-password")
	// Changing the password keeps the current session and ends the others.
	if err := s.SetUserPassword(viewer.ID, "another-password", tok1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByToken(tok1); err != nil {
		t.Error("current session was dropped")
	}
	if _, err := s.UserByToken(tok2); err == nil {
		t.Error("other session survived a password change")
	}
	if _, _, err := s.Login("viewer", "viewer-password"); err == nil {
		t.Error("old password still works")
	}
	if !s.CheckPassword(viewer.ID, "another-password") {
		t.Error("new password not accepted")
	}

	if err := s.DeleteUser(viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByToken(tok1); err == nil {
		t.Error("deleted user still has a session")
	}
}
