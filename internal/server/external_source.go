package server

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// external_source_query wraps POST /api/external-sources/query (read-only).
// External sources are queried only; the server never auto-imports, merges or
// modifies catalog entities from upstream data.
func externalSourceQuerySchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"required":             []string{"op"},
		"additionalProperties": false,
		"properties": map[string]any{
			"source":      map[string]any{"type": "string", "description": "Adapter id: bangumi|dlsite|dmm; empty/auto allowed"},
			"op":          map[string]any{"type": "string", "enum": []string{"search", "lookup", "detail"}},
			"q":           map[string]any{"type": "string", "description": "Search keyword (required for op=search)"},
			"id":          map[string]any{"type": "string", "description": "Exact source id (lookup/detail)"},
			"url_or_id":   map[string]any{"type": "string", "description": "URL or id (lookup/detail; alternative to id)"},
			"entity_type": map[string]any{"type": "string", "enum": []string{"work", "artist", "organization", "character"}},
			"limit":       map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "description": "Search page size (default 20)"},
		},
	}
}

func (s *Server) externalSourceQuery(ctx context.Context, body map[string]any) *mcp.CallToolResult {
	return s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/external-sources/query", Body: body})
}

func (s *Server) addExternalSourceTool() {
	schema := withDefs(externalSourceQuerySchema())
	s.MCP.AddTool(&mcp.Tool{
		Name:        "external_source_query",
		Description: "Read-only external source query via POST /api/external-sources/query. op=search|lookup|detail. Returns upstream raw data plus FetchMeta {url, fetched_at, content_hash}; lookup/detail also return the existing preview mapping. Never auto-imports, merges or writes the catalog — use normal commit workflow with Agent-chosen evidence. DLsite/DMM search returns search_not_supported. Requires a signed-in grant (mcp.read is enough for the tool call; catalog auth still applies upstream).",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, bad := validateArgs(schema, r.Params.Arguments); bad != nil {
			return bad, nil
		}
		var in map[string]any
		_ = json.Unmarshal(r.Params.Arguments, &in)
		return s.externalSourceQuery(ctx, in), nil
	})
}
