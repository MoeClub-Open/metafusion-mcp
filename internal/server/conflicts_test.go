package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkspaceConflictSurfacedOnPushAndRebase(t *testing.T) {
	field := map[string]any{
		"target": "entity", "id": gWork, "pointer": "/title", "base": "base", "current": "remote", "proposed": "local", "current_version": 3,
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/catalog/workspaces/"+gRelease+"/push" && r.Method == "POST":
			writeJSON(w, 409, map[string]any{
				"error": "workspace_conflict", "applied": false,
				"conflicts": []any{field},
			})
		case r.URL.Path == "/api/catalog/workspaces/"+gRelease+"/rebase" && r.Method == "POST":
			writeJSON(w, 409, map[string]any{
				"error": "workspace_conflict", "applied": false,
				"details": map[string]any{"conflict": map[string]any{"conflicts": []any{field}}},
			})
		default:
			writeJSON(w, 404, map[string]any{"error": "not_found"})
		}
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"catalog.entity.edit"}}})
	cs := connect(t, s, ctx)

	for _, action := range []string{"push", "rebase"} {
		t.Run(action, func(t *testing.T) {
			args := map[string]any{"workspace_id": gRelease, "expected_version": 4, "expected_head": gMedium}
			if action == "rebase" {
				args["id"] = gOther
			}
			r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_workspace_" + action, Arguments: args})
			if e != nil {
				t.Fatal(e)
			}
			m := structured(t, r)
			if r.IsError || m["ok"] != false || m["error"] != "workspace_conflict" || m["status"] != float64(409) {
				t.Fatal("workspace conflict not structured", m)
			}
			conflicts, _ := m["conflicts"].([]any)
			if len(conflicts) != 1 {
				t.Fatal("top-level conflicts missing", m)
			}
			fc, _ := conflicts[0].(map[string]any)
			if fc["target"] != "entity" || fc["id"] != gWork || fc["pointer"] != "/title" || fc["base"] != "base" || fc["current"] != "remote" || fc["proposed"] != "local" || fc["current_version"] != float64(3) {
				t.Fatal("conflict field values lost", fc)
			}
			if note, _ := m["conflicts_note"].(string); !strings.Contains(note, "workspace_rebase") {
				t.Fatal("resolution guidance missing", m)
			}
		})
	}
}

func TestWithWorkspaceConflictsIgnoresOtherErrors(t *testing.T) {
	for _, code := range []string{"invalid_limit", "version_conflict", "workspace_version_conflict", "source_head_changed", "identity_conflict"} {
		r := expectedError(409, code, "bad", map[string]any{"conflicts": []any{"not a workspace conflict"}}, nil)
		out := withWorkspaceConflicts(r)
		m := structured(t, out)
		if _, ok := m["conflicts"]; ok {
			t.Fatal("other error gained workspace conflicts", m)
		}
	}
	okRes := result(map[string]any{"ok": true, "status": 200, "data": map[string]any{}}, false)
	if withWorkspaceConflicts(okRes).StructuredContent.(map[string]any)["conflicts"] != nil {
		t.Fatal("success gained conflicts")
	}
}
