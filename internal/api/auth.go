package api

import (
	"net/http"
	"regexp"
	"strings"

	"freecenter/internal/store"
)

// defaultPassword is the bootstrap administrator password: logging in with it
// makes the UI insist on a change.
const defaultPassword = "admin"

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeBody(w, r, &req) {
		return
	}
	tok, user, err := s.store.Login(req.Username, req.Password)
	if err != nil {
		errJSON(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	writeJSON(w, 200, map[string]any{
		"token":                tok,
		"username":             user.Username,
		"id":                   user.ID,
		"role":                 user.RoleName(),
		"must_change_password": req.Password == defaultPassword,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	tok := bearer(r)
	s.store.DeleteToken(tok)
	writeJSON(w, 200, map[string]string{"status": "logged out"})
}

func currentUser(r *http.Request) *store.User {
	user, _ := r.Context().Value(userKey).(*store.User)
	return user
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	writeJSON(w, 200, map[string]any{"id": user.ID, "username": user.Username, "role": user.RoleName()})
}

// admin wraps handlers only administrators may call (use inside auth).
func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			errJSON(w, http.StatusForbidden, "administrator role required")
			return
		}
		h(w, r)
	}
}

// changesSomething reports whether a request is off limits for a read-only
// user: anything that modifies state, plus the VM console (keyboard access).
// Queries that happen to use POST, and managing one's own session, are allowed.
func changesSomething(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasSuffix(p, "/console") {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return !(strings.HasPrefix(p, "/api/v1/auth/") || strings.HasSuffix(p, "/esxcli") || strings.HasSuffix(p, "/migrate/check"))
}

func validPassword(pw string) string {
	switch {
	case len(pw) < store.MinPasswordLength:
		return "the password must be at least 8 characters long"
	case pw == defaultPassword:
		return "choose a password other than the default one"
	}
	return ""
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !s.store.CheckPassword(user.ID, req.Current) {
		errJSON(w, http.StatusForbidden, "the current password is not correct")
		return
	}
	if msg := validPassword(req.New); msg != "" {
		errJSON(w, http.StatusBadRequest, msg)
		return
	}
	// Other sessions of the user end; the one making the change stays.
	if err := s.store.SetUserPassword(user.ID, req.New, bearer(r)); err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// ---- users ----

type userView struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func viewUser(u store.User) userView {
	return userView{ID: u.ID, Username: u.Username, Role: u.RoleName(), CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00")}
}

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)

func validRole(role string) bool { return role == store.RoleAdmin || role == store.RoleReadOnly }

func (s *Server) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	users := s.store.Users()
	out := make([]userView, len(users))
	for i, u := range users {
		out[i] = viewUser(u)
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	switch {
	case !usernameRE.MatchString(req.Username):
		errJSON(w, http.StatusBadRequest, "user name: letters, digits and . _ @ - (max 64)")
		return
	case !validRole(req.Role):
		errJSON(w, http.StatusBadRequest, "role must be admin or readonly")
		return
	}
	if msg := validPassword(req.Password); msg != "" {
		errJSON(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.store.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 201, viewUser(*u))
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if req.Role != "" {
		if !validRole(req.Role) {
			errJSON(w, http.StatusBadRequest, "role must be admin or readonly")
			return
		}
		if err := s.store.SetUserRole(id, req.Role); err != nil {
			userError(w, err)
			return
		}
	}
	if req.Password != "" {
		if msg := validPassword(req.Password); msg != "" {
			errJSON(w, http.StatusBadRequest, msg)
			return
		}
		// An administrator resetting a password signs that user out everywhere.
		if err := s.store.SetUserPassword(id, req.Password, ""); err != nil {
			userError(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == currentUser(r).ID {
		errJSON(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if err := s.store.DeleteUser(id); err != nil {
		userError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func userError(w http.ResponseWriter, err error) {
	if err == store.ErrLastAdmin {
		errJSON(w, http.StatusConflict, err.Error())
		return
	}
	apiError(w, err)
}

func bearer(r *http.Request) string {
	tok := r.Header.Get("Authorization")
	return trimBearer(tok)
}

func trimBearer(s string) string {
	if len(s) > 7 && s[:7] == "Bearer " {
		return s[7:]
	}
	return s
}
