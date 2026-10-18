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
		case r.URL.Path == "/api/catalog/workspaces/"+uuid1+"/changes" && r.Method == "PUT":
			writeJSON(w, 409, map[string]any{"error": "workspace_version_conflict", "workspace_version": 3, "request_id": "req-409"})
		case r.URL.Path == "/api/catalog/workspaces/"+uuid1+"/commits":
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
	r = s.callAPI(ctx, APIInput{Method: "PUT", Path: "/api/catalog/workspaces/" + uuid1 + "/changes", Body: map[string]any{}})
	m = structured(t, r)
	details, _ := m["details"].(map[string]any)
	if r.IsError || m["error"] != "workspace_version_conflict" || m["request_id"] != "req-409" || details["workspace_version"] != float64(3) {
		t.Fatal("409 not structured", m)
	}
	r = s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/workspaces/" + uuid1 + "/commits", Body: map[string]any{}})
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

func validStage() map[string]any {
	return map[string]any{
		"workspace_id": uuid1, "expected_version": 2,
		"operations": []any{
			map[string]any{"target": "entity", "action": "create", "ref": "rel1", "document": map[string]any{"kind": "release", "title": "rye", "work_id": uuid1, "external_links": []any{map[string]any{"kind": "vgmdb", "url": "mf://external/vgmdb/1", "external_id": "1"}}, "attributes": map[string]any{"catalog_number": "TECI-1"}}},
			map[string]any{"target": "relation", "action": "create", "ref": "r", "document": map[string]any{"type": "edition_of", "source_id": map[string]any{"$ref": "rel1"}, "target_id": uuid1}},
			map[string]any{"target": "entity", "action": "update", "id": uuid1, "patch": []any{map[string]any{"path": "/title", "value": "x"}, map[string]any{"path": "/attributes/old", "remove": true}}},
		},
	}
}

func roundTrip(v any) any {
	b, _ := json.Marshal(v)
	var out any
	json.Unmarshal(b, &out)
	return out
}

func TestWorkspaceStageSchema(t *testing.T) {
	schema := workspaceStageSchema()
	if issues := validateSchema(schema, roundTrip(validStage())); len(issues) != 0 {
		t.Fatal("valid workspace stage rejected", issues)
	}
	bad := validStage()
	ops := bad["operations"].([]any)
	ops[0].(map[string]any)["document"] = "not an object"
	ops[1].(map[string]any)["ref"] = "1bad"
	ops[2].(map[string]any)["patch"] = []any{map[string]any{"path": "/a", "value": 1, "remove": true}}
	ops[2].(map[string]any)["id"] = "not-a-uuid"
	issues := validateSchema(schema, roundTrip(bad))
	got := map[string]bool{}
	for _, i := range issues {
		got[i.Path] = true
	}
	for _, want := range []string{"/operations/0/document", "/operations/1/ref", "/operations/2/id", "/operations/2/patch/0/remove"} {
		if !got[want] {
			t.Error("missing issue at", want, issues)
		}
	}
	if empty := validateSchema(schema, map[string]any{}); len(empty) != 3 {
		t.Fatal("required fields not all reported", empty)
	}
}

func TestArticleAndOptionalHistoricalMetadataSchema(t *testing.T) {
	schema := workspaceStageSchema()
	stage := validStage()
	doc := stage["operations"].([]any)[0].(map[string]any)["document"].(map[string]any)
	doc["translations"] = map[string]any{"zh-CN": map[string]any{"title": "条目", "body": "## 背景\n正文与结构化事实一同修订"}}
	doc["pictures"] = []any{map[string]any{"url": "https://example.com/cover.jpg"}}
	doc["contents"] = []any{map[string]any{"expression_id": uuid1, "position": 1}}
	if issues := validateSchema(schema, roundTrip(stage)); len(issues) != 0 {
		t.Fatal("source-free metadata rejected", issues)
	}
	// Existing source metadata can travel with the document without a separate
	// evidence or candidate-review workflow.
	doc["pictures"].([]any)[0].(map[string]any)["source"] = map[string]any{"kind": "url", "url": "https://example.com"}
	link := doc["external_links"].([]any)[0].(map[string]any)
	link["source_refs"] = []any{map[string]any{"source": "historic", "pointers": []any{"/external_links/0"}}}
	link["role"] = "official"
	if issues := validateSchema(schema, roundTrip(stage)); len(issues) != 0 {
		t.Fatal("historical metadata rejected", issues)
	}
	// Dynamic document fields are validated once by the native catalog API.
	doc["state"] = "active"
	doc["subjects"] = nil
	if issues := validateSchema(schema, roundTrip(stage)); len(issues) != 0 {
		t.Fatal("native snapshot shape rejected", issues)
	}
}

func TestQuerySchemas(t *testing.T) {
	cand := withDefs(shortcutSchema(candidateQuerySchema()))
	if is := validateSchema(cand, roundTrip(map[string]any{"body": map[string]any{"kind": "work", "titles": []any{"day by day"}, "limit": 50}})); len(is) != 0 {
		t.Fatal(is)
	}
	is := validateSchema(cand, roundTrip(map[string]any{"body": map[string]any{"kind": "song", "performed_by": "x", "external_links": []any{map[string]any{"kind": "x", "value": "1"}}, "limit": 0}}))
	if len(is) != 3 {
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
		if tool.Name == "metafusion_workspace_stage" && !strings.Contains(mustJSON(tool.InputSchema), "WorkspaceOperation") {
			t.Fatal("workspace operation schema not published")
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
	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_workspace_stage", Arguments: map[string]any{"operations": []any{}}})
	if m = structured(t, r); r.IsError || m["error"] != "invalid_tool_arguments" {
		t.Fatal("stage validation", m)
	}
}

func TestWorkspaceSchemaHasNoEvidencePrerequisite(t *testing.T) {
	body := roundTrip(validStage()).(map[string]any)
	if issues := validateSchema(workspaceStageSchema(), body); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, forbidden := range []string{"definitions_etag", "edit_note", "sources"} {
		body[forbidden] = "retired workflow"
		if issues := validateSchema(workspaceStageSchema(), body); len(issues) == 0 {
			t.Fatal("retired workflow field accepted", forbidden)
		}
		delete(body, forbidden)
	}
}
