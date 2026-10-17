package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWikiToolForwardsReadAndPreservesEvidence(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/external-sources/wiki" {
			t.Errorf("wrong operation: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mfp_wiki" || r.Header.Get("Cookie") != "" {
			t.Error("platform credential was lost or a cookie leaked")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["site"] != "wikipedia" || body["title"] != "MusicBrainz" || body["language"] != "en" {
			t.Error("wiki request changed", body)
		}
		writeJSON(w, 200, map[string]any{
			"url": "https://en.wikipedia.org/wiki/MusicBrainz", "fetched_at": "2026-10-18T00:00:00Z", "content_hash": "sha256:test",
			"content_type": "application/json", "text": "Original content", "attribution": "Wikipedia contributors", "license": "CC BY-SA 4.0",
		})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	s.WebHTTP.Transport = webRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("wiki tool must use the platform adapter, not fetch the public web itself")
		return nil, nil
	})
	ctx := principalContext(Principal{PAT: "mfp_wiki", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_wiki", Arguments: map[string]any{"site": "wikipedia", "language": "en", "title": "MusicBrainz"}})
	if err != nil {
		t.Fatal(err)
	}
	m := structured(t, r)
	data, _ := m["data"].(map[string]any)
	if r.IsError || m["ok"] != true || calls.Load() != 1 || data["license"] != "CC BY-SA 4.0" || data["attribution"] != "Wikipedia contributors" || data["content_hash"] != "sha256:test" || data["text"] != "Original content" {
		t.Fatal("read or fetch evidence changed", m)
	}
}

func TestWikiToolRejectsInvalidRequestsBeforeUpstream(t *testing.T) {
	s := New(testConfig("https://platform.example"), nil)
	s.HTTP.Transport = webRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid arguments reached upstream")
		return nil, nil
	})
	ctx := principalContext(Principal{Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)
	for _, args := range []map[string]any{
		{"site": "wikipedia"},
		{"site": "wikipedia", "title": "A", "query": "A"},
		{"site": "", "title": "A"},
		{"site": "moegirl", "query": ""},
		{"site": "wikipedia", "title": strings.Repeat("A", 501)},
		{"site": "wikipedia", "query": strings.Repeat("A", 501)},
		{"site": "wikipedia", "title": "A", "language": "en_US"},
		{"site": "wikipedia", "query": "A", "limit": 21},
		{"site": "wikipedia", "title": "A", "url": "https://other.example"},
	} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_wiki", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if m := structured(t, r); m["error"] != "invalid_tool_arguments" {
			t.Fatal("invalid request accepted", args, m)
		}
	}
	if m := structured(t, s.callAPI(context.Background(), APIInput{Method: "POST", Path: "/api/external-sources/wiki"})); m["error"] != "authentication_required" {
		t.Fatal("anonymous request accepted", m)
	}
}

func TestWikiAndFetchAreOnlyReadOnlyPosts(t *testing.T) {
	for _, path := range []string{"/api/external-sources/wiki", "/api/external-sources/fetch"} {
		if !readOperation("POST", path) {
			t.Fatal("read-only POST not allowed", path)
		}
		for _, method := range []string{"PUT", "PATCH", "DELETE"} {
			if readOperation(method, path) {
				t.Fatal("write method marked read-only", method, path)
			}
		}
	}
}

func TestWikiUpstreamPermissionErrorIsPreserved(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 403, map[string]any{"error": "permission_denied", "permission": "catalog.read"})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}, PAT: "mfp_denied"})
	r := s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/external-sources/wiki", Body: map[string]any{"site": "moegirl", "query": "音楽"}})
	m := structured(t, r)
	if m["ok"] != false || m["status"] != float64(403) || m["error"] != "permission_denied" || calls.Load() != 1 {
		t.Fatal("upstream denial was rewritten or replayed", m)
	}
}
