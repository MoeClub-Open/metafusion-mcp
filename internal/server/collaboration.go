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
		{"workspace_create", "POST", "/api/catalog/workspaces", "", "Create a server workspace (a private editable copy). Optional entity_ids and relation_ids checkout records immediately. Main catalog records stay unchanged until merge.", obj(map[string]any{"name": str(), "entity_ids": ids(), "relation_ids": ids()})},
		{"workspace_list", "GET", "/api/catalog/workspaces", "", "List workspaces accessible to the current account. Private copies remain subject to upstream ownership and collaborator permissions.", obj(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "offset": map[string]any{"type": "integer", "minimum": 0}})},
		{"workspace_get", "GET", "/api/catalog/workspaces/{id}", "workspace_id", "Read a workspace, its objects, dirty count, version and head_commit_id. Use this to recover an uncertain stage, push or rebase result before deciding whether to retry.", workspace(map[string]any{})},
		{"workspace_edit", "PATCH", "/api/catalog/workspaces/{id}", "workspace_id", "Rename or archive a workspace with its expected version. Native ownership and collaboration permissions apply.", workspace(map[string]any{"expected_version": version(), "name": str(), "state": map[string]any{"type": "string", "enum": []string{"active", "archived"}}}, "expected_version")},
		{"workspace_checkout", "POST", "/api/catalog/workspaces/{id}/checkout", "workspace_id", "Checkout main entity/relation snapshots into this server copy using expected_version. This updates only the workspace, never the main catalog.", workspace(map[string]any{"expected_version": version(), "entity_ids": ids(), "relation_ids": ids()}, "expected_version")},
		{"workspace_stage", "PUT", "/api/catalog/workspaces/{id}/changes", "workspace_id", "Save 1-100 operations to the workspace working copy. Multiple calls and objects can be combined into one commit. No source, source_refs or candidate-review prerequisite. Create returns stable UUIDs in refs; use those UUIDs in later calls. CAS expected_version prevents overwriting concurrent collaborator edits.", workspaceStageSchema()},
		{"workspace_members", "PUT", "/api/catalog/workspaces/{id}/members", "workspace_id", "Set a workspace's collaborating user UUIDs with expected_version. The owner remains responsible; upstream checks who may edit membership.", workspace(map[string]any{"expected_version": version(), "user_ids": ids()}, "expected_version", "user_ids")},
		{"workspace_commit", "POST", "/api/catalog/workspaces/{id}/commits", "workspace_id", "Commit all current workspace changes with a message and client-generated UUID id. Immutable authorship is recorded; main remains unchanged. Multiple commits may be pushed together. Keep the id and recover through metafusion_commit_get after an unknown outcome.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "message": message()}, "id", "expected_version", "message")},
		{"workspace_commits", "GET", "/api/catalog/workspaces/{id}/commits", "workspace_id", "Read the workspace's immutable commit history, original authors, parent chain and changes. Private history uses the same workspace access checks.", workspace(map[string]any{})},
		{"workspace_push", "POST", "/api/catalog/workspaces/{id}/push", "workspace_id", "Push the committed workspace using expected_version and expected_head. Bypass users merge atomically; other users create/update a pull request (202, applied=false). Real conflicts return base/current/proposed for rebase. Never automatically replay an unknown push: read workspace, commit and PR status first.", workspace(pushProps(), "expected_version", "expected_head")},
		{"workspace_rebase", "POST", "/api/catalog/workspaces/{id}/rebase", "workspace_id", "Rebase committed workspace changes onto current main using expected_version and expected_head. Supply explicit resolutions for returned conflicts, including current_version and either value or remove:true. Successful resolution creates immutable commit id (client UUID), retaining original contribution authors. Read that commit after an unknown result.", workspace(map[string]any{"id": uuidSchema(), "expected_version": version(), "expected_head": uuidSchema(), "message": str(), "resolutions": arr(obj(map[string]any{"target": map[string]any{"type": "string", "enum": []string{"entity", "relation"}}, "id": uuidSchema(), "pointer": str(), "current_version": map[string]any{"type": "integer", "minimum": 0}, "value": map[string]any{}, "remove": map[string]any{"type": "boolean"}}, "target", "id", "pointer", "current_version"))}, "id", "expected_version", "expected_head")},
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
