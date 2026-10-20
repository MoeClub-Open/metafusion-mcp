package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCollaborationToolsForwardNativeMethodsBodiesAndQueries(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		args, body         map[string]any
		query              string
	}{
		{"workspace_create", "POST", "/workspaces", map[string]any{"name": "共同編集", "entity_ids": []string{gWork}, "relation_ids": []string{gOther}}, map[string]any{"name": "共同編集", "entity_ids": []string{gWork}, "relation_ids": []string{gOther}}, ""},
		{"workspace_list", "GET", "/workspaces", map[string]any{"limit": 5, "offset": 10}, nil, "limit=5&offset=10"},
		{"workspace_get", "GET", "/workspaces/" + gWork, map[string]any{"workspace_id": gWork}, nil, ""},
		{"workspace_edit", "PATCH", "/workspaces/" + gWork, map[string]any{"workspace_id": gWork, "expected_version": 4, "name": "renamed", "state": "archived"}, map[string]any{"expected_version": 4, "name": "renamed", "state": "archived"}, ""},
		{"workspace_checkout", "POST", "/workspaces/" + gWork + "/checkout", map[string]any{"workspace_id": gWork, "expected_version": 4, "entity_ids": []string{gMedium}}, map[string]any{"expected_version": 4, "entity_ids": []string{gMedium}}, ""},
		{"workspace_members", "PUT", "/workspaces/" + gWork + "/members", map[string]any{"workspace_id": gWork, "expected_version": 4, "user_ids": []string{gOther}}, map[string]any{"expected_version": 4, "user_ids": []string{gOther}}, ""},
		{"workspace_commit", "POST", "/workspaces/" + gWork + "/commits", map[string]any{"workspace_id": gWork, "id": gTrack, "expected_version": 4, "message": "一同提交\nKeep original author"}, map[string]any{"id": gTrack, "expected_version": 4, "message": "一同提交\nKeep original author"}, ""},
		{"workspace_commits", "GET", "/workspaces/" + gWork + "/commits", map[string]any{"workspace_id": gWork}, nil, ""},
		{"workspace_commits", "GET", "/workspaces/" + gWork + "/commits", map[string]any{"workspace_id": gWork, "limit": 10, "offset": 20}, nil, "limit=10&offset=20"},
		{"workspace_rebase", "POST", "/workspaces/" + gWork + "/rebase", map[string]any{"workspace_id": gWork, "id": gTrack, "expected_version": 4, "expected_head": gOther, "onto_commit_id": gRelease, "order": []string{}}, map[string]any{"id": gTrack, "expected_version": 4, "expected_head": gOther, "onto_commit_id": gRelease, "order": []string{}}, ""},
		{"workspace_version", "POST", "/workspaces/" + gWork + "/version", map[string]any{"workspace_id": gWork, "id": gTrack, "expected_version": 4, "expected_head": gOther, "operation": "merge", "commit_ids": []string{gRelease}, "resolutions": []any{map[string]any{"commit_id": gRelease, "target": "entity", "id": gWork, "pointer": "/title", "current_version": 1, "value": "Merged"}}}, map[string]any{"id": gTrack, "expected_version": 4, "expected_head": gOther, "operation": "merge", "commit_ids": []string{gRelease}, "resolutions": []any{map[string]any{"commit_id": gRelease, "target": "entity", "id": gWork, "pointer": "/title", "current_version": 1, "value": "Merged"}}}, ""},
		{"workspace_push", "POST", "/workspaces/" + gWork + "/push", map[string]any{"workspace_id": gWork, "expected_version": 4, "expected_head": gTrack, "title": "提案"}, map[string]any{"expected_version": 4, "expected_head": gTrack, "title": "提案"}, ""},
		{"pull_request_create", "POST", "/pull-requests", map[string]any{"workspace_id": gWork, "expected_version": 4, "expected_head": gTrack}, map[string]any{"workspace_id": gWork, "expected_version": 4, "expected_head": gTrack}, ""},
		{"pull_request_list", "GET", "/pull-requests", map[string]any{"workspace_id": gWork, "status": "open", "limit": 20, "offset": 0}, nil, "limit=20&offset=0&status=open&workspace_id=" + gWork},
		{"pull_request_get", "GET", "/pull-requests/" + gRelease, map[string]any{"pull_request_id": gRelease}, nil, ""},
		{"pull_request_edit", "PATCH", "/pull-requests/" + gRelease, map[string]any{"pull_request_id": gRelease, "expected_version": 2, "status": "closed", "title": "Close"}, map[string]any{"expected_version": 2, "status": "closed", "title": "Close"}, ""},
		{"pull_request_edit", "PATCH", "/pull-requests/" + gRelease, map[string]any{"pull_request_id": gRelease, "expected_version": 3, "status": "open"}, map[string]any{"expected_version": 3, "status": "open"}, ""},
		{"pull_request_merge", "POST", "/pull-requests/" + gRelease + "/merge", map[string]any{"pull_request_id": gRelease, "expected_version": 2, "expected_head": gTrack}, map[string]any{"expected_version": 2, "expected_head": gTrack}, ""},
		{"commit_get", "GET", "/commits/" + gTrack, map[string]any{"commit_id": gTrack}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != tc.method || r.URL.Path != "/api/catalog"+tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("wrong upstream request %s %s", r.Method, r.URL.String())
				}
				if r.Header.Get("Authorization") != "Bearer mfp_collaborator" || r.Header.Get("Cookie") != "" {
					t.Error("delegated identity changed or cookie forwarded")
				}
				b, _ := io.ReadAll(r.Body)
				if tc.body == nil {
					if len(b) != 0 {
						t.Errorf("read body should be empty: %s", b)
					}
				} else {
					var got any
					if e := json.Unmarshal(b, &got); e != nil || !reflect.DeepEqual(got, roundTrip(tc.body)) {
						t.Errorf("body changed: got %s want %s", b, mustJSON(tc.body))
					}
				}
				writeJSON(w, 202, map[string]any{"applied": false, "source_head_id": gTrack})
			}))
			defer up.Close()
			s := New(testConfig(up.URL), nil)
			ctx := principalContext(Principal{PAT: "mfp_collaborator", Grant: Grant{UserID: "collaborator", Scopes: []string{"catalog.entity.edit"}}})
			r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_" + tc.name, Arguments: tc.args})
			if e != nil {
				t.Fatal(e)
			}
			m := structured(t, r)
			data, _ := m["data"].(map[string]any)
			if r.IsError || m["ok"] != true || m["status"] != float64(202) || data["applied"] != false || data["source_head_id"] != gTrack || calls.Load() != 1 {
				t.Fatal("native receipt changed or request replayed", m, calls.Load())
			}
		})
	}
}

func TestWorkspaceStagePreservesOperationsAndStableRefsWithoutEvidence(t *testing.T) {
	operations := []any{
		map[string]any{"target": "entity", "action": "create", "ref": "new_work", "document": map[string]any{"kind": "work", "title": "作品", "attributes": map[string]any{"visible": false, "optional": nil, "parts": []any{1, "二"}}}},
		map[string]any{"target": "entity", "action": "create", "ref": "release", "document": map[string]any{"kind": "release", "title": "Original [CD]", "subjects": []any{map[string]any{"work_id": map[string]any{"$ref": "new_work"}, "role": "main", "position": 1}}}},
		map[string]any{"target": "entity", "action": "update", "id": "@local:new_work", "document": map[string]any{"kind": "work", "title": "作品 revised", "state": "active", "review_status": "reviewed", "medium_id": nil}},
		map[string]any{"target": "entity", "action": "update", "id": gMedium, "patch": []any{map[string]any{"path": "/attributes/nullable", "value": nil}, map[string]any{"path": "/attributes/old", "remove": true}}},
	}
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "PUT" || r.URL.Path != "/api/catalog/workspaces/"+gWork+"/changes" {
			t.Errorf("unexpected endpoint %s %s", r.Method, r.URL.Path)
		}
		var body any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil || !reflect.DeepEqual(body, roundTrip(map[string]any{"expected_version": 7, "operations": operations})) {
			t.Error("staged JSON modified", body)
		}
		writeJSON(w, 200, map[string]any{"workspace": map[string]any{"id": gWork, "version": 8, "dirty_count": 3}, "refs": map[string]any{"new_work": gRelease, "release": gTrack}})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_edit", Grant: Grant{UserID: "u", Scopes: []string{"catalog.entity.edit"}}})
	r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_workspace_stage", Arguments: map[string]any{"workspace_id": gWork, "expected_version": 7, "operations": operations}})
	if e != nil {
		t.Fatal(e)
	}
	m := structured(t, r)
	data, _ := m["data"].(map[string]any)
	refs, _ := data["refs"].(map[string]any)
	if r.IsError || m["ok"] != true || refs["new_work"] != gRelease || refs["release"] != gTrack || calls.Load() != 1 {
		t.Fatal("stable refs lost, evidence required, or stage replayed", m, calls.Load())
	}
}

func TestWorkspaceIsolationIsCheckedByUpstreamOnEveryRead(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/catalog/workspaces/"+gWork || r.Method != "GET" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer mfp_owner" {
			writeJSON(w, 200, map[string]any{"id": gWork, "name": "private fork", "head_commit_id": gTrack})
			return
		}
		writeJSON(w, 404, map[string]any{"error": "workspace_not_found"})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	for _, user := range []string{"owner", "other"} {
		ctx := principalContext(Principal{PAT: "mfp_" + user, Grant: Grant{UserID: user, Scopes: []string{"mcp.read"}}})
		r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_workspace_get", Arguments: map[string]any{"workspace_id": gWork}})
		if e != nil {
			t.Fatal(e)
		}
		m := structured(t, r)
		if user == "owner" && m["ok"] != true {
			t.Fatal("owner read failed", m)
		}
		if user == "other" && (r.IsError || m["ok"] != false || m["error"] != "workspace_not_found" || strings.Contains(mustJSON(m), "private fork") || strings.Contains(mustJSON(m), gTrack)) {
			t.Fatal("other user obtained owner's cached workspace", m)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("workspace access was not checked per identity", calls.Load())
	}
}

func TestWorkspaceWritesRequireGrantAndPreserveUpstreamCAS(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["expected_version"] != float64(4) || body["expected_head"] != gTrack {
			t.Error("CAS was removed or changed", body)
		}
		writeJSON(w, 409, map[string]any{"error": "workspace_version_conflict", "current_version": 5, "applied": false})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	for _, allowed := range []bool{false, true} {
		scopes := []string{"mcp.read"}
		if allowed {
			scopes = []string{"catalog.entity.edit"}
		}
		ctx := principalContext(Principal{PAT: "mfp_edit", Grant: Grant{UserID: "u", Scopes: scopes}})
		r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_workspace_push", Arguments: map[string]any{"workspace_id": gWork, "expected_version": 4, "expected_head": gTrack}})
		if e != nil {
			t.Fatal(e)
		}
		m := structured(t, r)
		if !allowed && (m["error"] != "insufficient_scope" || calls.Load() != 0) {
			t.Fatal("read grant performed workspace write", m, calls.Load())
		}
		if allowed && (r.IsError || m["ok"] != false || m["error"] != "workspace_version_conflict" || m["details"].(map[string]any)["current_version"] != float64(5) || calls.Load() != 1) {
			t.Fatal("CAS denial lost or replayed", m, calls.Load())
		}
	}
}

func TestCollaborationPermissionDenialsRemainUpstreamAuthoritative(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"workspace_members", map[string]any{"workspace_id": gWork, "expected_version": 4, "user_ids": []string{gOther}}},
		{"pull_request_merge", map[string]any{"pull_request_id": gRelease, "expected_version": 2, "expected_head": gTrack}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer mfp_editor" {
					t.Error("upstream received a different identity")
				}
				writeJSON(w, 403, map[string]any{"error": "forbidden", "message": "not permitted for this user", "applied": false})
			}))
			defer up.Close()
			s := New(testConfig(up.URL), nil)
			ctx := principalContext(Principal{PAT: "mfp_editor", Grant: Grant{UserID: "editor", Scopes: []string{"catalog.entity.edit"}}})
			r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_" + tc.tool, Arguments: tc.args})
			if e != nil {
				t.Fatal(e)
			}
			m := structured(t, r)
			if r.IsError || m["ok"] != false || m["error"] != "forbidden" || m["status"] != float64(403) || calls.Load() != 1 {
				t.Fatal("upstream permission denial changed or replayed", m, calls.Load())
			}
		})
	}
}

func TestWorkspaceRebaseForwardsExplicitResolutionUnchanged(t *testing.T) {
	body := map[string]any{"id": gOther, "expected_version": 4, "expected_head": gTrack, "message": "Resolve title", "resolutions": []any{
		map[string]any{"target": "entity", "id": gMedium, "pointer": "/title", "current_version": 8, "value": "resolved"},
		map[string]any{"target": "relation", "id": gRelease, "pointer": "/attributes/credit", "current_version": 2, "remove": true},
		map[string]any{"target": "entity", "id": gWork, "pointer": "", "current_version": 0, "remove": true},
	}}
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/catalog/workspaces/"+gWork+"/rebase" {
			t.Errorf("wrong rebase route %s %s", r.Method, r.URL.Path)
		}
		var got any
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil || !reflect.DeepEqual(got, roundTrip(body)) {
			t.Error("explicit resolution changed", got)
		}
		writeJSON(w, 200, map[string]any{"commit": map[string]any{"id": gOther, "author_id": gWork, "status": "committed"}, "workspace": map[string]any{"version": 5, "head_commit_id": gOther}})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	ctx := principalContext(Principal{PAT: "mfp_edit"})
	args := map[string]any{"workspace_id": gWork}
	for key, value := range body {
		args[key] = value
	}
	r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_workspace_rebase", Arguments: args})
	if e != nil || failedResult(r) || calls.Load() != 1 {
		t.Fatal("rebase forwarding failed or replayed", e, r, calls.Load())
	}
}

func TestCollaborationUnknownWritesNeverReplay(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"workspace_stage", map[string]any{"workspace_id": gWork, "expected_version": 4, "operations": []any{map[string]any{"target": "relation", "action": "delete", "id": gOther}}}},
		{"workspace_commit", map[string]any{"workspace_id": gWork, "id": gOther, "expected_version": 4, "message": "Keep this id"}},
		{"workspace_push", map[string]any{"workspace_id": gWork, "expected_version": 4, "expected_head": gTrack}},
		{"workspace_rebase", map[string]any{"workspace_id": gWork, "id": gOther, "expected_version": 4, "expected_head": gTrack}},
		{"pull_request_merge", map[string]any{"pull_request_id": gRelease, "expected_version": 2, "expected_head": gTrack}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			s := New(testConfig("http://localhost"), nil)
			transport := &failingTransport{}
			s.HTTP.Transport = transport
			ctx := principalContext(Principal{PAT: "mfp_edit"})
			r, e := connect(t, s, ctx).CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_" + tc.tool, Arguments: tc.args})
			if e != nil {
				t.Fatal(e)
			}
			m := structured(t, r)
			if !r.IsError || m["error"] != "upstream_unavailable" || m["outcome"] != "unknown" || m["retry_automatically"] != false || transport.calls.Load() != 1 {
				t.Fatal("uncertain write replayed or treated as unapplied", m, transport.calls.Load())
			}
		})
	}
}

func TestCollaborationToolValidationDoesNotReachUpstream(t *testing.T) {
	s := New(testConfig("http://localhost"), nil)
	transport := &failingTransport{}
	s.HTTP.Transport = transport
	ctx := principalContext(Principal{PAT: "mfp_edit"})
	cs := connect(t, s, ctx)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"workspace_push", map[string]any{"workspace_id": gWork, "expected_version": 4}},
		{"workspace_commit", map[string]any{"workspace_id": gWork, "id": gOther, "expected_version": 4, "message": "  "}},
		{"workspace_stage", map[string]any{"workspace_id": gWork, "expected_version": 0, "operations": []any{}}},
		{"workspace_stage", map[string]any{"workspace_id": gWork, "expected_version": 4, "operations": []any{map[string]any{"target": "entity", "action": "create", "id": gOther, "document": map[string]any{"kind": "work", "title": "No supplied id"}}}}},
		{"workspace_stage", map[string]any{"workspace_id": gWork, "expected_version": 4, "operations": []any{map[string]any{"target": "entity", "action": "update", "id": gOther, "document": map[string]any{"kind": "work"}, "patch": []any{map[string]any{"path": "/title", "value": "ambiguous"}}}}}},
		{"workspace_stage", map[string]any{"workspace_id": gWork, "expected_version": 4, "operations": []any{map[string]any{"target": "entity", "action": "update", "id": gOther, "document": "not an object"}}}},
		{"workspace_get", map[string]any{"workspace_id": "../another-user"}},
		{"pull_request_merge", map[string]any{"pull_request_id": gRelease, "expected_version": 2}},
	} {
		r, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "metafusion_" + tc.tool, Arguments: tc.args})
		if e != nil {
			t.Fatal(e)
		}
		if m := structured(t, r); r.IsError || m["error"] != "invalid_tool_arguments" {
			t.Fatal("malformed tool request accepted", tc.tool, m)
		}
	}
	if transport.calls.Load() != 0 {
		t.Fatal("malformed arguments reached upstream", transport.calls.Load())
	}
}
