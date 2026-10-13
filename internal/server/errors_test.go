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

func structured(t *testing.T, r *mcp.CallToolResult) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &m); e != nil {
		t.Fatal(e)
	}
	return m
}

func connect(t *testing.T, s *Server, ctx context.Context) *mcp.ClientSession {
	t.Helper()
	a, b := mcp.NewInMemoryTransports()
	ss, e := s.MCP.Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ss.Close() })
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestExpectedAPIErrorsAreStructured(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("limit") == "0":
			w.Header().Set("X-Request-ID", "req-400")
			writeJSON(w, 400, map[string]any{"error": "invalid_limit", "message": "limit must be between 1 and 100"})
		case r.URL.Path == "/api/catalog/entities" && r.Method == "POST":
			writeJSON(w, 409, map[string]any{"error": "identity_candidates_changed", "identity_review": map[string]any{"candidates": []any{"x"}}, "request_id": "req-409"})
		case r.URL.Path == "/api/catalog/commits":
			w.Header().Set("Retry-After", "7")
			writeJSON(w, 503, map[string]any{"error": "transaction_busy", "applied": false})
		case r.URL.Path == "/api/catalog/boom":
			writeJSON(w, 500, map[string]any{"error": "internal"})
		default:
			writeJSON(w, 200, map[string]any{"items": []any{}})
		}
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"catalog.entity.edit"}}})

	r := s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/entities", Query: map[string]any{"limit": "0"}})
	m := structured(t, r)
	if r.IsError || m["ok"] != false || m["status"] != float64(400) || m["error"] != "invalid_limit" || m["request_id"] != "req-400" || m["message"] != "limit must be between 1 and 100" {
		t.Fatal("400 not structured", m)
	}
	r = s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/entities", Body: map[string]any{}})
	m = structured(t, r)
	details, _ := m["details"].(map[string]any)
	if r.IsError || m["error"] != "identity_candidates_changed" || m["request_id"] != "req-409" || details["identity_review"] == nil {
		t.Fatal("409 not structured", m)
	}
	r = s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/commits", Body: map[string]any{}})
	m = structured(t, r)
	if r.IsError || m["status"] != float64(503) || m["retry_after"] != float64(7) || m["error"] != "transaction_busy" {
		t.Fatal("503 not structured", m)
	}
	r = s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/boom"})
	if !r.IsError || structured(t, r)["ok"] != false {
		t.Fatal("internal 500 must remain a tool failure")
	}
	r = s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/entities"})
	if m = structured(t, r); r.IsError || m["ok"] != true || m["status"] != float64(200) {
		t.Fatal("success lacks ok:true", m)
	}
	r = s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/internal/x"})
	if m = structured(t, r); r.IsError || m["status"] != float64(400) {
		t.Fatal("local validation error not structured", m)
	}
	// Transport failures stay exceptions.
	s.HTTP = &http.Client{Transport: &failingTransport{}}
	if r = s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/entities"}); !r.IsError {
		t.Fatal("transport failure hidden")
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if n, ok := parseRetryAfter("120"); !ok || n != 120 {
		t.Fatal(n)
	}
	if _, ok := parseRetryAfter("soon"); ok {
		t.Fatal("garbage accepted")
	}
	if _, ok := parseRetryAfter("Wed, 21 Oct 2015 07:28:00 GMT"); !ok {
		t.Fatal("http-date rejected")
	}
}

const uuid1 = "11111111-2222-4333-8444-555555555555"

func validCommit() map[string]any {
	return map[string]any{
		"definitions_etag": "etag", "edit_note": "checked",
		"sources": []any{map[string]any{"kind": "url", "url": "https://example.com/a", "citation": "Label page", "key": "label"}},
		"operations": []any{
			map[string]any{"target": "entity", "action": "create", "ref": "rel1", "reviewed_candidate_ids": []any{}, "document": map[string]any{"kind": "release", "title": "rye", "work_id": uuid1, "external_ids": map[string]any{"vgmdb": "1"}, "attributes": map[string]any{"catalog_number": "TECI-1"}}, "source_refs": []any{map[string]any{"source": "label", "pointers": []any{"/attributes/catalog_number"}}}},
			map[string]any{"target": "relation", "action": "create", "ref": "r", "document": map[string]any{"type": "edition_of", "source_id": map[string]any{"$ref": "rel1"}, "target_id": uuid1}, "source_refs": []any{map[string]any{"source": "label"}}},
			map[string]any{"target": "entity", "action": "update", "id": uuid1, "base_version": 3, "patch": []any{map[string]any{"path": "/title", "value": "x"}, map[string]any{"path": "/attributes/old", "remove": true}}, "source_refs": []any{map[string]any{"source": "label", "pointers": []any{"/title"}}}},
		},
	}
}

func roundTrip(v any) any {
	b, _ := json.Marshal(v)
	var out any
	json.Unmarshal(b, &out)
	return out
}

func TestCommitPrepareSchema(t *testing.T) {
	schema := commitPrepareSchema()
	if issues := validateSchema(schema, roundTrip(validCommit())); len(issues) != 0 {
		t.Fatal("valid commit rejected", issues)
	}
	bad := validCommit()
	bad["sources"] = []any{map[string]any{"kind": "self"}}
	ops := bad["operations"].([]any)
	ops[0].(map[string]any)["document"].(map[string]any)["bogus"] = 1
	delete(ops[0].(map[string]any), "reviewed_candidate_ids")
	ops[1].(map[string]any)["document"].(map[string]any)["source_id"] = map[string]any{"$ref": "1bad"}
	ops[2].(map[string]any)["patch"] = []any{map[string]any{"path": "/a", "value": 1, "remove": true}}
	ops[2].(map[string]any)["id"] = "not-a-uuid"
	issues := validateSchema(schema, roundTrip(bad))
	got := map[string]bool{}
	for _, i := range issues {
		got[i.Path] = true
	}
	for _, want := range []string{"/sources/0/kind", "/sources/0/citation", "/operations/0/document/bogus", "/operations/0/reviewed_candidate_ids", "/operations/1/document/source_id/$ref", "/operations/2/id", "/operations/2/patch/0/remove"} {
		if !got[want] {
			t.Error("missing issue at", want, issues)
		}
	}
	empty := validateSchema(schema, map[string]any{})
	if len(empty) != 4 {
		t.Fatal("required fields not all reported", empty)
	}
}

func TestQuerySchemas(t *testing.T) {
	cand := withDefs(shortcutSchema(candidateQuerySchema()))
	if is := validateSchema(cand, roundTrip(map[string]any{"body": map[string]any{"kind": "work", "titles": []any{"day by day"}, "limit": 50}})); len(is) != 0 {
		t.Fatal(is)
	}
	is := validateSchema(cand, roundTrip(map[string]any{"body": map[string]any{"kind": "song", "performed_by": "x", "external_ids": []any{map[string]any{"provider": "x"}}, "limit": 0}}))
	if len(is) != 4 {
		t.Fatal("candidate issues", is)
	}
	rel := withDefs(shortcutSchema(relationshipQuerySchema()))
	if is := validateSchema(rel, roundTrip(map[string]any{"body": map[string]any{"ids": []any{uuid1}, "direction": "both", "limit": 100, "offset": 0}})); len(is) != 0 {
		t.Fatal(is)
	}
	if is := validateSchema(rel, roundTrip(map[string]any{"body": map[string]any{"ids": []any{}, "direction": "up", "limit": 101}})); len(is) != 3 {
		t.Fatal("relationship issues", is)
	}
}

func TestShortcutToolValidationBeforeUpstream(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeJSON(w, 200, map[string]any{"pages": []any{}})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)
	tools, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "metafusion_commit_prepare" && !strings.Contains(mustJSON(tool.InputSchema), "CommitOperation") {
			t.Fatal("prepare schema not published")
		}
	}
	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_relationships", Arguments: map[string]any{"body": map[string]any{"ids": []string{"nope"}}}})
	if e != nil || r.IsError {
		t.Fatal("validation surfaced as tool failure", e)
	}
	m := structured(t, r)
	if m["error"] != "invalid_tool_arguments" || m["status"] != float64(400) || hits.Load() != 0 || !strings.Contains(mustJSON(m["details"]), "/body/ids/0") {
		t.Fatal("bad validation result", m)
	}
	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_relationships", Arguments: map[string]any{"body": map[string]any{"ids": []string{uuid1}}}})
	if m = structured(t, r); m["ok"] != true || hits.Load() != 1 {
		t.Fatal("valid query not forwarded", m)
	}
	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit_prepare", Arguments: map[string]any{"operations": []any{}}})
	if m = structured(t, r); r.IsError || m["error"] != "invalid_tool_arguments" {
		t.Fatal("prepare validation", m)
	}
}

func TestSourceRefsSchemaShape(t *testing.T) {
	schema := commitPrepareSchema()
	c := validCommit()
	if is := validateSchema(schema, roundTrip(c)); len(is) != 0 {
		t.Fatal(is)
	}
	ops := c["operations"].([]any)
	ops[0].(map[string]any)["source_refs"] = []any{}
	ops[2].(map[string]any)["source_refs"] = []any{map[string]any{"source": "label", "pointers": []any{"title"}}}
	c["sources"].([]any)[0].(map[string]any)["key"] = "1bad"
	got := map[string]bool{}
	for _, i := range validateSchema(schema, roundTrip(c)) {
		got[i.Path] = true
	}
	for _, want := range []string{"/operations/0/source_refs", "/operations/2/source_refs/0/pointers/0", "/sources/0/key"} {
		if !got[want] {
			t.Error("missing", want, got)
		}
	}
}

func TestSourceRefsRequiredEverywhere(t *testing.T) {
	schema := commitPrepareSchema()
	c := validCommit()
	delete(c["operations"].([]any)[1].(map[string]any), "source_refs")
	delete(c["sources"].([]any)[0].(map[string]any), "key")
	got := map[string]bool{}
	for _, i := range validateSchema(schema, roundTrip(c)) {
		got[i.Path] = true
	}
	for _, want := range []string{"/operations/1/source_refs", "/sources/0/key"} {
		if !got[want] {
			t.Error("missing required issue", want, got)
		}
	}
}

func TestSourceRefIssues(t *testing.T) {
	if is := sourceRefIssues(roundTrip(validCommit()).(map[string]any)); len(is) != 0 {
		t.Fatal("valid commit flagged", is)
	}
	c := validCommit()
	c["sources"] = append(c["sources"].([]any),
		map[string]any{"kind": "url", "url": "https://example.com/b", "citation": "Unused", "key": "unused"},
		map[string]any{"kind": "url", "url": "https://example.com/c", "citation": "Dup", "key": "label"})
	ops := c["operations"].([]any)
	ops[1].(map[string]any)["source_refs"] = []any{map[string]any{"source": "nope"}}
	ops[2].(map[string]any)["source_refs"] = []any{map[string]any{"source": "label"}, map[string]any{"source": "label"}}
	got := map[string]string{}
	for _, i := range sourceRefIssues(roundTrip(c).(map[string]any)) {
		got[i.Path] = i.Message
	}
	want := map[string]string{
		"/sources/2/key":                     "duplicate_source_key",
		"/sources/1":                         "source_unused",
		"/operations/1/source_refs/0/source": "unknown_source_ref",
		"/operations/2/source_refs/1/source": "duplicate_source_ref",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q (all: %v)", k, got[k], v, got)
		}
	}
}
