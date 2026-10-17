package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// catalog_graph_read: one call that reads semantic relations plus structural
// and attribute-reference links, follows pagination until has_more=false, and
// says explicitly whether the returned graph is complete.

const (
	graphBatch       = 20  // backend /relationships/query subject limit
	graphPageLimit   = 100 // backend max page size
	graphMaxPages    = 20  // per subject per depth level
	graphMaxDepth    = 3
	graphMaxEntities = 200
)

type graphInput struct {
	IDs         []string `json:"ids"`
	Depth       int      `json:"depth"`
	Direction   string   `json:"direction"`
	RuleCodes   []string `json:"rule_codes"`
	PeerKinds   []string `json:"peer_kinds"`
	MaxEntities int      `json:"max_entities"`
}

func graphSchema() map[string]any {
	return obj(map[string]any{
		"ids":          arr(map[string]any{"type": "string", "format": "uuid"}, "minItems", 1, "maxItems", graphMaxEntities),
		"depth":        map[string]any{"type": "integer", "minimum": 1, "maximum": graphMaxDepth, "description": "Hops to expand (default 1). 3 covers Work -> Release -> Medium -> Track."},
		"direction":    map[string]any{"type": "string", "enum": []string{"both", "outgoing", "incoming"}},
		"rule_codes":   arr(str()),
		"peer_kinds":   arr(map[string]any{"type": "string", "enum": entityKinds}),
		"max_entities": map[string]any{"type": "integer", "minimum": 1, "maximum": graphMaxEntities},
	}, "ids")
}

func edgeCategory(class string) string {
	switch class {
	case "semantic":
		return "semantic"
	case "reference":
		return "reference"
	}
	return "structural"
}

// okData returns data of a successful callAPI result, or the structured error.
func okData(r *mcp.CallToolResult) (map[string]any, map[string]any) {
	m, _ := r.StructuredContent.(map[string]any)
	if r.IsError || m == nil || m["ok"] != true {
		return nil, m
	}
	d, _ := m["data"].(map[string]any)
	if d == nil {
		return nil, map[string]any{"error": "unexpected_response"}
	}
	return d, nil
}

func asMaps(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, x := range list {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (s *Server) graphRead(ctx context.Context, in graphInput) *mcp.CallToolResult {
	if in.Depth == 0 {
		in.Depth = 1
	}
	if in.Direction == "" {
		in.Direction = "both"
	}
	if in.MaxEntities == 0 {
		in.MaxEntities = graphMaxEntities
	}
	edges := map[string]map[string]any{}
	entities := map[string]any{}
	visited := map[string]int{}
	var reasons []string
	var errorsSeen []any
	unavailable := map[string]bool{}
	etag := ""
	frontier := []string{}
	for _, id := range in.IDs {
		if _, ok := visited[id]; !ok {
			visited[id] = 0
			frontier = append(frontier, id)
		}
	}
	reason := func(r string) {
		for _, x := range reasons {
			if x == r {
				return
			}
		}
		reasons = append(reasons, r)
	}
	addEdge := func(link map[string]any) {
		key, _ := link["key"].(string)
		if key == "" {
			key = fmt.Sprintf("%v|%v|%v|%v", link["rule_code"], link["source_id"], link["target_id"], link["field"])
		}
		if _, ok := edges[key]; ok {
			return
		}
		class, _ := link["class"].(string)
		edge := map[string]any{"key": key, "category": edgeCategory(class), "class": class, "rule_code": link["rule_code"], "source_id": link["source_id"], "target_id": link["target_id"]}
		for _, field := range []string{"version", "position", "role", "field", "locator", "attributes", "references", "via"} {
			if value, ok := link[field]; ok {
				edge[field] = value
			}
		}
		edges[key] = edge
	}
	for depth := 1; depth <= in.Depth && len(frontier) > 0; depth++ {
		next := []string{}
		discover := func(peer string) {
			if peer == "" {
				return
			}
			if _, seen := visited[peer]; seen {
				return
			}
			if len(visited) >= in.MaxEntities {
				reason("max_entities_reached")
				return
			}
			visited[peer] = depth
			if depth < in.Depth {
				next = append(next, peer)
			}
		}
		for start := 0; start < len(frontier); start += graphBatch {
			batch := frontier[start:min(start+graphBatch, len(frontier))]
			pending := batch
			for page := 0; len(pending) > 0; page++ {
				if page >= graphMaxPages {
					reason("page_cap_reached")
					break
				}
				body := map[string]any{"ids": pending, "direction": in.Direction, "limit": graphPageLimit, "offset": page * graphPageLimit}
				if len(in.RuleCodes) > 0 {
					body["rule_codes"] = in.RuleCodes
				}
				if len(in.PeerKinds) > 0 {
					body["peer_kinds"] = in.PeerKinds
				}
				data, bad := okData(s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/catalog/relationships/query", Body: body}))
				if bad != nil {
					reason("upstream_error")
					errorsSeen = append(errorsSeen, map[string]any{"step": "relationships_query", "ids": pending, "offset": page * graphPageLimit, "error": bad})
					break
				}
				if e, _ := data["definition_etag"].(string); e != "" {
					etag = e
				}
				if ents, ok := data["entities"].(map[string]any); ok {
					for k, v := range ents {
						entities[k] = v
					}
				}
				for _, u := range asStrings(data["unavailable_ids"]) {
					unavailable[u] = true
				}
				var more []string
				for _, p := range asMaps(data["pages"]) {
					subject, _ := p["subject_id"].(string)
					for _, link := range asMaps(p["items"]) {
						addEdge(link)
						src, _ := link["source_id"].(string)
						dst, _ := link["target_id"].(string)
						if src == subject {
							discover(dst)
						} else {
							discover(src)
						}
					}
					if hm, _ := p["has_more"].(bool); hm {
						more = append(more, subject)
					}
				}
				pending = more
			}
		}
		frontier = next
	}
	if len(unavailable) > 0 {
		reason("unavailable_ids")
	}
	list := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i]["key"].(string) < list[j]["key"].(string) })
	counts := map[string]int{}
	for _, e := range list {
		counts[e["category"].(string)]++
	}
	un := make([]string, 0, len(unavailable))
	for u := range unavailable {
		un = append(un, u)
	}
	sort.Strings(un)
	out := map[string]any{"ok": true, "complete": len(reasons) == 0, "incomplete_reasons": reasons, "depth": in.Depth, "definition_etag": etag, "visited": visited, "edges": list, "edge_counts": counts, "entities": entities, "unavailable_ids": un}
	if len(errorsSeen) > 0 {
		out["errors"] = errorsSeen
	}
	return result(out, false)
}

func asStrings(v any) []string {
	list, _ := v.([]any)
	out := []string{}
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (s *Server) addGraphTool() {
	schema := withDefs(graphSchema())
	s.MCP.AddTool(&mcp.Tool{Name: "catalog_graph_read", Description: "Read the relationship graph around 1+ entity UUIDs in one call: structural (ownership/placement), attribute-reference and semantic edges from /relationships/query (batched by 20, every page followed until has_more=false), deduplicated by edge key. Each edge has category semantic|structural|reference; semantic edges include their current version and attributes. depth 1-3 (default 1) = edges within that many hops; visited maps every reached entity to its hop distance, e.g. depth 3 for Work -> Release -> Medium -> Track. complete=false with incomplete_reasons (page_cap_reached, max_entities_reached, unavailable_ids, upstream_error) means absence of an edge is NOT proof it does not exist.", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, bad := validateArgs(schema, r.Params.Arguments); bad != nil {
			return bad, nil
		}
		var in graphInput
		_ = json.Unmarshal(r.Params.Arguments, &in)
		return s.graphRead(ctx, in), nil
	})
}
