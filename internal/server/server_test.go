package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(base string) Config {
	return Config{PublicURL: base, APIURL: base, AuthURL: base, Key: make([]byte, 32), AllowedOrigins: []string{base}}
}
func TestRequestedBusinessScopes(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      []string
		invalid   bool
	}{
		{"chatgpt identity additions", "openid email mcp.read catalog.entity.edit", []string{"mcp.read", "catalog.entity.edit"}, false},
		{"profile and duplicates", "profile mcp.read mcp.read", []string{"mcp.read"}, false},
		{"unknown permission", "openid mcp.read catalog.superuser", nil, true},
		{"identity only never expands", "openid email profile", nil, true},
		{"scope is case sensitive", "OPENID mcp.read", nil, true},
		{"omitted scopes offer minimal consent", "", []string{"mcp.read", "catalog.entity.edit"}, false},
		{"wildcard cannot be delegated", "mcp.read *", nil, true},
		{"management cannot be delegated", "mcp.read catalog.definitions.manage", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := requestedBusinessScopes(tc.raw)
			if (err != nil) != tc.invalid || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v, invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}
func principalContext(p Principal) context.Context {
	if p.PAT != "" && p.Grant.Scopes == nil {
		p.Grant.Scopes = []string{"catalog.entity.edit"}
	}
	var ctx context.Context
	auth.RequireBearerToken(func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: p.Grant.UserID, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"principal": p}}, nil
	}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), withBearer(httptest.NewRequest("POST", "http://localhost/mcp", nil), "mfm_test"))
	return ctx
}
func withBearer(r *http.Request, token string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}
func TestSecretEncryption(t *testing.T) {
	key := make([]byte, 32)
	b, e := encrypt(key, "grant-a", "secret")
	if e != nil {
		t.Fatal(e)
	}
	if got, e := decrypt(key, "grant-a", b); e != nil || got != "secret" {
		t.Fatal("round trip failed")
	}
	if _, e := decrypt(key, "grant-b", b); e == nil {
		t.Fatal("AAD binding failed")
	}
	b[len(b)-1] ^= 1
	if _, e := decrypt(key, "grant-a", b); e == nil {
		t.Fatal("tampering accepted")
	}
}
func TestAPIPathIsolation(t *testing.T) {
	for _, p := range []string{"https://evil.test/api/catalog/entities", "//evil.test/api/x", "/api/../auth/me", "/api/%2e%2e/auth/me", "/api/%252e%252e/auth/me", "/api/catalog%2finternal/x", "/api/internal/x", "/api/catalog/internal/x", "/api/auth/tokens", "/api/oauth/authorize", "/api/setup", "/api/catalog/entities?token=x", "/api/catalog/entities#x", "/api/catalog\\..\\auth"} {
		if _, e := safeAPIPath(p); e == nil {
			t.Errorf("unsafe path accepted: %s", p)
		}
	}
	for _, p := range []string{"/api/catalog/entities", "/api/admin/catalog-definitions", "/api/storage/assets", "/api/community/posts/123", "/api/catalog/entities/abc/relations"} {
		if _, e := safeAPIPath(p); e != nil {
			t.Errorf("public API rejected: %s: %v", p, e)
		}
	}
}
func TestReadOnlyAndCredentialIsolation(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer mfp_private" || r.Header.Get("Cookie") != "" {
			t.Error("wrong credential forwarded")
		}
		writeJSON(w, 409, map[string]any{"error": "version_conflict", "current_version": 2})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	readonly := principalContext(Principal{Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	if !failedResult(s.callAPI(readonly, APIInput{Method: "POST", Path: "/api/catalog/entities", Body: map[string]any{"title": "x"}})) || hits.Load() != 0 {
		t.Fatal("read-only grant performed mutation")
	}
	ctx := principalContext(Principal{Grant: Grant{UserID: "u", Scopes: []string{"catalog.entity.edit"}}, PAT: "mfp_private"})
	res := s.callAPI(ctx, APIInput{Method: "PUT", Path: "/api/catalog/entities/123", Body: map[string]any{"expected_version": 1}})
	data := res.StructuredContent.(map[string]any)
	if data["status"] != 409 || res.IsError || data["ok"] != false || data["error"] != "version_conflict" || hits.Load() != 1 {
		t.Fatal("conflict changed/replayed")
	}
	if strings.Contains(mustJSON(data), "mfp_private") {
		t.Fatal("credential leaked")
	}
	s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/entities", Headers: map[string]string{"Authorization": "Bearer attacker"}})
	if hits.Load() != 1 {
		t.Fatal("header override accepted")
	}
}

type failingTransport struct{ calls atomic.Int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, fmt.Errorf("uncertain upstream result")
}
func TestUnknownWriteNeverReplayed(t *testing.T) {
	s := New(testConfig("http://localhost"), nil)
	f := &failingTransport{}
	s.HTTP.Transport = f
	ctx := principalContext(Principal{PAT: "mfp_test"})
	r := s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/commits", Body: map[string]string{"id": "sealed"}})
	if f.calls.Load() != 1 || r.StructuredContent.(map[string]any)["outcome"] != "unknown" {
		t.Fatal("unknown write replayed or classified as unapplied")
	}
}
func TestKnownBusyReceiptPreserved(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, 503, map[string]any{"error": "transaction_busy", "applied": false})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	r := s.callAPI(principalContext(Principal{PAT: "pat"}), APIInput{Method: "POST", Path: "/api/catalog/commits"})
	v := r.StructuredContent.(map[string]any)
	d := v["data"].(map[string]any)
	if d["error"] != "transaction_busy" || d["applied"] != false || v["headers"].(map[string]string)["Retry-After"] != "1" {
		t.Fatal("busy contract lost")
	}
}
func TestMultipartAndRepeatedQuery(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Join(r.URL.Query()["kind"], ",") != "work,release" {
			t.Error("array query lost")
		}
		if e := r.ParseMultipartForm(1024); e != nil {
			t.Fatal(e)
		}
		file, _, e := r.FormFile("file")
		if e != nil {
			t.Fatal(e)
		}
		defer file.Close()
		b, _ := io.ReadAll(file)
		if string(b) != "bytes" {
			t.Error("binary lost")
		}
		writeJSON(w, 201, map[string]any{"ok": true})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	r := s.callAPI(principalContext(Principal{PAT: "pat"}), APIInput{Method: "POST", Path: "/api/storage/assets", Query: map[string]any{"kind": []any{"work", "release"}}, Multipart: []Part{{Name: "file", Filename: "test.bin", Base64: base64.StdEncoding.EncodeToString([]byte("bytes"))}}})
	if r.IsError {
		t.Fatal(r)
	}
}
func TestOriginAndDiscovery(t *testing.T) {
	s := New(testConfig("https://site.test"), nil)
	h := s.Handler()
	for _, method := range []string{"POST", "DELETE", "PATCH"} {
		r := httptest.NewRequest(method, "https://site.test/mcp/manage/api/approve", strings.NewReader("{}"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("CSRF accepted %s: %d", method, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "https://site.test/.well-known/oauth-protected-resource/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v["resource"] != "https://site.test/mcp" {
		t.Fatal("wrong resource")
	}
	r = httptest.NewRequest("POST", "https://site.test/mcp", strings.NewReader("{}"))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatal("OAuth discovery challenge missing")
	}
}
func TestMCPToolsThroughOfficialClient(t *testing.T) {
	s := New(testConfig("http://localhost"), nil)
	a, b := mcp.NewInMemoryTransports()
	ss, e := s.MCP.Connect(context.Background(), a, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, e := client.Connect(context.Background(), b, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	list, e := cs.ListTools(context.Background(), nil)
	if e != nil || len(list.Tools) != 16 {
		t.Fatalf("tool discovery: %v %v", list, e)
	}
	r, e := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "metafusion://workflow"})
	if e != nil || !strings.Contains(r.Contents[0].Text, "commit") {
		t.Fatal("workflow resource missing")
	}
}
func TestRedirectAndPKCEValidation(t *testing.T) {
	for _, r := range []string{"javascript:alert(1)", "http://evil.test/cb", "https://user:pass@site.test/cb", "https://site.test/cb#fragment", "//evil.test/cb"} {
		if validRedirect(r) {
			t.Errorf("unsafe redirect accepted %s", r)
		}
	}
	for _, r := range []string{"https://agent.test/cb", "http://127.0.0.1:1234/cb", "http://localhost/cb"} {
		if !validRedirect(r) {
			t.Errorf("safe redirect rejected %s", r)
		}
	}
}
