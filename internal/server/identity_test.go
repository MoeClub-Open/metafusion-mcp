package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIdentityCheckTool(t *testing.T) {
	var gotBody map[string]any
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/api/catalog/entities/candidates/batch" {
			writeJSON(w, 404, map[string]any{"error": "not_found"})
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		writeJSON(w, 200, map[string]any{"results": []any{
			map[string]any{
				"key": "song1", "total": 1, "complete": true,
				"items": []any{map[string]any{
					"matched": map[string]any{"id": gWork, "kind": "work", "title": "Shared"},
					"assessment": map[string]any{
						"matched_fields":     []any{"title_exact", "ctx_creator"},
						"confidence":         0.5,
						"recommended_action": "review",
						"context_summary":    map[string]any{"creator": []any{gOther}},
					},
				}},
			},
		}})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)

	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "catalog_identity_check", Arguments: map[string]any{
		"queries": []any{map[string]any{
			"key": "song1", "kind": "work", "titles": []any{"Shared"},
			"external_links": []any{map[string]any{"kind": "wikidata", "value": "Q123"}},
			"context":        map[string]any{"creator": []any{gOther}, "catalog_number": []any{"CAT-1"}},
		}},
	}})
	if e != nil {
		t.Fatal(e)
	}
	m := structured(t, r)
	if m["ok"] != true {
		t.Fatal(m)
	}
	data := m["data"].(map[string]any)
	results := data["results"].([]any)
	if len(results) != 1 {
		t.Fatal(results)
	}
	item := results[0].(map[string]any)
	if item["key"] != "song1" {
		t.Fatal(item)
	}
	assess := item["items"].([]any)[0].(map[string]any)["assessment"].(map[string]any)
	if assess["recommended_action"] != "review" {
		t.Fatal(assess)
	}
	if len(paths) != 1 || paths[0] != "POST /api/catalog/entities/candidates/batch" {
		t.Fatal(paths)
	}
	queries := gotBody["queries"].([]any)
	if len(queries) != 1 || queries[0].(map[string]any)["key"] != "song1" {
		t.Fatal(gotBody)
	}
	links := queries[0].(map[string]any)["external_links"].([]any)
	if links[0].(map[string]any)["kind"] != "wikidata" || links[0].(map[string]any)["value"] != "Q123" {
		t.Fatal("external link identity changed in transit", gotBody)
	}

	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "catalog_identity_check", Arguments: map[string]any{"queries": []any{}}})
	if m := structured(t, r); m["ok"] != false || m["error"] != "invalid_tool_arguments" {
		t.Fatal(m)
	}
}
