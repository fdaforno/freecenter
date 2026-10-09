package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"freecenter/internal/store"
)

// oidcDiscovery is the well-known OpenID Connect configuration.
type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// oidcState is a short-lived state parameter for the authorization flow.
type oidcState struct {
	Nonce     string
	Redirect  string // where to send the browser after login
	CreatedAt time.Time
}

const oidcStateTTL = 5 * time.Minute

var (
	oidcStates   = map[string]*oidcState{}
	oidcStatesMu sync.Mutex
)

// handleOIDCLogin redirects to the OIDC provider's authorization endpoint.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.OIDC()
	if cfg == nil || !cfg.Enabled {
		http.Redirect(w, r, "/#/login?error=oidc_disabled", http.StatusSeeOther)
		return
	}
	disc, err := fetchDiscovery(cfg.Issuer)
	if err != nil {
		s.logger.Error("oidc discovery", "err", err)
		http.Redirect(w, r, "/#/login?error=oidc_discovery", http.StatusSeeOther)
		return
	}
	// Derive the redirect URL from the request so it works behind any proxy.
	redirectURL := externalURL(r) + "/auth/oidc/callback"
	state := randomHex(16)
	nonce := randomHex(16)
	oidcStatesMu.Lock()
	// GC old states.
	for k, v := range oidcStates {
		if time.Since(v.CreatedAt) > oidcStateTTL {
			delete(oidcStates, k)
		}
	}
	oidcStates[state] = &oidcState{Nonce: nonce, Redirect: "/", CreatedAt: time.Now()}
	oidcStatesMu.Unlock()

	params := url.Values{
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {redirectURL},
		"response_type": {"code"},
		"scope":         {strings.Join(cfg.EffectiveScopes(), " ")},
		"state":         {state},
		"nonce":         {nonce},
	}
	http.Redirect(w, r, disc.AuthorizationEndpoint+"?"+params.Encode(), http.StatusSeeOther)
}

// handleOIDCCallback exchanges the authorization code for tokens, fetches
// userinfo, maps groups → role, and issues a local session.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/#/login?error=oidc_missing", http.StatusSeeOther)
		return
	}
	oidcStatesMu.Lock()
	st, ok := oidcStates[state]
	if ok {
		delete(oidcStates, state)
	}
	oidcStatesMu.Unlock()
	if !ok || time.Since(st.CreatedAt) > oidcStateTTL {
		http.Redirect(w, r, "/#/login?error=oidc_state", http.StatusSeeOther)
		return
	}

	cfg := s.store.OIDC()
	if cfg == nil || !cfg.Enabled {
		http.Redirect(w, r, "/#/login?error=oidc_disabled", http.StatusSeeOther)
		return
	}
	disc, err := fetchDiscovery(cfg.Issuer)
	if err != nil {
		http.Redirect(w, r, "/#/login?error=oidc_discovery", http.StatusSeeOther)
		return
	}

	// Exchange the code for tokens.
	redirectURL := externalURL(r) + "/auth/oidc/callback"
	tokResp, err := exchangeCode(disc.TokenEndpoint, cfg.ClientID, cfg.ClientSecret, code, redirectURL)
	if err != nil {
		s.logger.Error("oidc token exchange", "err", err)
		http.Redirect(w, r, "/#/login?error=oidc_token", http.StatusSeeOther)
		return
	}

	// Fetch userinfo to get the user's identity and groups.
	info, err := fetchUserinfo(disc.UserinfoEndpoint, tokResp.AccessToken)
	if err != nil {
		s.logger.Error("oidc userinfo", "err", err)
		http.Redirect(w, r, "/#/login?error=oidc_userinfo", http.StatusSeeOther)
		return
	}

	// Determine the role from group membership.
	role := resolveRole(cfg, info)
	username := info.PreferredUsername
	if username == "" {
		username = info.Email
	}
	if username == "" {
		username = info.Sub
	}

	// Create or update the local user.
	user, err := s.store.CreateOIDCUser(username, info.Sub, role)
	if err != nil {
		s.logger.Error("oidc create user", "err", err)
		http.Redirect(w, r, "/#/login?error=oidc_user", http.StatusSeeOther)
		return
	}

	// Issue a local session token (same mechanism as regular login).
	tok := issueSessionToken(s.store, user.ID)
	if tok == "" {
		http.Redirect(w, r, "/#/login?error=oidc_session", http.StatusSeeOther)
		return
	}

	// Redirect to the frontend with the session token.
	target := fmt.Sprintf("/#/auth/oidc/callback?token=%s&username=%s&role=%s",
		url.QueryEscape(tok),
		url.QueryEscape(user.Username),
		url.QueryEscape(user.RoleName()),
	)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// ---- API handlers for OIDC config and roles ----

func (s *Server) handleGetOIDCConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.OIDC()
	if cfg == nil {
		writeJSON(w, 200, &store.OIDCConfig{Enabled: false})
		return
	}
	// Never expose the client secret.
	out := *cfg
	out.ClientSecret = ""
	writeJSON(w, 200, out)
}

func (s *Server) handlePutOIDCConfig(w http.ResponseWriter, r *http.Request) {
	var req store.OIDCConfig
	if !decodeBody(w, r, &req) {
		return
	}
	// Preserve the existing secret when the new one is empty.
	if req.ClientSecret == "" {
		if existing := s.store.OIDC(); existing != nil {
			req.ClientSecret = existing.ClientSecret
		}
	}
	if err := s.store.SetOIDC(&req); err != nil {
		apiError(w, err)
		return
	}
	req.ClientSecret = ""
	writeJSON(w, 200, req)
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.store.Roles())
}

func (s *Server) handlePutRole(w http.ResponseWriter, r *http.Request) {
	var role store.Role
	if !decodeBody(w, r, &role) {
		return
	}
	if r.Method == http.MethodPut {
		role.ID = r.PathValue("id")
	}
	if role.Name == "" {
		errJSON(w, http.StatusBadRequest, "role name is required")
		return
	}
	out, err := s.store.PutRole(role)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteRole(id); err != nil {
		if err == store.ErrRoleBuiltin {
			errJSON(w, http.StatusConflict, err.Error())
			return
		}
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

// ---- helpers ----

// fetchDiscovery retrieves the OIDC well-known configuration.
func fetchDiscovery(issuer string) (*oidcDiscovery, error) {
	url := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discovery: %s", resp.Status)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
}

func exchangeCode(tokenURL, clientID, clientSecret, code, redirectURI string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange: %s: %s", resp.Status, string(body))
	}
	var t tokenResponse
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

type userinfoResponse struct {
	Sub               string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	Groups            []string `json:"groups"`
}

func fetchUserinfo(userinfoURL, accessToken string) (*userinfoResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", userinfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("userinfo: %s", resp.Status)
	}
	var info userinfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// resolveRole maps OIDC groups to a FreeCenter role ID, falling back to
// the configured default role.
func resolveRole(cfg *store.OIDCConfig, info *userinfoResponse) string {
	for _, g := range info.Groups {
		if roleID, ok := cfg.GroupRoles[g]; ok {
			return roleID
		}
	}
	return cfg.DefaultRole
}

// issueSessionToken creates a local session token for a user.
func issueSessionToken(st *store.Store, userID string) string {
	return st.IssueToken(userID)
}

// externalURL returns the scheme://host portion of the request, honoring
// X-Forwarded-* headers for reverse proxies.
func externalURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
