package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCommitConflictSurfacedOnPush(t *testing.T) {
	field := map[string]any{
		"operation": 0, "pointer": "/title", "base": "base", "current": "remote", "proposed": "local",
		"kind": "concurrent_edit", "suggestions": []any{"manual_merge", "take_current", "rebase_keep_mine"},
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/catalog/commits" && r.Method == "POST":
			writeJSON(w, 409, map[string]any{
				"error": "commit_conflict", "applied": false,
				"conflict": map[string]any{
					"operation": 0, "id": gWork, "base_version": 1, "current_version": 3,
					"conflicts": []any{field},
				},
				"conflicts": []any{field},
			})
		case r.URL.Path == "/api/catalog/commits/preview" && r.Method == "POST":
			writeJSON(w, 409, map[string]any{
				"error": "commit_conflict", "applied": false,
				"conflict":  map[string]any{"operation": 0, "id": gWork, "base_version": 1, "current_version": 2, "conflicts": []any{field}},
				"conflicts": []any{field},
			})
		default:
			writeJSON(w, 404, map[string]any{"error": "not_found"})
		}
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"catalog.entity.edit"}}})
	cs := connect(t, s, ctx)

	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit_push", Arguments: map[string]any{"body": map[string]any{"id": gWork}}})
	if e != nil {
		t.Fatal(e)
	}
	m := structured(t, r)
	if r.IsError || m["ok"] != false || m["error"] != "commit_conflict" || m["status"] != float64(409) {
		t.Fatal("push conflict not structured", m)
	}
	conflicts, _ := m["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatal("top-level conflicts missing", m)
	}
	fc, _ := conflicts[0].(map[string]any)
	if fc["pointer"] != "/title" || fc["base"] != "base" || fc["current"] != "remote" || fc["proposed"] != "local" {
		t.Fatal("field values", fc)
	}
	if note, _ := m["conflicts_note"].(string); note == "" {
		t.Fatal("conflicts_note missing")
	}

	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit_preview", Arguments: map[string]any{"body": map[string]any{"id": gWork}}})
	m = structured(t, r)
	if m["ok"] != false || m["error"] != "commit_conflict" {
		t.Fatal("preview conflict", m)
	}
	if conflicts, _ = m["conflicts"].([]any); len(conflicts) != 1 {
		t.Fatal("preview conflicts", m)
	}

	// Non-conflict 409 must not invent a conflicts array via the shortcut path.
	// Identity conflicts stay in details only.
}

func TestWithCommitConflictsIgnoresOtherErrors(t *testing.T) {
	r := expectedError(400, "invalid_limit", "bad", nil, nil)
	out := withCommitConflicts(r)
	m := structured(t, out)
	if _, ok := m["conflicts"]; ok {
		t.Fatal("non-conflict gained conflicts", m)
	}
	okRes := result(map[string]any{"ok": true, "status": 200, "data": map[string]any{}}, false)
	if withCommitConflicts(okRes).StructuredContent.(map[string]any)["conflicts"] != nil {
		t.Fatal("success gained conflicts")
	}
}
