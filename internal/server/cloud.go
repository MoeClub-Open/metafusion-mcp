package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func newUUID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (s *Server) addCloudTools() {
	object := map[string]any{"type": "object", "additionalProperties": true}
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_commit_prepare", Description: "Seal a catalog commit in the cloud, generating its UUID and saving the exact immutable payload for 30 days. Requires edit_note, sources, operations and checkout definitions_etag. No local files, shell or runtime needed. Use prepare once; retain its ID for preview, push and recovery.", InputSchema: object}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, ok := currentPrincipal(ctx)
		if !ok {
			return toolError("authentication_required"), nil
		}
		if !s.limiter.AllowAPI(p, "POST", "/mcp/drafts") {
			return toolError("rate_limited"), nil
		}
		var body map[string]any
		if json.Unmarshal(r.Params.Arguments, &body) != nil {
			return toolError("invalid_arguments"), nil
		}
		if _, ok := body["id"]; ok {
			return toolError("id_is_generated_by_prepare"), nil
		}
		ops, ok := body["operations"].([]any)
		note, _ := body["edit_note"].(string)
		etag, _ := body["definitions_etag"].(string)
		sources, _ := body["sources"].([]any)
		if !ok || len(ops) == 0 || len(ops) > 100 || strings.TrimSpace(note) == "" || etag == "" || len(sources) == 0 {
			return toolError("invalid_commit"), nil
		}
		id := newUUID()
		body["id"] = id
		raw, _ := json.Marshal(body)
		if len(raw) > 2<<20 {
			return toolError("commit_too_large"), nil
		}
		// Saving a workspace is not publishing catalog data; upstream preview checks the complete contract.
		_, e := s.Store.DB.ExecContext(ctx, "INSERT INTO mcp.drafts(id,user_id,payload) VALUES($1,$2,$3)", id, p.Grant.UserID, string(raw))
		if e != nil {
			return toolError("draft_unavailable"), nil
		}
		return result(map[string]any{"id": id, "commit": body, "expires_in_days": 30, "applied": false}, false), nil
	})
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_commit", Description: "Use a saved cloud commit by ID. get returns the exact sealed payload; preview validates; push sends it once; recover queries the upstream receipt without replaying a write. Owner-scoped. Conflicts require a new rebased commit; unknown outcomes require recover first.", InputSchema: map[string]any{"type": "object", "required": []string{"id", "action"}, "properties": map[string]any{"id": map[string]string{"type": "string"}, "action": map[string]any{"type": "string", "enum": []string{"get", "preview", "push", "recover"}}}}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, ok := currentPrincipal(ctx)
		if !ok {
			return toolError("authentication_required"), nil
		}
		var in struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if json.Unmarshal(r.Params.Arguments, &in) != nil {
			return toolError("invalid_arguments"), nil
		}
		if !uuidPattern.MatchString(in.ID) {
			return toolError("invalid_id"), nil
		}
		var raw []byte
		e := s.Store.DB.QueryRowContext(ctx, "SELECT payload FROM mcp.drafts WHERE id=$1 AND user_id=$2 AND expires_at>now()", in.ID, p.Grant.UserID).Scan(&raw)
		if e == sql.ErrNoRows {
			return toolError("draft_not_found"), nil
		}
		if e != nil {
			return toolError("draft_unavailable"), nil
		}
		var body any
		_ = json.Unmarshal(raw, &body)
		switch in.Action {
		case "get":
			return result(map[string]any{"commit": body}, false), nil
		case "preview":
			return s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/commits/preview", Body: body}), nil
		case "push":
			return s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/commits", Body: body}), nil
		case "recover":
			return s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/commits/" + in.ID}), nil
		default:
			return toolError("invalid_action"), nil
		}
	})
	s.addFileTools()
	s.addSkillTools()
}
