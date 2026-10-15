package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPostgresNarrowGrantReusesCredential(t *testing.T) {
	store := testStore(t)
	g, _, _ := testGrant(t, store)
	g.Scopes = []string{"mcp.read", "catalog.entity.edit"}
	g.PATID = "delegation-id"
	key := make([]byte, 32)
	g.Cipher, _ = encrypt(key, g.ID, "mfp_delegation_secret")
	if _, err := store.DB.Exec("UPDATE mcp.grants SET scopes=$1,pat_id=$2,pat_cipher=$3 WHERE id=$4", mustJSON(g.Scopes), g.PATID, g.Cipher, g.ID); err != nil {
		t.Fatal(err)
	}
	var patches, creations atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/auth/me":
			writeJSON(w, 200, User{ID: g.UserID, Username: "owner", Permissions: []string{"catalog.entity.edit"}, TokenUse: "session"})
		case r.Method == "PATCH" && r.URL.Path == "/api/auth/mcp-tokens/"+g.PATID:
			patches.Add(1)
			var in struct {
				Scopes []string `json:"scopes"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || !reflect.DeepEqual(in.Scopes, []string{"mcp.connect"}) {
				t.Error("local scopes leaked into the platform credential")
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("browser session must authorize credential management")
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
		case r.Method == "POST":
			creations.Add(1)
			writeError(w, 500, "unexpected_credential_creation")
		default:
			writeError(w, 404, "unexpected_endpoint")
		}
	}))
	defer upstream.Close()
	s := New(testConfig(upstream.URL), store)
	request := httptest.NewRequest("PATCH", upstream.URL+"/mcp/manage/api/grants/"+g.ID, strings.NewReader(`{"scopes":["mcp.read"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", upstream.URL)
	request.AddCookie(&http.Cookie{Name: "mf_session", Value: "session"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	if w.Code != 200 || patches.Load() != 1 || creations.Load() != 0 {
		t.Fatalf("narrow status=%d patches=%d creations=%d body=%s", w.Code, patches.Load(), creations.Load(), w.Body.String())
	}
	items, err := store.Grants(context.Background(), g.UserID)
	if err != nil || len(items) != 1 || items[0].PATID != g.PATID || !reflect.DeepEqual(items[0].Cipher, g.Cipher) || !reflect.DeepEqual(items[0].Scopes, []string{"mcp.read"}) {
		t.Fatalf("narrowing replaced credential or lost grant: %v", err)
	}
	request = httptest.NewRequest("PATCH", upstream.URL+"/mcp/manage/api/grants/"+g.ID, strings.NewReader(`{"scopes":["mcp.read","catalog.entity.edit"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", upstream.URL)
	request.AddCookie(&http.Cookie{Name: "mf_session", Value: "session"})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	if w.Code != 400 || patches.Load() != 1 {
		t.Fatalf("scope expansion reached upstream: %d %s", w.Code, w.Body.String())
	}
}

func TestReferencePathsAllowOnlySkillDocuments(t *testing.T) {
	for _, p := range []string{"skills/metafusion-curator/SKILL.md", "skills/metafusion-curator/references/articles.md", "docs/api-auth.md"} {
		if !referencePath.MatchString(p) {
			t.Errorf("published documentation path rejected: %s", p)
		}
	}
	for _, p := range []string{"skills/metafusion-curator/scripts/metafusion-api.mjs", "skills/metafusion-curator/references/../credentials.json", "skills/metafusion-curator/references/%2e%2e/private.md", "docs/../../private.md", "skills/metafusion-curator/reference-cloud.md"} {
		if referencePath.MatchString(p) {
			t.Errorf("noncanonical or unsafe document path accepted: %s", p)
		}
	}
}

func TestPostgresOAuthRevocationCleansDelegationAndCanRetry(t *testing.T) {
	for _, deferCleanup := range []bool{false, true} {
		t.Run(map[bool]string{false: "immediate", true: "retry"}[deferCleanup], func(t *testing.T) {
			store := testStore(t)
			g, code, verifier := testGrant(t, store)
			g.PATID = "delegation-id"
			g.Cipher, _ = encrypt(make([]byte, 32), g.ID, "mfp_test_delegation_secret")
			if _, err := store.DB.Exec("UPDATE mcp.grants SET pat_id=$1,pat_cipher=$2 WHERE id=$3", g.PATID, g.Cipher, g.ID); err != nil {
				t.Fatal(err)
			}
			access, refresh := "mfm_"+randomToken(), "mfr_"+randomToken()
			if _, err := store.Exchange(context.Background(), g.ClientID, code, "http://localhost/cb", verifier, access, refresh); err != nil {
				t.Fatal(err)
			}
			var available atomic.Bool
			available.Store(!deferCleanup)
			var cleanupCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/auth/me":
					writeJSON(w, 200, User{ID: g.UserID, Username: "owner", TokenUse: "session"})
				case r.Method == "POST" && r.URL.Path == "/api/auth/mcp-tokens/revoke":
					cleanupCalls.Add(1)
					var body struct {
						Token string `json:"token"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.Token != "mfp_test_delegation_secret" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
						t.Error("revocation proof was not isolated")
					}
					if available.Load() {
						writeJSON(w, 200, map[string]bool{"ok": true})
					} else {
						writeError(w, 503, "unavailable")
					}
				case r.Method == "DELETE" && r.URL.Path == "/api/auth/mcp-tokens/"+g.PATID:
					if available.Load() {
						writeJSON(w, 200, map[string]bool{"ok": true})
					} else {
						writeError(w, 503, "unavailable")
					}
				default:
					writeError(w, 404, "unexpected_endpoint")
				}
			}))
			defer upstream.Close()
			s := New(testConfig(upstream.URL), store)
			revoke := func(client string) *httptest.ResponseRecorder {
				form := url.Values{"client_id": {client}, "token": {access}}
				r := httptest.NewRequest("POST", upstream.URL+"/mcp/oauth/revoke", strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			if w := revoke("other-client"); w.Code != 200 {
				t.Fatal(w.Code)
			}
			if _, err := store.Access(context.Background(), access); err != nil || cleanupCalls.Load() != 0 {
				t.Fatal("wrong client revoked a connection")
			}
			if w := revoke(g.ClientID); w.Code != 200 {
				t.Fatalf("OAuth revocation status %d", w.Code)
			}
			if _, err := store.Access(context.Background(), access); err == nil {
				t.Fatal("revoked access still accepted")
			}
			if _, err := store.Refresh(context.Background(), g.ClientID, refresh, randomToken(), randomToken()); err == nil {
				t.Fatal("revoked connection can refresh")
			}
			items, err := store.Grants(context.Background(), g.UserID)
			if err != nil || len(items) != 1 || items[0].Revoked == nil {
				t.Fatal("revocation did not close the grant")
			}
			if deferCleanup {
				if items[0].PATID == "" {
					t.Fatal("failed cleanup discarded the retry credential")
				}
				available.Store(true)
				r := httptest.NewRequest("DELETE", upstream.URL+"/mcp/manage/api/grants/"+g.ID, nil)
				r.AddCookie(&http.Cookie{Name: "mf_session", Value: "session"})
				r.Header.Set("Origin", upstream.URL)
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("cleanup retry failed: %d", w.Code)
				}
			}
			items, err = store.Grants(context.Background(), g.UserID)
			if err != nil || items[0].PATID != "" || len(items[0].Cipher) != 0 {
				t.Fatalf("revoked secret was retained after cleanup: %v", err)
			}
		})
	}
}
