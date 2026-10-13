package server

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// catalog_identity_check wraps POST /api/catalog/entities/candidates/batch only.
// It is read-only: ranking context never filters candidates, and recommended_action
// is advisory (reuse|review|distinct). The server never auto-merges.
func identityCheckSchema() map[string]any {
	val := map[string]any{"type": "string", "maxLength": 256}
	uuid := map[string]any{"type": "string", "format": "uuid"}
	contextSchema := obj(map[string]any{
		"creator":        arr(uuid, "maxItems", 20, "description", "Creator agent UUIDs; ranks only, never filters"),
		"performer":      arr(uuid, "maxItems", 20, "description", "Performer agent UUIDs; ranks only, never filters"),
		"label":          arr(uuid, "maxItems", 20, "description", "Label/publisher agent UUIDs; ranks only, never filters"),
		"catalog_number": arr(val, "maxItems", 20, "description", "Catalog numbers; ranks only, never filters"),
	})
	query := obj(map[string]any{
		"key":          map[string]any{"type": "string", "minLength": 1, "maxLength": 64, "description": "Client correlation key; unique within the batch"},
		"kind":         map[string]any{"type": "string", "enum": entityKinds},
		"titles":       arr(val, "maxItems", 20),
		"external_ids": arr(obj(map[string]any{"provider": map[string]any{"type": "string", "minLength": 1}, "value": val}, "provider", "value"), "maxItems", 20),
		"attributes":   arr(obj(map[string]any{"key": map[string]any{"type": "string", "minLength": 1}, "value": val}, "key", "value"), "maxItems", 20),
		"work_id":      uuid,
		"release_id":   uuid,
		"medium_id":    uuid,
		"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"context":      contextSchema,
	}, "key", "kind")
	return map[string]any{"type": "object", "required": []string{"queries"}, "additionalProperties": false, "properties": map[string]any{
		"queries": arr(query, "minItems", 1, "maxItems", 50, "description", "1-50 identity queries; each needs kind plus at least one of titles/external_ids/attributes"),
	}}
}

func (s *Server) identityCheck(ctx context.Context, queries []map[string]any) *mcp.CallToolResult {
	return s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/entities/candidates/batch", Body: map[string]any{"queries": queries}})
}

func (s *Server) addIdentityTool() {
	schema := withDefs(identityCheckSchema())
	s.MCP.AddTool(&mcp.Tool{Name: "catalog_identity_check", Description: "Batch identity candidate check (1-50 queries) via POST /api/catalog/entities/candidates/batch. Optional context {creator,performer,label,catalog_number} only adjusts ranking/confidence/recommended_action — it NEVER filters candidates (same-name different-artist entities remain). Each item carries assessment {matched_fields, conflicts?, context_summary?, confidence, recommended_action: reuse|review|distinct}; advisory only — the server never auto-merges. Per-query validation failures appear as results[].error. Read-only; build data from evidence through the normal commit workflow.", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, bad := validateArgs(schema, r.Params.Arguments); bad != nil {
			return bad, nil
		}
		var in struct {
			Queries []map[string]any `json:"queries"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &in)
		return s.identityCheck(ctx, in.Queries), nil
	})
}
