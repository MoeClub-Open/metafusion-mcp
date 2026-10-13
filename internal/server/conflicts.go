package server

import "github.com/modelcontextprotocol/go-sdk/mcp"

// withCommitConflicts lifts field-level conflicts from a 409 commit_conflict
// upstream body to the top level of the ok:false tool result, so agents see
// pointer/base/current/proposed/suggestions without digging into details.
// Successful results and other errors are returned untouched.
func withCommitConflicts(r *mcp.CallToolResult) *mcp.CallToolResult {
	m, ok := r.StructuredContent.(map[string]any)
	if !ok || m["ok"] != false {
		return r
	}
	if m["error"] != "commit_conflict" {
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
	m["conflicts_note"] = "field-level three-way merge diagnostics (pointer, base, current, proposed, kind, suggestions); suggestions are advisory only (manual_merge|take_current|rebase_keep_mine). Do not replay the same sealed commit id — prepare a rebased commit."
	return result(m, r.IsError)
}
