package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var pkcePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"resource": s.Config.PublicURL + "/mcp", "authorization_servers": []string{s.Config.PublicURL + "/mcp/oauth"}, "bearer_methods_supported": []string{"header"}, "scopes_supported": supportedScopes, "resource_documentation": s.Config.PublicURL + "/docs/api-mcp"})
}
func (s *Server) authorizationMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.Config.PublicURL + "/mcp/oauth"
	writeJSON(w, 200, map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "registration_endpoint": base + "/register", "revocation_endpoint": base + "/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": supportedScopes})
}
func validRedirect(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))
}
func (s *Server) registerClient(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string   `json:"client_name"`
		Redirects     []string `json:"redirect_uris"`
		AuthMethod    string   `json:"token_endpoint_auth_method"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
		Scope         string   `json:"scope"`
		ClientURI     string   `json:"client_uri"`
		LogoURI       string   `json:"logo_uri"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Name) == 0 || len(in.Name) > 48 || len(in.Redirects) == 0 || len(in.Redirects) > 8 || (in.AuthMethod != "" && in.AuthMethod != "none") {
		writeError(w, 400, "invalid_client_metadata")
		return
	}
	for _, redirect := range in.Redirects {
		if !validRedirect(redirect) {
			writeError(w, 400, "invalid_redirect_uri")
			return
		}
	}
	for _, gt := range in.GrantTypes {
		if gt != "authorization_code" && gt != "refresh_token" {
			writeError(w, 400, "invalid_client_metadata")
			return
		}
	}
	for _, rt := range in.ResponseTypes {
		if rt != "code" {
			writeError(w, 400, "invalid_client_metadata")
			return
		}
	}
	c := Client{ID: "mfc_" + randomToken(), Name: in.Name, Redirects: in.Redirects}
	if e := s.Store.SaveClient(r.Context(), c); e != nil {
		writeError(w, 503, "registration_unavailable")
		return
	}
	writeJSON(w, 201, map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.Redirects, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}

type authorization struct {
	Client                     Client
	Redirect, Challenge, State string
	Scopes                     []string
}

func requestedBusinessScopes(raw string) ([]string, error) {
	requested := strings.Fields(raw)
	if len(requested) == 0 {
		// Omitted scope offers a small, explicit consent set. The user may approve
		// mcp.read alone; other supported business scopes require a new request.
		return append([]string(nil), defaultScopes...), nil
	}
	scopes := []string{}
	for _, scope := range requested {
		// Some OAuth clients append OIDC identity scopes. This resource server does
		// not offer OIDC: leave them ungranted, never turn them into PAT permissions
		// or imply an ID token. The token response reports only the granted scopes.
		// catalog.import.submit was removed with the write-import API: drop silently so
		// old clients do not break consent.
		if scope == "openid" || scope == "email" || scope == "profile" || scope == "catalog.import.submit" {
			continue
		}
		if !slices.Contains(supportedScopes, scope) {
			return nil, errors.New("invalid_scope")
		}
		if !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return nil, errors.New("invalid_scope")
	}
	return scopes, nil
}

func (s *Server) validateAuthorization(r *http.Request, q url.Values) (authorization, error) {
	a := authorization{}
	for _, name := range []string{"client_id", "redirect_uri", "response_type", "resource", "code_challenge", "code_challenge_method", "scope", "state"} {
		if len(q[name]) > 1 {
			return a, errors.New("duplicate_parameter")
		}
	}
	if q.Get("resource") != s.Config.PublicURL+"/mcp" {
		return a, errors.New("invalid_target")
	}
	if q.Get("response_type") != "code" {
		return a, errors.New("unsupported_response_type")
	}
	if q.Get("code_challenge_method") != "S256" || !pkcePattern.MatchString(q.Get("code_challenge")) {
		return a, errors.New("invalid_code_challenge")
	}
	c, e := s.Store.Client(r.Context(), q.Get("client_id"))
	if e != nil {
		return a, errors.New("invalid_client")
	}
	if !slices.Contains(c.Redirects, q.Get("redirect_uri")) {
		return a, errors.New("invalid_redirect_uri")
	}
	scopes, e := requestedBusinessScopes(q.Get("scope"))
	if e != nil {
		return a, e
	}
	if len(q.Get("state")) > 2048 {
		return a, errors.New("invalid_request")
	}
	return authorization{c, q.Get("redirect_uri"), q.Get("code_challenge"), q.Get("state"), scopes}, nil
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if _, e := s.validateAuthorization(r, r.URL.Query()); e != nil {
		writeError(w, 400, e.Error())
		return
	}
	s.page(w, r)
}
func (s *Server) authorizationInfo(w http.ResponseWriter, r *http.Request) {
	u, _, e := s.session(r)
	if e != nil {
		writeError(w, 401, "session_required")
		return
	}
	a, e := s.validateAuthorization(r, r.URL.Query())
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	existing := []string{}
	if grants, err := s.Store.Grants(r.Context(), u.ID); err == nil {
		for _, g := range grants {
			if g.ClientID == a.Client.ID && g.Revoked == nil && g.Expires.After(time.Now()) {
				existing = filterSupportedScopes(g.Scopes)
				break
			}
		}
	}
	writeJSON(w, 200, map[string]any{"client": a.Client, "scopes": a.Scopes, "redirect_uri": a.Redirect, "existing_scopes": existing})
}
func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	u, cookie, e := s.session(r)
	if e != nil {
		writeError(w, 401, "session_required")
		return
	}
	var in struct {
		Request string   `json:"request"`
		Scopes  []string `json:"scopes"`
		Days    int      `json:"expires_in_days"`
		Allow   bool     `json:"allow"`
	}
	if decode(r, &in) != nil || len(in.Request) > 8192 || in.Days < 1 || in.Days > 30 {
		writeError(w, 400, "invalid_request")
		return
	}
	q, e := url.ParseQuery(in.Request)
	if e != nil {
		writeError(w, 400, "invalid_request")
		return
	}
	a, e := s.validateAuthorization(r, q)
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	target, _ := url.Parse(a.Redirect)
	reply := target.Query()
	reply.Set("state", a.State)
	if !in.Allow {
		reply.Set("error", "access_denied")
		target.RawQuery = reply.Encode()
		writeJSON(w, 200, map[string]any{"redirect": target.String()})
		return
	}
	if len(in.Scopes) == 0 {
		writeError(w, 400, "invalid_scope")
		return
	}
	for _, scope := range in.Scopes {
		if !slices.Contains(a.Scopes, scope) || (scope != "mcp.read" && !slices.Contains(u.Permissions, "*") && !slices.Contains(u.Permissions, scope)) {
			writeError(w, 403, "scope_not_granted")
			return
		}
	}
	g := Grant{ID: randomToken(), UserID: u.ID, ClientID: a.Client.ID, Scopes: in.Scopes, Expires: time.Now().Add(time.Duration(in.Days) * 24 * time.Hour)}
	id, pat, e := s.mintPAT(r.Context(), cookie, a.Client.Name, in.Scopes, in.Days)
	if e != nil {
		writeError(w, 503, "pat_creation_failed")
		return
	}
	g.PATID = id
	g.Cipher, e = encrypt(s.Config.Key, g.ID, pat)
	if e != nil {
		_ = s.deletePAT(r.Context(), cookie, id)
		writeError(w, 500, "encryption_failed")
		return
	}
	code := randomToken()
	if e = s.Store.SaveCode(r.Context(), g, code, a.Redirect, a.Challenge); e != nil {
		_ = s.deletePAT(r.Context(), cookie, id)
		writeError(w, 503, "authorization_unavailable")
		return
	}
	reply.Set("code", code)
	target.RawQuery = reply.Encode()
	writeJSON(w, 200, map[string]any{"redirect": target.String()})
}
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		writeError(w, 401, "invalid_client")
		return
	}
	if e := r.ParseForm(); e != nil {
		writeError(w, 400, "invalid_request")
		return
	}
	f := r.PostForm
	if f.Get("resource") != s.Config.PublicURL+"/mcp" {
		writeError(w, 400, "invalid_target")
		return
	}
	for k, v := range f {
		if len(v) != 1 {
			writeError(w, 400, "invalid_request")
			return
		}
		_ = k
	}
	access := "mfm_" + randomToken()
	refresh := "mfr_" + randomToken()
	var g Grant
	var e error
	switch f.Get("grant_type") {
	case "authorization_code":
		if !verifierPattern.MatchString(f.Get("code_verifier")) {
			writeError(w, 400, "invalid_grant")
			return
		}
		g, e = s.Store.Exchange(r.Context(), f.Get("client_id"), f.Get("code"), f.Get("redirect_uri"), f.Get("code_verifier"), access, refresh)
	case "refresh_token":
		g, e = s.Store.Refresh(r.Context(), f.Get("client_id"), f.Get("refresh_token"), access, refresh)
	default:
		writeError(w, 400, "unsupported_grant_type")
		return
	}
	if e != nil {
		if errors.Is(e, ErrInvalidGrant) {
			writeError(w, 400, "invalid_grant")
		} else {
			writeError(w, 503, "token_unavailable")
		}
		return
	}
	expires := 900
	if left := int(time.Until(g.Expires).Seconds()); left < expires {
		expires = left
	}
	writeJSON(w, 200, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": expires, "scope": strings.Join(g.Scopes, " ")})
}
func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	if r.ParseForm() != nil {
		writeError(w, 400, "invalid_request")
		return
	}
	if e := s.Store.RevokeToken(r.Context(), r.PostForm.Get("client_id"), r.PostForm.Get("token")); e != nil {
		writeError(w, 503, "revoke_unavailable")
		return
	}
	w.WriteHeader(200)
}
