package server

import "fmt"

// Tool input schemas derived from the MetaFusion catalog OpenAPI DTOs
// (backend/internal/catalog/openapi.go: CatalogCommit, CommitOperation,
// CommitPatch, SourceInput, Entity, Relation, IdentityCandidateQuery,
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
func stringMap() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": str()}
}

// catalogDefs is shared by every catalog tool schema.
func catalogDefs() map[string]any {
	return map[string]any{
		// {"$ref":"name"} refers to an operation ref created earlier in the same commit.
		"LocalRef": obj(map[string]any{"$ref": map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{0,63}$"}}, "$ref"),
		"IDOrRef":  map[string]any{"anyOf": []any{str(), ref("LocalRef")}, "description": "Entity/relation UUID, or {\"$ref\":\"name\"} of an earlier operation in this commit"},
		// Commit-level evidence pool. Every source needs a unique "key", and every
		// source must be cited by at least one operation's source_refs
		// (MetaFusion backend contract: source_key_required / source_unused).
		"SourceInput": obj(map[string]any{
			"kind":     map[string]any{"type": "string", "enum": []string{"url", "publication"}},
			"citation": map[string]any{"type": "string", "minLength": 1},
			"url":      str(),
			"key":      map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{0,63}$", "description": "Required commit-local name, unique within the commit; every source must be cited by some operation's source_refs"},
		}, "kind", "citation", "key"),
		// Stored/revision source as read back from the API. scope=commit marks
		// historical revisions written before source_refs: the whole commit's pool,
		// not field-level evidence for that revision.
		"Source":      obj(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"url", "publication", "self"}}, "citation": str(), "url": str(), "key": str(), "pointers": arr(str()), "scope": map[string]any{"type": "string", "enum": []string{"commit"}}}),
		"TimeSpan":    obj(map[string]any{"begin": str(), "end": str()}),
		"Translation": obj(map[string]any{"title": str(), "summary": str(), "aliases": arr(str())}),
		"Inclusion":   obj(map[string]any{"expression_id": ref("IDOrRef"), "position": map[string]any{"type": "integer"}, "locator": freeObject(), "attributes": freeObject(), "sources": arr(ref("Source"))}),
		"Picture":     obj(map[string]any{"url": str(), "asset_id": str(), "role": str(), "caption": stringMap(), "version_label": stringMap(), "taken_at": str(), "usage_period": ref("TimeSpan"), "source": ref("Source")}),
		"Subject":     obj(map[string]any{"work_id": ref("IDOrRef"), "role": str(), "position": map[string]any{"type": "integer"}, "attributes": freeObject()}),
		"Entity": obj(map[string]any{
			"id": ref("IDOrRef"), "kind": map[string]any{"type": "string", "enum": entityKinds}, "title": str(), "status": str(), "version": map[string]any{"type": "integer"},
			"original_language": str(), "number": str(), "position": map[string]any{"type": "integer"},
			"parent_id": ref("IDOrRef"), "work_id": ref("IDOrRef"), "content_unit_id": ref("IDOrRef"), "release_id": ref("IDOrRef"), "medium_id": ref("IDOrRef"), "redirect_id": ref("IDOrRef"),
			"attributes": freeObject(), "external_links": arr(obj(map[string]any{"kind": map[string]any{"type": "string", "minLength": 1}, "url": map[string]any{"type": "string", "minLength": 1}, "external_id": map[string]any{"type": "string"}, "role": map[string]any{"type": "string"}, "origin": map[string]any{"type": "string"}}, "kind", "url"), "maxItems", 50), "translations": map[string]any{"type": "object", "additionalProperties": ref("Translation")},
			"contents": arr(ref("Inclusion")), "pictures": arr(ref("Picture"), "maxItems", 40), "subjects": arr(ref("Subject")),
			"created_by": str(), "updated_at": str(),
		}, "kind"),
		"Relation": obj(map[string]any{"id": ref("IDOrRef"), "type": map[string]any{"type": "string", "minLength": 1}, "source_id": ref("IDOrRef"), "target_id": ref("IDOrRef"), "via": ref("IDOrRef"), "position": map[string]any{"type": "integer"}, "attributes": freeObject(), "version": map[string]any{"type": "integer"}}, "type", "source_id", "target_id"),
		"CommitPatch": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"path"},
			"properties": map[string]any{"path": map[string]any{"type": "string", "pattern": "^/"}, "value": map[string]any{}, "remove": map[string]any{"type": "boolean"}},
			"oneOf": []any{
				map[string]any{"required": []string{"value"}, "properties": map[string]any{"remove": map[string]any{"const": false}}},
				map[string]any{"required": []string{"remove"}, "properties": map[string]any{"remove": map[string]any{"const": true}}, "not": map[string]any{"required": []string{"value"}}},
			},
		},
		// [{source: key, pointers: [json-pointer]}]: only the cited sources are
		// written to this operation's revision, with their field pointers.
		"SourceRefs": arr(obj(map[string]any{"source": map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{0,63}$", "description": "key of a commit source"}, "pointers": arr(map[string]any{"type": "string", "pattern": "^/"}, "description", "JSON Pointer field paths this source supports (e.g. /attributes/catalog_number); omit for the whole operation")}, "source"), "minItems", 1, "description", "Required evidence for this operation (at least one entry): only the cited sources are written to its revision, with their field pointers. Missing or empty is rejected (source_refs_required)"),
		"CommitOperation": map[string]any{"oneOf": []any{
			obj(map[string]any{"target": map[string]any{"const": "entity"}, "action": map[string]any{"const": "create"}, "ref": ref("RefName"), "document": ref("Entity"), "reviewed_candidate_ids": arr(str(), "maxItems", 1000), "source_refs": ref("SourceRefs")}, "target", "action", "ref", "document", "reviewed_candidate_ids", "source_refs"),
			obj(map[string]any{"target": map[string]any{"const": "relation"}, "action": map[string]any{"const": "create"}, "ref": ref("RefName"), "document": ref("Relation"), "reviewed_candidate_ids": arr(str(), "maxItems", 1000), "source_refs": ref("SourceRefs")}, "target", "action", "ref", "document", "source_refs"),
			obj(map[string]any{"target": map[string]any{"type": "string", "enum": []string{"entity", "relation"}}, "action": map[string]any{"const": "update"}, "id": map[string]any{"type": "string", "format": "uuid"}, "base_version": map[string]any{"type": "integer", "minimum": 1}, "patch": arr(ref("CommitPatch"), "minItems", 1, "maxItems", 200), "reviewed_candidate_ids": arr(str(), "maxItems", 1000), "source_refs": ref("SourceRefs")}, "target", "action", "id", "base_version", "patch", "source_refs"),
		}},
		"RefName": map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{0,63}$"},
	}
}

func withDefs(root map[string]any) map[string]any {
	root["$defs"] = catalogDefs()
	return root
}

func commitPrepareSchema() map[string]any {
	s := obj(map[string]any{
		"definitions_etag": map[string]any{"type": "string", "minLength": 1},
		"edit_note":        map[string]any{"type": "string", "minLength": 1, "pattern": `\S`},
		"sources":          arr(ref("SourceInput"), "minItems", 1),
		"operations":       arr(ref("CommitOperation"), "minItems", 1, "maxItems", 100),
	}, "definitions_etag", "edit_note", "sources", "operations")
	return withDefs(s)
}

func candidateQuerySchema() map[string]any {
	val := map[string]any{"type": "string", "maxLength": 256}
	return obj(map[string]any{
		"kind":         map[string]any{"type": "string", "enum": entityKinds},
		"titles":       arr(val, "maxItems", 20),
		"external_links": arr(obj(map[string]any{"kind": map[string]any{"type": "string", "minLength": 1}, "value": val}, "kind", "value"), "maxItems", 20),
		"attributes":   arr(obj(map[string]any{"key": map[string]any{"type": "string", "minLength": 1}, "value": val}, "key", "value"), "maxItems", 20),
		"work_id":      map[string]any{"type": "string", "format": "uuid"},
		"release_id":   map[string]any{"type": "string", "format": "uuid"},
		"medium_id":    map[string]any{"type": "string", "format": "uuid"},
		"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
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

// sourceRefIssues cross-checks the evidence pool against operations, which JSON
// Schema cannot express: keys are unique, every cited key exists, and every
// pool source is cited by some operation. Mirrors the backend errors so an agent
// learns about them at prepare time instead of at preview.
func sourceRefIssues(body map[string]any) []schemaIssue {
	var issues []schemaIssue
	keys := map[string]bool{}
	sources, _ := body["sources"].([]any)
	for i, raw := range sources {
		src, _ := raw.(map[string]any)
		key, _ := src["key"].(string)
		if key == "" {
			continue // the schema already reports a missing key
		}
		if keys[key] {
			issues = append(issues, schemaIssue{Path: fmt.Sprintf("/sources/%d/key", i), Message: "duplicate_source_key"})
		}
		keys[key] = true
	}
	used := map[string]bool{}
	ops, _ := body["operations"].([]any)
	for i, raw := range ops {
		op, _ := raw.(map[string]any)
		refs, _ := op["source_refs"].([]any)
		seen := map[string]bool{}
		for j, r := range refs {
			ref, _ := r.(map[string]any)
			key, _ := ref["source"].(string)
			at := fmt.Sprintf("/operations/%d/source_refs/%d/source", i, j)
			switch {
			case !keys[key]:
				issues = append(issues, schemaIssue{Path: at, Message: "unknown_source_ref"})
			case seen[key]:
				issues = append(issues, schemaIssue{Path: at, Message: "duplicate_source_ref"})
			}
			seen[key] = true
			used[key] = true
		}
	}
	for i, raw := range sources {
		src, _ := raw.(map[string]any)
		if key, _ := src["key"].(string); key != "" && !used[key] {
			issues = append(issues, schemaIssue{Path: fmt.Sprintf("/sources/%d", i), Message: "source_unused"})
		}
	}
	return issues
}
