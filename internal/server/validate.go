package server

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A small JSON Schema subset validator that, unlike a first-error validator,
// collects every violation with its JSON Pointer so the agent can fix the
// whole request in one round. Supported: type, properties, required,
// additionalProperties, items, enum, const, min/maxItems, minimum/maximum,
// min/maxLength, pattern, format uuid, oneOf, anyOf, not and local $ref.

type schemaIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

var patternCache sync.Map

func compiled(p string) *regexp.Regexp {
	if r, ok := patternCache.Load(p); ok {
		return r.(*regexp.Regexp)
	}
	r := regexp.MustCompile(p)
	patternCache.Store(p, r)
	return r
}

func pointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	case json.Number:
		if _, e := x.Int64(); e == nil {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, e := x.Float64()
		return f, e == nil
	}
	return 0, false
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

type validator struct{ defs map[string]any }

func validateSchema(root map[string]any, instance any) []schemaIssue {
	defs, _ := root["$defs"].(map[string]any)
	v := validator{defs: defs}
	issues := v.check(root, instance, "")
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return issues
}

func (v validator) check(schema map[string]any, x any, at string) []schemaIssue {
	var out []schemaIssue
	add := func(path, format string, args ...any) {
		out = append(out, schemaIssue{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if r, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(r, "#/$defs/")
		target, _ := v.defs[name].(map[string]any)
		if target == nil {
			add(at, "unresolvable schema reference %s", r)
			return out
		}
		out = append(out, v.check(target, x, at)...)
	}
	if t, ok := schema["type"].(string); ok {
		got := jsonType(x)
		if got != t && !(t == "number" && got == "integer") {
			add(at, "expected %s, got %s", t, got)
			return out
		}
	}
	if c, ok := schema["const"]; ok && !jsonEqual(c, x) {
		add(at, "must equal %v", c)
	}
	if e, ok := schema["enum"]; ok {
		found := false
		b, _ := json.Marshal(e)
		var list []any
		_ = json.Unmarshal(b, &list)
		for _, item := range list {
			if jsonEqual(item, x) {
				found = true
			}
		}
		if !found {
			add(at, "must be one of %s", string(b))
		}
	}
	if s, ok := x.(string); ok {
		n := len([]rune(s))
		if m, ok := schema["minLength"].(int); ok && n < m {
			add(at, "must be at least %d characters", m)
		}
		if m, ok := schema["maxLength"].(int); ok && n > m {
			add(at, "must be at most %d characters", m)
		}
		if p, ok := schema["pattern"].(string); ok && !compiled(p).MatchString(s) {
			add(at, "must match pattern %s", p)
		}
		if schema["format"] == "uuid" && !uuidPattern.MatchString(s) {
			add(at, "must be a UUID")
		}
	}
	if f, ok := toFloat(x); ok {
		if m, ok := schema["minimum"].(int); ok && f < float64(m) {
			add(at, "must be >= %d", m)
		}
		if m, ok := schema["maximum"].(int); ok && f > float64(m) {
			add(at, "must be <= %d", m)
		}
	}
	if a, ok := x.([]any); ok {
		if m, ok := schema["minItems"].(int); ok && len(a) < m {
			add(at, "must contain at least %d items", m)
		}
		if m, ok := schema["maxItems"].(int); ok && len(a) > m {
			add(at, "must contain at most %d items", m)
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range a {
				out = append(out, v.check(items, item, fmt.Sprintf("%s/%d", at, i))...)
			}
		}
	}
	if o, ok := x.(map[string]any); ok {
		props, _ := schema["properties"].(map[string]any)
		if req, ok := schema["required"].([]string); ok {
			for _, k := range req {
				if _, ok := o[k]; !ok {
					add(at+"/"+pointerEscape(k), "is required")
				}
			}
		}
		keys := make([]string, 0, len(o))
		for k := range o {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := at + "/" + pointerEscape(k)
			if ps, ok := props[k].(map[string]any); ok {
				out = append(out, v.check(ps, o[k], child)...)
				continue
			}
			switch ap := schema["additionalProperties"].(type) {
			case bool:
				if !ap {
					add(child, "unknown property")
				}
			case map[string]any:
				out = append(out, v.check(ap, o[k], child)...)
			}
		}
	}
	if alts, ok := schema["anyOf"].([]any); ok {
		var best []schemaIssue
		matched := false
		for i, s := range alts {
			issues := v.check(s.(map[string]any), x, at)
			if len(issues) == 0 {
				matched = true
				break
			}
			if i == 0 || branchScore(issues, at) < branchScore(best, at) {
				best = issues
			}
		}
		if !matched {
			out = append(out, best...)
		}
	}
	if one, ok := schema["oneOf"].([]any); ok {
		var best []schemaIssue
		count := 0
		for i, s := range one {
			issues := v.check(s.(map[string]any), x, at)
			if len(issues) == 0 {
				count++
			} else if i == 0 || best == nil || branchScore(issues, at) < branchScore(best, at) {
				best = issues
			}
		}
		switch {
		case count == 0:
			out = append(out, best...)
		case count > 1:
			add(at, "matches more than one allowed shape")
		}
	}
	if not, ok := schema["not"].(map[string]any); ok && len(v.check(not, x, at)) == 0 {
		add(at, "matches a forbidden shape")
	}
	return out
}

// branchScore ranks failed alternatives: a branch whose type does not even
// match the value is the least helpful explanation.
func branchScore(issues []schemaIssue, at string) int {
	n := len(issues)
	for _, i := range issues {
		if i.Path == at && strings.HasPrefix(i.Message, "expected ") {
			n += 1000
		}
	}
	return n
}

// validateArgs decodes tool arguments and validates them; on failure it
// returns the structured invalid_tool_arguments result.
func validateArgs(schema map[string]any, raw json.RawMessage) (map[string]any, *mcp.CallToolResult) {
	var args any
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if json.Unmarshal(raw, &args) != nil {
		return nil, expectedError(400, "invalid_tool_arguments", "arguments must be a JSON object", []schemaIssue{{Path: "", Message: "invalid JSON"}}, nil)
	}
	if issues := validateSchema(schema, args); len(issues) > 0 {
		return nil, expectedError(400, "invalid_tool_arguments", fmt.Sprintf("%d argument problem(s); fix every listed path and call again", len(issues)), issues, nil)
	}
	m, _ := args.(map[string]any)
	return m, nil
}

// normalizeSchema converts a schema literal (which may contain typed maps
// such as map[string]string) into plain JSON maps the validator understands.
func normalizeSchema(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	fixInts(m)
	return m
}

// fixInts restores integer keywords decoded as float64.
func fixInts(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			switch k {
			case "minItems", "maxItems", "minLength", "maxLength", "minimum", "maximum":
				if f, ok := c.(float64); ok {
					x[k] = int(f)
					continue
				}
			case "required":
				if list, ok := c.([]any); ok {
					req := make([]string, 0, len(list))
					for _, r := range list {
						if s, ok := r.(string); ok {
							req = append(req, s)
						}
					}
					x[k] = req
					continue
				}
			}
			fixInts(c)
		}
	case []any:
		for _, c := range x {
			fixInts(c)
		}
	}
}
