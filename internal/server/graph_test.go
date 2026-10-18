package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	gWork    = "00000000-0000-4000-8000-000000000001"
	gRelease = "00000000-0000-4000-8000-000000000002"
	gMedium  = "00000000-0000-4000-8000-000000000003"
	gTrack   = "00000000-0000-4000-8000-000000000004"
	gOther   = "00000000-0000-4000-8000-000000000005"
)

// fakeGraph: work -ownership-> (via release.work_id) release -> medium -> track,
// plus a semantic relation release -> other. The release has 150 links so its
// page needs a second request.
func fakeGraph(t *testing.T, queries *atomic.Int32) *httptest.Server {
	link := func(key, class, src, dst string) map[string]any {
		edge := map[string]any{"key": key, "rule_code": "structure:x", "class": class, "source_id": src, "target_id": dst}
		if class == "semantic" {
			edge["rule_code"] = "relation:edition_of"
			edge["version"] = 7
			edge["attributes"] = map[string]any{"credit_role": "publisher"}
		}
		return edge
	}
	adj := map[string][]map[string]any{
		gWork:    {link("w-r", "ownership", gWork, gRelease)},
		gRelease: {link("w-r", "ownership", gWork, gRelease), link("r-m", "ownership", gRelease, gMedium), link("rel-1", "semantic", gRelease, gOther)},
		gMedium:  {link("r-m", "ownership", gRelease, gMedium), link("m-t", "ownership", gMedium, gTrack)},
		gTrack:   {link("m-t", "ownership", gMedium, gTrack)},
		gOther:   {},
	}
	for i := 0; i < 150; i++ {
		adj[gRelease] = append(adj[gRelease], link("pad-"+string(rune('a'+i%26))+strings.Repeat("x", i/26), "reference", gRelease, gOther))
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/catalog/relationships/query":
			queries.Add(1)
			var body struct {
				IDs    []string `json:"ids"`
				Limit  int      `json:"limit"`
				Offset int      `json:"offset"`
			}
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &body)
			if len(body.IDs) > 20 {
				writeJSON(w, 400, map[string]any{"error": "invalid_ids"})
				return
			}
			var pages []any
			var un []string
			for _, id := range body.IDs {
				items, ok := adj[id]
				if !ok {
					un = append(un, id)
					continue
				}
				end := min(body.Offset+body.Limit, len(items))
				page := []map[string]any{}
				if body.Offset < len(items) {
					page = items[body.Offset:end]
				}
				pages = append(pages, map[string]any{"subject_id": id, "items": page, "limit": body.Limit, "offset": body.Offset, "has_more": end < len(items)})
			}
			writeJSON(w, 200, map[string]any{"definition_etag": "e1", "pages": pages, "entities": map[string]any{gWork: map[string]any{"kind": "work"}}, "unavailable_ids": un})
		default:
			writeJSON(w, 404, map[string]any{"error": "not_found"})
		}
	}))
}

func TestGraphReadPaginatesMergesAndTraverses(t *testing.T) {
	var queries atomic.Int32
	up := fakeGraph(t, &queries)
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	r := s.graphRead(ctx, graphInput{IDs: []string{gWork}, Depth: 3})
	m := structured(t, r)
	if m["complete"] != true {
		t.Fatal("expected complete", m["incomplete_reasons"], m["errors"])
	}
	visited := m["visited"].(map[string]any)
	if visited[gTrack] != float64(3) {
		t.Fatal("did not reach track at depth 3", visited)
	}
	edges := m["edges"].([]any)
	var sem map[string]any
	for _, e := range edges {
		if e.(map[string]any)["key"] == "rel-1" {
			sem = e.(map[string]any)
		}
	}
	if sem == nil || sem["category"] != "semantic" || sem["version"] != float64(7) || sem["attributes"].(map[string]any)["credit_role"] != "publisher" {
		t.Fatal("semantic edge context lost", sem)
	}
	counts := m["edge_counts"].(map[string]any)
	if counts["structural"] != float64(3) || counts["reference"] != float64(150) {
		t.Fatal("dedupe/pagination wrong", counts)
	}
	r = s.graphRead(ctx, graphInput{IDs: []string{gRelease}, Depth: 1})
	if m = structured(t, r); m["complete"] != true || m["edge_counts"].(map[string]any)["reference"] != float64(150) {
		t.Fatal("depth 1 release", m["incomplete_reasons"])
	}
}

func TestGraphReadBatchesAndReportsTruncation(t *testing.T) {
	var queries atomic.Int32
	up := fakeGraph(t, &queries)
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	ids := []string{gWork}
	for i := 0; i < 25; i++ {
		ids = append(ids, strings.Replace("00000000-0000-4000-8000-0000000001xx", "xx", string(rune('a'+i/10))+string(rune('0'+i%10)), 1))
	}
	m := structured(t, s.graphRead(ctx, graphInput{IDs: ids}))
	if queries.Load() != 2 {
		t.Fatal("expected two batches of <=20", queries.Load())
	}
	if m["complete"] != false || !strings.Contains(mustJSON(m["incomplete_reasons"]), "unavailable_ids") {
		t.Fatal("unavailable should make it incomplete", m["incomplete_reasons"])
	}
	m = structured(t, s.graphRead(ctx, graphInput{IDs: []string{gWork}, Depth: 3, MaxEntities: 2}))
	if m["complete"] != false || !strings.Contains(mustJSON(m["incomplete_reasons"]), "max_entities_reached") {
		t.Fatal("entity cap not reported", m["incomplete_reasons"])
	}
}

func TestGraphToolValidation(t *testing.T) {
	s := New(testConfig("http://127.0.0.1:1"), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)
	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "catalog_graph_read", Arguments: map[string]any{"ids": []string{"x"}, "depth": 9}})
	if e != nil || r.IsError {
		t.Fatal(e)
	}
	if m := structured(t, r); m["error"] != "invalid_tool_arguments" || len(m["details"].([]any)) != 2 {
		t.Fatal(m)
	}
}

func TestContractModes(t *testing.T) {
	doc := map[string]any{
		"info": map[string]any{"title": "c"},
		"paths": map[string]any{
			"/catalog/workspaces/{id}/changes": map[string]any{"put": map[string]any{"summary": strings.Repeat("long ", 100), "requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/WorkspaceChanges"}}}}, "responses": map[string]any{"200": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/WorkspaceDTO"}}}}}}},
			"/catalog/workspaces/{id}/commits": map[string]any{"post": map[string]any{"summary": "Commit the workspace", "responses": map[string]any{"201": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/CommitDTO"}}}}}}},
		},
		"components": map[string]any{"schemas": map[string]any{
			"WorkspaceChanges":   map[string]any{"properties": map[string]any{"operations": map[string]any{"items": map[string]any{"$ref": "#/components/schemas/WorkspaceOperation"}}}},
			"WorkspaceOperation": map[string]any{}, "WorkspaceDTO": map[string]any{}, "CommitDTO": map[string]any{}, "Unrelated": map[string]any{"big": strings.Repeat("x", 5000)},
		}},
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, doc) }))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	m := structured(t, s.contract(ctx, nil))
	ops := m["operations"].([]any)
	op := ops[0].(map[string]any)
	if m["mode"] != "summary" || op["request"] != "WorkspaceChanges" || op["response"] != "WorkspaceDTO" || len([]rune(op["summary"].(string))) > 161 || strings.Contains(mustJSON(m), strings.Repeat("x", 100)) {
		t.Fatal("bad summary", m)
	}
	if len(ops) != 2 || ops[1].(map[string]any)["response"] != "CommitDTO" {
		t.Fatal("commit DTO missing from index", ops)
	}
	m = structured(t, s.contract(ctx, json.RawMessage(`{"path":"/api/catalog/workspaces/{id}/changes","method":"PUT"}`)))
	sc := m["schemas"].(map[string]any)
	if sc["WorkspaceOperation"] == nil || sc["WorkspaceDTO"] == nil || sc["CommitDTO"] != nil || sc["Unrelated"] != nil {
		t.Fatal("operation schemas closure", sc)
	}
	if m = structured(t, s.contract(ctx, json.RawMessage(`{"mode":"full"}`))); m["catalog"] == nil {
		t.Fatal("full mode")
	}
	if m = structured(t, s.contract(ctx, json.RawMessage(`{"path":"/api/nope"}`))); m["status"] != float64(404) {
		t.Fatal("missing op", m)
	}
}
