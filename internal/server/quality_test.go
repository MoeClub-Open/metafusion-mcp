package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
		case "/api/catalog/workspaces/" + gRelease + "/changes":
			writeJSON(w, 200, map[string]any{"workspace": map[string]any{"id": gRelease, "version": 2, "dirty_count": 1}, "refs": map[string]any{}})
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

	tools, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "catalog_quality_audit" && !strings.Contains(tool.Description, "never block") {
			t.Fatal("quality tool lacks advisory-only guidance", tool.Description)
		}
	}
	editContext := principalContext(Principal{PAT: "mfp_edit", Grant: Grant{UserID: "u", Scopes: []string{"catalog.entity.edit"}}})
	editClient := connect(t, s, editContext)
	r, e = editClient.CallTool(editContext, &mcp.CallToolParams{Name: "metafusion_workspace_stage", Arguments: map[string]any{"workspace_id": gRelease, "expected_version": 1, "operations": []any{map[string]any{"target": "entity", "action": "update", "id": gWork, "patch": []any{map[string]any{"path": "/title", "value": "Work"}}}}}})
	if e != nil {
		t.Fatal(e)
	}
	m := structured(t, r)
	data, _ := m["data"].(map[string]any)
	workspace, _ := data["workspace"].(map[string]any)
	if m["ok"] != true || workspace["dirty_count"] != float64(1) {
		t.Fatal("workspace stage must forward changes", m)
	}
	if len(paths) != 3 || paths[2] != "PUT /api/catalog/workspaces/"+gRelease+"/changes" {
		t.Fatal("workspace stage must not request a separate audit", paths)
	}
}
