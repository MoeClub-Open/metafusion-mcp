package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestQualityAuditTool(t *testing.T) {
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/catalog/entities/" + gWork + "/quality":
			writeJSON(w, 200, map[string]any{"entity_id": gWork, "status": "complete", "issues": []any{}})
		case "/api/catalog/quality/batch":
			writeJSON(w, 200, map[string]any{"reports": []any{}, "errors": map[string]any{gOther: "not_found"}})
		case "/api/catalog/commits/preview":
			writeJSON(w, 200, map[string]any{"applied": false, "preview": []any{map[string]any{"target": "entity", "after": map[string]any{"title": "Work"}}}})
		default:
			writeJSON(w, 404, map[string]any{"error": "not_found"})
		}
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)

	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "catalog_quality_audit", Arguments: map[string]any{"ids": []string{gWork}}})
	if e != nil {
		t.Fatal(e)
	}
	if m := structured(t, r); m["ok"] != true || m["data"].(map[string]any)["status"] != "complete" {
		t.Fatal(m)
	}
	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "catalog_quality_audit", Arguments: map[string]any{"ids": []string{gWork, gOther}}})
	if m := structured(t, r); m["ok"] != true || m["data"].(map[string]any)["errors"].(map[string]any)[gOther] != "not_found" {
		t.Fatal(m)
	}
	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "catalog_quality_audit", Arguments: map[string]any{"ids": []string{}}})
	if m := structured(t, r); m["ok"] != false || m["error"] != "invalid_tool_arguments" {
		t.Fatal(m)
	}
	if len(paths) != 2 || paths[0] != "GET /api/catalog/entities/"+gWork+"/quality" || paths[1] != "POST /api/catalog/quality/batch" {
		t.Fatal(paths)
	}

	r, e = cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit_preview", Arguments: map[string]any{"body": map[string]any{"id": "x"}}})
	if e != nil {
		t.Fatal(e)
	}
	m := structured(t, r)
	data, _ := m["data"].(map[string]any)
	changes, _ := data["preview"].([]any)
	if m["ok"] != true || data["applied"] != false || len(changes) != 1 {
		t.Fatal("preview must forward changes", m)
	}
	if len(paths) != 3 || paths[2] != "POST /api/catalog/commits/preview" {
		t.Fatal("preview must not request a separate audit", paths)
	}
}
