package server

import "regexp"

var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// Tool input schemas derived from the MetaFusion catalog OpenAPI DTOs
// (backend/internal/catalog/collaboration_types.go: WorkspaceOperation;
// openapi.go: CommitPatch, Inclusion, IdentityCandidateQuery,
// RelationshipQueryRequest). They are validated at tool entry so agents get
// every bad path at once; the upstream API remains authoritative for
// definitions-driven rules (field codes, vocabularies, relationship rules).

var entityKinds = []string{"agent", "collection", "work", "content_unit", "expression", "release", "medium", "track"}

func str() map[string]any { return map[string]any{"type": "string"} }
func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/$defs/" + name}
}
func arr(items any, extra ...any) map[string]any {
	m := map[string]any{"type": "array", "items": items}
	for i := 0; i+1 < len(extra); i += 2 {
		m[extra[i].(string)] = extra[i+1]
	}
	return m
}
func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
func freeObject() map[string]any { return map[string]any{"type": "object"} }

// catalogDefs is shared by every catalog tool schema.
func catalogDefs() map[string]any {
	return map[string]any{
		// Stored/revision source as read back from the API. scope=commit marks
		// historical revisions written before source_refs: the whole commit's pool,
		// not field-level evidence for that revision.
		"Source":    obj(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"", "url", "publication", "self"}}, "citation": str(), "url": str(), "key": str(), "pointers": arr(str()), "scope": map[string]any{"type": "string", "enum": []string{"commit"}}}),
		"Inclusion": obj(map[string]any{"expression_id": uuidSchema(), "position": map[string]any{"type": "integer"}, "locator": map[string]any{"anyOf": []any{freeObject(), map[string]any{"type": "null"}}}, "attributes": freeObject(), "sources": arr(ref("Source"))}),
		"CommitPatch": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string", "pattern": "^/"}, "value": map[string]any{}, "remove": map[string]any{"type": "boolean"}},
			"oneOf": []any{
				map[string]any{"required": []string{"value"}, "properties": map[string]any{"remove": map[string]any{"const": false}}},
				map[string]any{"required": []string{"remove"}, "properties": map[string]any{"remove": map[string]any{"const": true}}, "not": map[string]any{"required": []string{"value"}}},
			},
		},
		"WorkspaceOperation": map[string]any{"oneOf": []any{
			obj(map[string]any{"target": map[string]any{"type": "string", "enum": []string{"entity", "relation"}}, "action": map[string]any{"const": "delete"}, "id": ref("OperationID")}, "target", "action", "id"),
			obj(map[string]any{"target": map[string]any{"const": "entity"}, "action": map[string]any{"const": "content"}, "id": ref("OperationID"), "content_change": obj(map[string]any{"action": map[string]any{"enum": []string{"add", "replace", "delete"}}, "position": map[string]any{"type": "integer", "minimum": 0}, "inclusion": ref("Inclusion")}, "action", "position")}, "target", "action", "id", "content_change"),
			obj(map[string]any{"target": map[string]any{"const": "entity"}, "action": map[string]any{"const": "create"}, "ref": ref("RefName"), "document": freeObject()}, "target", "action", "ref", "document"),
			obj(map[string]any{"target": map[string]any{"const": "relation"}, "action": map[string]any{"const": "create"}, "ref": ref("RefName"), "document": freeObject()}, "target", "action", "ref", "document"),
			workspaceUpdateOperation("entity"),
			workspaceUpdateOperation("relation"),
		}},
		"OperationID": map[string]any{"anyOf": []any{uuidSchema(), map[string]any{"type": "string", "pattern": "^@local:[A-Za-z][A-Za-z0-9_-]{0,63}$"}}},
		"RefName":     map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{0,63}$"},
	}
}

func workspaceUpdateOperation(target string) map[string]any {
	s := obj(map[string]any{
		"target": map[string]any{"const": target}, "action": map[string]any{"const": "update"},
		"id": ref("OperationID"), "document": freeObject(), "patch": arr(ref("CommitPatch"), "minItems", 1, "maxItems", 200),
	}, "target", "action", "id")
	s["oneOf"] = []any{
		map[string]any{"required": []string{"document"}, "not": map[string]any{"required": []string{"patch"}}},
		map[string]any{"required": []string{"patch"}, "not": map[string]any{"required": []string{"document"}}},
	}
	return s
}

func withDefs(root map[string]any) map[string]any {
	root["$defs"] = catalogDefs()
	return root
}

func uuidSchema() map[string]any { return map[string]any{"type": "string", "format": "uuid"} }

func workspaceStageSchema() map[string]any {
	return withDefs(obj(map[string]any{
		"workspace_id":     uuidSchema(),
		"expected_version": map[string]any{"type": "integer", "minimum": 1},
		"operations":       arr(ref("WorkspaceOperation"), "minItems", 1, "maxItems", 100),
	}, "workspace_id", "expected_version", "operations"))
}

func candidateQuerySchema() map[string]any {
	val := map[string]any{"type": "string", "maxLength": 256}
	return obj(map[string]any{
		"kind":           map[string]any{"type": "string", "enum": entityKinds},
		"titles":         arr(val, "maxItems", 20),
		"external_links": arr(obj(map[string]any{"kind": map[string]any{"type": "string", "minLength": 1}, "value": val}, "kind", "value"), "maxItems", 20),
		"attributes":     arr(obj(map[string]any{"key": map[string]any{"type": "string", "minLength": 1}, "value": val}, "key", "value"), "maxItems", 20),
		"work_id":        map[string]any{"type": "string", "format": "uuid"},
		"release_id":     map[string]any{"type": "string", "format": "uuid"},
		"medium_id":      map[string]any{"type": "string", "format": "uuid"},
		"limit":          map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
	}, "kind")
}

func relationshipQuerySchema() map[string]any {
	return obj(map[string]any{
		"ids":        arr(map[string]any{"type": "string", "format": "uuid"}, "minItems", 1, "maxItems", 20),
		"direction":  map[string]any{"type": "string", "enum": []string{"both", "outgoing", "incoming"}},
		"rule_codes": arr(str()),
		"peer_kinds": arr(map[string]any{"type": "string", "enum": entityKinds}),
		"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"offset":     map[string]any{"type": "integer", "minimum": 0},
	}, "ids")
}

// shortcutSchema wraps a request-body schema in the {query, body} tool input.
func shortcutSchema(body map[string]any) map[string]any {
	return obj(map[string]any{"query": map[string]any{"type": "object", "additionalProperties": true}, "body": body}, "body")
}
