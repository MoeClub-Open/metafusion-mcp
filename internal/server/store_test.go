package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MCP_TEST_DATABASE_URL required for isolated PostgreSQL integration")
	}
	s, e := OpenStore(dsn)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}
func testGrant(t *testing.T, s *Store) (Grant, string, string) {
	t.Helper()
	ctx := context.Background()
	client := Client{ID: randomToken(), Name: "test", Redirects: []string{"http://localhost/cb"}}
	if e := s.SaveClient(ctx, client); e != nil {
		t.Fatal(e)
	}
	g := Grant{ID: randomToken(), UserID: randomToken(), ClientID: client.ID, Scopes: []string{"mcp.read"}, Expires: time.Now().Add(time.Hour)}
	g.Cipher, _ = encrypt(make([]byte, 32), g.ID, "")
	code := randomToken()
	verifier := randomToken()
	if e := s.SaveCode(ctx, g, code, client.Redirects[0], challenge(verifier)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec("DELETE FROM mcp.audit WHERE grant_id=$1", g.ID)
		_, _ = s.DB.Exec("DELETE FROM mcp.tokens WHERE grant_id=$1", g.ID)
		_, _ = s.DB.Exec("DELETE FROM mcp.codes WHERE grant_id=$1", g.ID)
		_, _ = s.DB.Exec("DELETE FROM mcp.grants WHERE id=$1", g.ID)
		_, _ = s.DB.Exec("DELETE FROM mcp.clients WHERE id=$1", client.ID)
	})
	return g, code, verifier
}
func TestPostgresAtomicCodeRefreshRevocation(t *testing.T) {
	s := testStore(t)
	g, code, verifier := testGrant(t, s)
	ctx := context.Background()
	if _, e := s.Exchange(ctx, g.ClientID, code, "http://localhost/cb", randomToken(), randomToken(), randomToken()); !errors.Is(e, ErrInvalidGrant) {
		t.Fatal("bad PKCE accepted")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	var access, refresh string
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, r := randomToken(), randomToken()
			_, e := s.Exchange(ctx, g.ClientID, code, "http://localhost/cb", verifier, a, r)
			if e == nil {
				wins.Add(1)
				mu.Lock()
				access, refresh = a, r
				mu.Unlock()
			} else if !errors.Is(e, ErrInvalidGrant) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("code redeemed %d times", wins.Load())
	}
	if _, e := s.Access(ctx, access); e != nil {
		t.Fatal(e)
	}
	wins.Store(0)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, r := randomToken(), randomToken()
			_, e := s.Refresh(ctx, g.ClientID, refresh, a, r)
			if e == nil {
				wins.Add(1)
				mu.Lock()
				access = a
				mu.Unlock()
			} else if !errors.Is(e, ErrInvalidGrant) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("refresh redeemed %d times", wins.Load())
	}
	if e := s.Revoke(ctx, "other-user", g.ID); e == nil {
		t.Fatal("cross-user revoke allowed")
	}
	if e := s.Revoke(ctx, g.UserID, g.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Access(ctx, access); e == nil {
		t.Fatal("revoked grant remains usable")
	}
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(copy)
}
func TestPostgresOAuthToRemoteMCP(t *testing.T) {
	store := testStore(t)
	var writes atomic.Int32
	var canEdit atomic.Bool
	canEdit.Store(true)
	var mintedScopes atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			if ck, e := r.Cookie("mf_session"); e != nil || ck.Value != "session" {
				writeError(w, 401, "invalid_session")
				return
			}
			permissions := []string{}
			if canEdit.Load() {
				permissions = append(permissions, "catalog.entity.edit")
			}
			writeJSON(w, 200, User{ID: "oauth-test-user", Username: "editor", Permissions: permissions, TokenUse: "session"})
		case "/api/auth/tokens":
			if r.Method == "POST" {
				var in struct {
					Scopes []string `json:"scopes"`
				}
				if json.NewDecoder(r.Body).Decode(&in) != nil {
					t.Error("invalid PAT request")
				}
				scope := strings.Join(in.Scopes, " ")
				if scope != "mcp.connect catalog.entity.edit" && scope != "mcp.connect" {
					t.Error("identity or unapproved scopes reached the API credential")
				}
				mintedScopes.Store(scope)
				writeJSON(w, 201, map[string]any{"token": "mfp_test_secret", "item": map[string]string{"id": "test-pat"}})
			}
		case "/api/auth/tokens/introspect":
			writeJSON(w, 200, map[string]any{"valid": true, "user_id": "oauth-test-user", "permissions": []string{"catalog.entity.edit"}})
		case "/api/catalog/entities":
			if r.Header.Get("Authorization") != "Bearer mfp_test_secret" {
				t.Error("MCP token passed upstream")
			}
			if r.Method == "POST" {
				writes.Add(1)
			}
			writeJSON(w, 200, map[string]any{"items": []any{}, "pagination": map[string]any{"next_cursor": "opaque"}})
		default:
			writeError(w, 404, "not_found")
		}
	}))
	defer upstream.Close()
	c := testConfig("https://site.test")
	c.APIURL = upstream.URL
	c.AuthURL = upstream.URL
	s := New(c, store)
	cloud := httptest.NewServer(s.Handler())
	defer cloud.Close()
	send := func(method, path, body string, session bool) *http.Response {
		r, _ := http.NewRequest(method, cloud.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", c.PublicURL)
		if strings.Contains(path, "/token") {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r.Header.Set("Content-Type", "application/json")
		}
		if session {
			r.AddCookie(&http.Cookie{Name: "mf_session", Value: "session"})
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	read := func(res *http.Response) map[string]any {
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		var v map[string]any
		if json.Unmarshal(b, &v) != nil {
			t.Fatalf("invalid response %d %s", res.StatusCode, b)
		}
		return v
	}
	reg := read(send("POST", "/mcp/oauth/register", `{"client_name":"interop test","redirect_uris":["http://localhost/cb"],"token_endpoint_auth_method":"none"}`, false))
	clientID := reg["client_id"].(string)
	t.Cleanup(func() {
		_, _ = store.DB.Exec("DELETE FROM mcp.audit WHERE grant_id IN(SELECT id FROM mcp.grants WHERE client_id=$1)", clientID)
		_, _ = store.DB.Exec("DELETE FROM mcp.tokens WHERE grant_id IN(SELECT id FROM mcp.grants WHERE client_id=$1)", clientID)
		_, _ = store.DB.Exec("DELETE FROM mcp.codes WHERE grant_id IN(SELECT id FROM mcp.grants WHERE client_id=$1)", clientID)
		_, _ = store.DB.Exec("DELETE FROM mcp.grants WHERE client_id=$1", clientID)
		_, _ = store.DB.Exec("DELETE FROM mcp.clients WHERE id=$1", clientID)
	})
	verifier := randomToken()
	q := url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost/cb"}, "response_type": {"code"}, "resource": {c.PublicURL + "/mcp"}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "scope": {"openid email mcp.read catalog.entity.edit"}, "state": {"test-state"}}
	page := send("GET", "/mcp/oauth/authorize?"+q.Encode(), "", false)
	page.Body.Close()
	if page.StatusCode != 200 {
		t.Fatalf("ChatGPT authorization rejected: %d", page.StatusCode)
	}
	info := read(send("GET", "/mcp/manage/api/authorization?"+q.Encode(), "", true))
	if mustJSON(info["scopes"]) != `["mcp.read","catalog.entity.edit"]` {
		t.Fatal("identity scopes offered as permissions")
	}
	denied := read(send("POST", "/mcp/manage/api/approve", mustJSON(map[string]any{"request": q.Encode(), "allow": true, "scopes": []string{"mcp.read", "openid"}, "expires_in_days": 1}), true))
	if denied["error"] != "scope_not_granted" {
		t.Fatal("identity scope granted as a business permission")
	}
	approve := read(send("POST", "/mcp/manage/api/approve", mustJSON(map[string]any{"request": q.Encode(), "allow": true, "scopes": []string{"mcp.read", "catalog.entity.edit"}, "expires_in_days": 1}), true))
	redirect, e := url.Parse(approve["redirect"].(string))
	if e != nil || redirect.Query().Get("state") != "test-state" {
		t.Fatal("state lost")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {redirect.Query().Get("code")}, "redirect_uri": {"http://localhost/cb"}, "code_verifier": {verifier}, "resource": {c.PublicURL + "/mcp"}}
	grant := read(send("POST", "/mcp/oauth/token", form.Encode(), false))
	if grant["scope"] != "mcp.read catalog.entity.edit" || grant["id_token"] != nil {
		t.Fatal("ungranted identity scopes or ID token reported")
	}
	token := grant["access_token"].(string)
	if !strings.HasPrefix(token, "mfm_") {
		t.Fatal("not a dedicated MCP credential")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "real-mcp-client", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: cloud.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport, token}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	listed, e := session.ListTools(ctx, nil)
	if e != nil || len(listed.Tools) != 13 {
		t.Fatal("remote MCP tool discovery failed", e)
	}
	res, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_search", Arguments: map[string]any{"query": map[string]string{"q": "sample"}}})
	if e != nil || res.IsError {
		t.Fatal("remote search failed", e, res)
	}
	res, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_api_call", Arguments: map[string]any{"method": "POST", "path": "/api/catalog/entities", "body": map[string]string{"title": "sample"}}})
	if e != nil || res.IsError || writes.Load() != 1 {
		t.Fatal("remote write failed/replayed", e)
	}
	grants, e := store.Grants(ctx, "oauth-test-user")
	if e != nil || len(grants) != 1 {
		t.Fatal(e)
	}
	if e = store.Revoke(ctx, "oauth-test-user", grants[0].ID); e != nil {
		t.Fatal(e)
	}
	_, e = session.ListTools(ctx, nil)
	if e == nil {
		t.Fatal("revoked MCP grant was accepted")
	}

	// Scope omitted by the client must offer only the minimal consent set. A
	// user without edit permission can still approve read-only MCP access.
	canEdit.Store(false)
	q.Del("scope")
	info = read(send("GET", "/mcp/manage/api/authorization?"+q.Encode(), "", true))
	if mustJSON(info["scopes"]) != `["mcp.read","catalog.entity.edit"]` {
		t.Fatal("omitted scope offered expanded permissions")
	}
	denied = read(send("POST", "/mcp/manage/api/approve", mustJSON(map[string]any{"request": q.Encode(), "allow": true, "scopes": []string{"catalog.entity.edit"}, "expires_in_days": 1}), true))
	if denied["error"] != "scope_not_granted" {
		t.Fatal("a read-only user was granted edit permission")
	}
	approve = read(send("POST", "/mcp/manage/api/approve", mustJSON(map[string]any{"request": q.Encode(), "allow": true, "scopes": []string{"mcp.read"}, "expires_in_days": 1}), true))
	redirect, e = url.Parse(approve["redirect"].(string))
	if e != nil || mintedScopes.Load() != "mcp.connect" {
		t.Fatal("read-only grant did not mint an identity-only PAT", e)
	}
	form.Set("code", redirect.Query().Get("code"))
	grant = read(send("POST", "/mcp/oauth/token", form.Encode(), false))
	if grant["scope"] != "mcp.read" {
		t.Fatal("read-only scope response expanded")
	}
	readSession, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: cloud.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{http.DefaultTransport, grant["access_token"].(string)}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer readSession.Close()
	res, e = readSession.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_search", Arguments: map[string]any{"query": map[string]string{"q": "read-only"}}})
	if e != nil || res.IsError {
		t.Fatal("read-only search failed", e)
	}
	res, e = readSession.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_api_call", Arguments: map[string]any{"method": "POST", "path": "/api/catalog/entities", "body": map[string]string{"title": "must not write"}}})
	if e != nil || !failedResult(res) || writes.Load() != 1 {
		t.Fatal("read-only grant authorized an upstream write", e)
	}
}
