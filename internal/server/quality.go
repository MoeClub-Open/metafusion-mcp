package server

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// catalog_quality_audit wraps the backend's on-demand quality audit
// (GET /api/catalog/entities/{id}/quality, POST /api/catalog/quality/batch).
// It is read-only: the audit only reports, it never modifies catalog data.
func qualitySchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"ids"}, "additionalProperties": false, "properties": map[string]any{
		"ids": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "string", "format": "uuid"}, "description": "1-100 entity UUIDs"},
	}}
}

func (s *Server) qualityAudit(ctx context.Context, ids []string) *mcp.CallToolResult {
	if len(ids) == 1 {
		return s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/entities/" + ids[0] + "/quality"})
	}
	return s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/quality/batch", Body: map[string]any{"ids": ids}})
}

func (s *Server) addQualityTool() {
	schema := withDefs(qualitySchema())
	s.MCP.AddTool(&mcp.Tool{Name: "catalog_quality_audit", Description: "Run the server's on-demand quality audit for 1-100 entity UUIDs (computed live, never stored, advisory only, never modifies data). One id returns a single QualityReport in data; several return data.reports plus data.errors {id: invalid_id|not_found}. status: complete = automated checks found no issue (UI 无缺项; NOT human-verified), partial = structural gaps, needs_verification = data problems such as dangling references. checks lists which checks ran; required relationships are not checked yet. Fix issues by building data from raw evidence through the normal commit workflow.", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, bad := validateArgs(schema, r.Params.Arguments); bad != nil {
			return bad, nil
		}
		var in struct {
			IDs []string `json:"ids"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &in)
		return s.qualityAudit(ctx, in.IDs), nil
	})
}
