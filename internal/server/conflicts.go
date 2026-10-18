package server

import "github.com/modelcontextprotocol/go-sdk/mcp"

// withWorkspaceConflicts lifts field-level conflicts from a 409 workspace_conflict
// upstream body to the top level of the ok:false tool result, so agents see
// pointer/base/current/proposed without digging into details.
// Successful results and other errors are returned untouched.
func withWorkspaceConflicts(r *mcp.CallToolResult) *mcp.CallToolResult {
	m, ok := r.StructuredContent.(map[string]any)
	if !ok || m["ok"] != false {
		return r
	}
	if m["error"] != "workspace_conflict" {
		return r
	}
	conflicts := []any{}
	if details, ok := m["details"].(map[string]any); ok {
		if c, ok := details["conflicts"].([]any); ok {
			conflicts = c
		} else if conflict, ok := details["conflict"].(map[string]any); ok {
			if c, ok := conflict["conflicts"].([]any); ok {
				conflicts = c
			}
		}
	}
	if len(conflicts) == 0 {
		if data, ok := m["data"].(map[string]any); ok {
			if c, ok := data["conflicts"].([]any); ok {
				conflicts = c
			}
		}
	}
	m["conflicts"] = conflicts
	m["conflicts_note"] = "Three-way workspace conflict: inspect target, id, pointer, base, current, proposed and current_version. Submit explicit resolutions to workspace_rebase with expected workspace version/head and a new client-generated commit id; main remains unchanged until merge."
	return result(m, r.IsError)
}
