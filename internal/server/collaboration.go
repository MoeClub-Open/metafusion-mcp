package server

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Collaboration tools use the catalog's persistent workspaces directly. The
// MCP service neither stores another copy nor runs a second commit workflow.
func (s *Server) addCollaborationTools() {
	version := func() map[string]any { return map[string]any{"type": "integer", "minimum": 1} }
	message := func() map[string]any { return map[string]any{"type": "string", "minLength": 1, "pattern": `\S`} }
	ids := func() map[string]any { return arr(uuidSchema()) }
	page := func() map[string]any {
		return map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "offset": map[string]any{"type": "integer", "minimum": 0}}
	}
	records := func() map[string]any {
		p := page()
		p["target"] = map[string]any{"type": "string", "enum": []string{"entity", "relation"}}
		p["record_id"] = uuidSchema()
		return p
	}
	history := func(branch bool) map[string]any {
		p := records()
		p["author_id"] = uuidSchema()
		p["since"] = map[string]any{"type": "string", "format": "date-time"}
		p["until"] = map[string]any{"type": "string", "format": "date-time"}
		if branch {
			p["state"] = map[string]any{"type": "string", "enum": []string{"active", "pending", "integrated", "replaced", "all"}}
		}
		return p
	}
	commit := func(p map[string]any, required ...string) map[string]any {
		p["commit_id"] = uuidSchema()
		return obj(p, append([]string{"commit_id"}, required...)...)
	}
	resolutions := func() map[string]any {
		return arr(obj(map[string]any{"commit_id": uuidSchema(), "target": map[string]any{"type": "string", "enum": []string{"entity", "relation"}}, "id": uuidSchema(), "pointer": str(), "current_version": map[string]any{"type": "integer", "minimum": 0}, "value": map[string]any{}, "remove": map[string]any{"type": "boolean"}}, "target", "id", "pointer", "current_version"))
	}
	workspace := func(props map[string]any, required ...string) map[string]any {
		props["workspace_id"] = uuidSchema()
		return obj(props, append([]string{"workspace_id"}, required...)...)
	}
	pr := func(props map[string]any, required ...string) map[string]any {
		props["pull_request_id"] = uuidSchema()
		return obj(props, append([]string{"pull_request_id"}, required...)...)
	}
	pushProps := func() map[string]any {
		return map[string]any{"expected_version": version(), "expected_head": uuidSchema(), "title": str()}
	}
	type operation struct {
		name, method, path, pathID, description string
		schema                                  map[string]any
	}
	operations := []operation{
		{"workspace_create", "POST", "/api/catalog/workspaces", "", "Create a server workspace. Optional client-selected id provides durable creation identity; retain it across unknown outcomes. Main facts stay unchanged until merge.", obj(map[string]any{"id": uuidSchema(), "name": str(), "entity_ids": ids(), "relation_ids": ids()})},
		{"workspace_list", "GET", "/api/catalog/workspaces", "", "List workspaces accessible to the current account. Private copies remain subject to upstream ownership and collaborator permissions.", obj(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "offset": map[string]any{"type": "integer", "minimum": 0}})},
		{"workspace_get", "GET", "/api/catalog/workspaces/{id}", "workspace_id", "Read a workspace, its objects, dirty count, version and head_commit_id. Use this to recover an uncertain stage, push or rebase result before deciding whether to retry.", workspace(map[string]any{})},
		{"workspace_edit", "PATCH", "/api/catalog/workspaces/{id}", "workspace_id", "Rename or archive a workspace with its expected version. Native ownership and collaboration permissions apply.", workspace(map[string]any{"expected_version": version(), "name": str(), "state": map[string]any{"type": "string", "enum": []string{"active", "archived"}}}, "expected_version")},
		{"workspace_checkout", "POST", "/api/catalog/workspaces/{id}/checkout", "workspace_id", "Checkout main entity/relation snapshots into this server copy using expected_version. This updates only the workspace, never the main catalog.", workspace(map[string]any{"expected_version": version(), "entity_ids": ids(), "relation_ids": ids()}, "expected_version")},
		{"workspace_stage", "PUT", "/api/catalog/workspaces/{id}/changes", "workspace_id", "Save 1-100 operations to the workspace working copy. Multiple calls and objects can be combined into one commit. No source, source_refs or candidate-review prerequisite. Create returns stable UUIDs in refs; use those UUIDs in later calls. CAS expected_version prevents overwriting concurrent collaborator edits.", workspaceStageSchema()},
		{"workspace_members", "PUT", "/api/catalog/workspaces/{id}/members", "workspace_id", "Set a workspace's collaborating user UUIDs with expected_version. The owner remains responsible; upstream checks who may edit membership.", workspace(map[string]any{"expected_version": version(), "user_ids": ids()}, "expected_version", "user_ids")},
		{"workspace_commit", "POST", "/api/catalog/workspaces/{id}/commits", "workspace_id", "Commit all current workspace changes with a message and client-generated UUID id. Immutable authorship is recorded; main remains unchanged. Multiple commits may be pushed together. Keep the id and recover through metafusion_commit_get after an unknown outcome.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "message": message()}, "id", "expected_version", "message")},
		{"workspace_commits", "GET", "/api/catalog/workspaces/{id}/commits", "workspace_id", "Read branch commits with record, author, RFC3339 creation-time and membership-state filters before pagination. state defaults to active; pending/integrated/replaced/all select retained branch memberships.", workspace(history(true))},
		{"workspace_fork", "POST", "/api/catalog/workspaces/fork", "", "Derive an independent private copy from an accessible frozen commit. For an official catalog checkpoint, optional entity_ids/relation_ids select the record set; omission selects that checkpoint's changed records. Keep id for retries.", obj(map[string]any{"id": uuidSchema(), "name": str(), "commit_id": uuidSchema(), "entity_ids": ids(), "relation_ids": ids()}, "id", "name", "commit_id")},
		{"workspace_version", "POST", "/api/catalog/workspaces/{id}/version", "workspace_id", "Apply a merge, cherry-pick, revert, snapshot restore, amend, suffix squash, reset or discard in the copy only. Rewrite retains originals and requires owner permission. Sources are commit IDs, never live unsaved state. Unknown writes must not be replayed automatically.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "expected_head": str(), "operation": map[string]any{"type": "string", "enum": []string{"merge", "cherry_pick", "revert", "restore", "amend", "squash", "reset", "discard"}}, "commit_ids": ids(), "message": str(), "mode": map[string]any{"type": "string", "enum": []string{"soft", "hard"}}, "resolutions": resolutions()}, "id", "expected_version", "expected_head", "operation")},
		{"workspace_status", "GET", "/api/catalog/workspaces/{id}/status", "workspace_id", "Read dirty and pending counts plus committed merge conflicts without changing any data.", workspace(map[string]any{})},
		{"workspace_events", "GET", "/api/catalog/workspaces/{id}/events", "workspace_id", "Read private immutable branch events, including original heads after history rewriting. Follow pagination.", workspace(page())},
		{"workspace_tags", "GET", "/api/catalog/workspaces/{id}/tags", "workspace_id", "List private branch tags with pagination.", workspace(page())},
		{"workspace_tag", "POST", "/api/catalog/workspaces/{id}/tags", "workspace_id", "Owner-only create, rename or move a tag; tag_id selects an existing tag, delete=true removes it. Keep request id and version/head guards.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "expected_head": str(), "tag_id": uuidSchema(), "delete": map[string]any{"type": "boolean"}, "name": str(), "commit_id": uuidSchema(), "message": str()}, "id", "expected_version", "expected_head")},
		{"workspace_stashes", "GET", "/api/catalog/workspaces/{id}/stashes", "workspace_id", "List saved private working changes with pagination.", workspace(page())},
		{"workspace_stash", "POST", "/api/catalog/workspaces/{id}/stashes", "workspace_id", "Save working changes and clear the copy, apply or pop with three-way conflict handling, or drop a stash. Keep id; this never publishes records.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "expected_head": str(), "operation": map[string]any{"type": "string", "enum": []string{"save", "apply", "pop", "drop"}}, "stash_id": uuidSchema(), "name": str(), "resolutions": resolutions()}, "id", "expected_version", "expected_head", "operation")},
		{"workspace_diff", "GET", "/api/catalog/workspaces/{id}/diff", "workspace_id", "Read working changes relative to the committed head (default) or checkout base, without writing. Filter target/record_id before pagination. Keep returned version/head_id when reviewing concurrent edits.", func() map[string]any {
			p := records()
			p["basis"] = map[string]any{"type": "string", "enum": []string{"head", "base"}}
			return workspace(p)
		}()},
		{"commit_diff", "GET", "/api/catalog/commits/{id}/diff", "commit_id", "Read one immutable commit's original changes, with target/record_id filtering and pagination.", commit(records())},
		{"pull_request_diff", "GET", "/api/catalog/pull-requests/{id}/diff", "pull_request_id", "Read net changes frozen at the selected PR version/source_head_id, excluding later workspace edits. Filter target/record_id before pagination.", pr(records())},
		{"pull_request_commits", "GET", "/api/catalog/pull-requests/{id}/commits", "pull_request_id", "Read the frozen PR commit set, newest first. Record/author/RFC3339 creation-time filters run before pagination. This never follows a live workspace head.", pr(history(false))},
		{"record_revisions", "GET", "/api/catalog/entities/{id}/revisions", "record_id", "Read visible entity or relation revisions with exact version, original actor and RFC3339 creation-time filters before pagination. Deleted relation history requires active endpoints.", func() map[string]any {
			p := page()
			p["record_id"] = uuidSchema()
			p["version"] = version()
			p["author_id"] = uuidSchema()
			p["since"] = map[string]any{"type": "string", "format": "date-time"}
			p["until"] = map[string]any{"type": "string", "format": "date-time"}
			return obj(p, "record_id")
		}()},
		{"catalog_head", "GET", "/api/catalog/head", "", "Read the official transactional catalog head.", obj(map[string]any{})},
		{"catalog_history", "GET", "/api/catalog/history", "", "Read visible official integration checkpoints newest first. Filter by record, author and RFC3339 merge time before pagination.", obj(history(false))},
		{"commit_snapshot", "GET", "/api/catalog/commits/{id}/snapshot", "commit_id", "Read a paginated frozen snapshot. Scope catalog is the official catalog at that checkpoint; tracked_records is a complete tracked branch set; known_records is retained historical data only.", commit(records())},
		{"commit_compare", "GET", "/api/catalog/commits/{id}/compare", "commit_id", "Compare two immutable versions with paginated changes. base is the starting commit UUID.", func() map[string]any {
			p := records()
			p["commit_id"] = uuidSchema()
			p["base"] = uuidSchema()
			return obj(p, "commit_id", "base")
		}()},
		{"commit_blame", "GET", "/api/catalog/commits/{id}/blame", "commit_id", "Find the latest readable retained DAG edit change to a record field. This reports field attribution, not per-character authorship.", obj(map[string]any{"commit_id": uuidSchema(), "target": map[string]any{"type": "string", "enum": []string{"entity", "relation"}}, "record_id": uuidSchema(), "pointer": str()}, "commit_id", "target", "record_id", "pointer")},
		{"workspace_push", "POST", "/api/catalog/workspaces/{id}/push", "workspace_id", "Push the committed workspace using expected_version and expected_head. Bypass users merge atomically; other users create/update a pull request (202, applied=false). Real conflicts return base/current/proposed for rebase. Never automatically replay an unknown push: read workspace, commit and PR status first.", workspace(pushProps(), "expected_version", "expected_head")},
		{"workspace_rebase", "POST", "/api/catalog/workspaces/{id}/rebase", "workspace_id", "Replay pending edits onto onto_commit_id or the current catalog head. Optional order selects and reorders pending edits; [] drops all. Retain earlier resolutions with commit_id when a later replay step conflicts. Original commits and authors are retained with replacement links. The entire replay rolls back on conflict. Read the final commit id after an unknown result.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "expected_head": uuidSchema(), "message": str(), "onto_commit_id": uuidSchema(), "order": ids(), "resolutions": resolutions()}, "id", "expected_version", "expected_head")},
		{"pull_request_create", "POST", "/api/catalog/pull-requests", "", "Open or update a pull request for the workspace's expected version and head. Main records remain unchanged until an authorized merge.", workspace(pushProps(), "expected_version", "expected_head")},
		{"pull_request_list", "GET", "/api/catalog/pull-requests", "", "List pull requests visible to the current account with their merge status. Supports optional workspace_id, status, limit and offset filters.", obj(map[string]any{"workspace_id": uuidSchema(), "status": map[string]any{"type": "string", "enum": []string{"open", "closed", "merged"}}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "offset": map[string]any{"type": "integer", "minimum": 0}})},
		{"pull_request_get", "GET", "/api/catalog/pull-requests/{id}", "pull_request_id", "Read a pull request, its source head, original commits and proposed changes. Use its current version and source_head_id for an authorized merge.", pr(map[string]any{})},
		{"pull_request_edit", "PATCH", "/api/catalog/pull-requests/{id}", "pull_request_id", "Edit a pull request title, close or reopen it using expected_version. These operations never change main records.", pr(map[string]any{"expected_version": version(), "title": str(), "status": map[string]any{"type": "string", "enum": []string{"open", "closed"}}}, "expected_version")},
		{"pull_request_merge", "POST", "/api/catalog/pull-requests/{id}/merge", "pull_request_id", "Merge a pull request with expected_version and expected_head. The catalog checks review permission and reruns the shared three-way merge; original commit authors are preserved separately from the merger.", pr(map[string]any{"expected_version": version(), "expected_head": uuidSchema()}, "expected_version", "expected_head")},
		{"commit_get", "GET", "/api/catalog/commits/{id}", "commit_id", "Read an immutable collaboration commit, original author, changes, merge status and related pull request. Recover unknown commit/rebase results by the same client-generated id; do not create another id before checking.", obj(map[string]any{"commit_id": uuidSchema()}, "commit_id")},
	}
	for _, op := range operations {
		op := op
		s.MCP.AddTool(&mcp.Tool{Name: "metafusion_" + op.name, Description: op.description, InputSchema: op.schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: op.method == "GET"}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			body, bad := validateArgs(op.schema, req.Params.Arguments)
			if bad != nil {
				return bad, nil
			}
			apiPath := op.path
			if op.pathID != "" {
				apiPath = strings.Replace(apiPath, "{id}", body[op.pathID].(string), 1)
				delete(body, op.pathID)
			}
			in := APIInput{Method: op.method, Path: apiPath}
			if op.method == "GET" {
				in.Query = body
			} else {
				in.Body = body
			}
			return withWorkspaceConflicts(s.callAPI(ctx, in)), nil
		})
	}
}
