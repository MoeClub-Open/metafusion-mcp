package server

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// commitDiff summarizes a sealed draft per operation. For entity updates it
// reads the current entity once (read-only GET) to show old -> new values and
// whether base_version is stale. Nothing is written or replayed.

func pointerGet(doc any, pointer string) (any, bool) {
	if pointer == "" {
		return doc, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	cur := doc
	for _, raw := range strings.Split(pointer[1:], "/") {
		seg := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch x := cur.(type) {
		case map[string]any:
			v, ok := x[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, e := strconv.Atoi(seg)
			if e != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			cur = x[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func docFields(doc map[string]any) []string {
	return sortedKeys(doc)
}

func (s *Server) commitDiff(ctx context.Context, body any) *mcp.CallToolResult {
	commit, _ := body.(map[string]any)
	ops := asMaps(commit["operations"])
	out := make([]map[string]any, 0, len(ops))
	cache := map[string]map[string]any{}
	stale, unread := 0, 0
	for i, op := range ops {
		item := map[string]any{"index": i, "target": op["target"], "action": op["action"]}
		item["source_refs"] = op["source_refs"]
		switch op["action"] {
		case "create":
			doc, _ := op["document"].(map[string]any)
			item["ref"] = op["ref"]
			item["fields"] = docFields(doc)
			if op["target"] == "entity" {
				item["kind"], item["title"] = doc["kind"], doc["title"]
				item["reviewed_candidate_ids"] = op["reviewed_candidate_ids"]
			} else {
				item["type"], item["source_id"], item["target_id"] = doc["type"], doc["source_id"], doc["target_id"]
			}
		case "delete", "content":
			item["id"], item["base_version"] = op["id"], op["base_version"]
			item["content_change"] = op["content_change"]
			item["note"] = "Proposed operation; preview validates it. Pending submissions do not change current facts."
		case "update":
			id, _ := op["id"].(string)
			item["id"], item["base_version"] = id, op["base_version"]
			var current map[string]any
			if op["target"] == "entity" && uuidPattern.MatchString(id) {
				if c, ok := cache[id]; ok {
					current = c
				} else if data, bad := okData(s.callAPI(ctx, APIInput{Method: "GET", Path: "/api/catalog/entities/" + id})); bad == nil {
					current = data
					cache[id] = data
				} else {
					item["read_error"] = bad["error"]
				}
			}
			if current != nil {
				item["kind"], item["title"], item["current_version"] = current["kind"], current["title"], current["version"]
				if cv, ok := current["version"].(float64); ok {
					if bv, ok := op["base_version"].(float64); ok && bv != cv {
						item["stale_base"] = true
						stale++
					}
				}
			} else {
				unread++
			}
			var changes []map[string]any
			for _, p := range asMaps(op["patch"]) {
				path, _ := p["path"].(string)
				c := map[string]any{"path": path}
				if rm, _ := p["remove"].(bool); rm {
					c["op"] = "remove"
				} else {
					c["op"] = "set"
					c["new"] = p["value"]
				}
				if current != nil {
					if old, ok := pointerGet(current, path); ok {
						c["old"] = old
						if c["op"] == "set" && jsonEqual(old, p["value"]) {
							c["no_op"] = true
						}
					} else {
						c["old_absent"] = true
					}
				}
				changes = append(changes, c)
			}
			item["changes"] = changes
		}
		out = append(out, item)
	}
	return result(map[string]any{"ok": true, "id": commit["id"], "edit_note": commit["edit_note"], "source_count": len(asMaps(commit["sources"])), "operation_count": len(ops), "operations": out, "stale_base_versions": stale, "updates_without_current_snapshot": unread, "note": "old values come from the current upstream read, not from base_version; preview remains authoritative"}, false)
}
