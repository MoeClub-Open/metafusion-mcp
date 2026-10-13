package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"

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
	prepareSchema := commitPrepareSchema()
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_commit_prepare", Description: "Seal a catalog commit in the cloud, generating its UUID and saving the exact immutable payload for 30 days. Requires edit_note, sources (each with a unique key, every key cited by some operation), operations (1-100, every operation with source_refs:[{source:key,pointers?:[json-pointer]}]: entity create {target:entity,action:create,ref,document,reviewed_candidate_ids,source_refs}, relation create {target:relation,action:create,ref,document,source_refs}, update {target,action:update,id,base_version,patch:[{path,value}|{path,remove:true}],source_refs}) and checkout definitions_etag. Use {\"$ref\":\"name\"} for IDs created earlier in the batch. Each revision records only the sources its operation cites (e.g. source_refs [{source:key,pointers:[\"/attributes/catalog_number\"]}]). Prepare rejects broken evidence mapping with 400 invalid_source_refs (duplicate_source_key, unknown_source_ref, duplicate_source_ref, source_unused); the server additionally rejects source_refs_required, source_key_required and pointer mismatches. Arguments are validated against this schema first; a 400 invalid_tool_arguments result lists every bad path. Use prepare once; retain its ID for preview, push and recovery.", InputSchema: prepareSchema}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, ok := currentPrincipal(ctx)
		if !ok {
			return toolError("authentication_required"), nil
		}
		if !s.limiter.AllowAPI(p, "POST", "/mcp/drafts") {
			return expectedError(429, "rate_limited", "MCP rate limit exhausted", nil, 60), nil
		}
		var probe map[string]any
		if json.Unmarshal(r.Params.Arguments, &probe) == nil {
			if _, ok := probe["id"]; ok {
				return expectedError(400, "id_is_generated_by_prepare", "omit id; prepare generates the commit UUID", []schemaIssue{{Path: "/id", Message: "must be omitted"}}, nil), nil
			}
		}
		body, bad := validateArgs(prepareSchema, r.Params.Arguments)
		if bad != nil {
			return bad, nil
		}
		if issues := sourceRefIssues(body); len(issues) > 0 {
			return expectedError(400, "invalid_source_refs", "every source needs a unique key cited by some operation, and every operation must cite existing source keys", issues, nil), nil
		}
		id := newUUID()
		body["id"] = id
		raw, _ := json.Marshal(body)
		if len(raw) > 2<<20 {
			return expectedError(413, "commit_too_large", "sealed commit exceeds 2 MiB", nil, nil), nil
		}
		// Saving a workspace is not publishing catalog data; upstream preview checks the complete contract.
		_, e := s.Store.DB.ExecContext(ctx, "INSERT INTO mcp.drafts(id,user_id,payload) VALUES($1,$2,$3)", id, p.Grant.UserID, string(raw))
		if e != nil {
			return toolError("draft_unavailable"), nil
		}
		return result(map[string]any{"ok": true, "id": id, "commit": body, "expires_in_days": 30, "applied": false}, false), nil
	})
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_commit", Description: "Use a saved cloud commit by ID. get returns the exact sealed payload; diff summarizes each operation (target, entity, changed fields, old -> new read from the current upstream entity, base_version staleness) without writing; preview validates; push sends it once; recover queries the upstream receipt without replaying a write. Owner-scoped. Conflicts require a new rebased commit; unknown outcomes require recover first.", InputSchema: map[string]any{"type": "object", "required": []string{"id", "action"}, "properties": map[string]any{"id": map[string]string{"type": "string"}, "action": map[string]any{"type": "string", "enum": []string{"get", "diff", "preview", "push", "recover"}}}}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			return expectedError(400, "invalid_id", "id must be a UUID", nil, nil), nil
		}
		var raw []byte
		e := s.Store.DB.QueryRowContext(ctx, "SELECT payload FROM mcp.drafts WHERE id=$1 AND user_id=$2 AND expires_at>now()", in.ID, p.Grant.UserID).Scan(&raw)
		if e == sql.ErrNoRows {
			return expectedError(404, "draft_not_found", "no unexpired cloud commit with this id for the current user", nil, nil), nil
		}
		if e != nil {
			return toolError("draft_unavailable"), nil
		}
		var body any
		_ = json.Unmarshal(raw, &body)
		switch in.Action {
		case "get":
			return result(map[string]any{"ok": true, "commit": body}, false), nil
		case "diff":
			return s.commitDiff(ctx, body), nil
		case "preview":
			return withQualityWarnings(s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/commits/preview", Body: body})), nil
		case "push":
			return s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/commits", Body: body}), nil
		case "recover":
			return s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/commits/" + in.ID}), nil
		default:
			return expectedError(400, "invalid_action", "action must be get, diff, preview, push or recover", nil, nil), nil
		}
	})
	s.addFileTools()
	s.addSkillTools()
}
