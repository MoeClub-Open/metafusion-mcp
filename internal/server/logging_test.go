package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUpstreamCallsLoggedWithRequestID(t *testing.T) {
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Request-ID"))
		if r.URL.Query().Get("limit") == "0" {
			w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
			writeJSON(w, 400, map[string]any{"error": "invalid_limit"})
			return
		}
		writeJSON(w, 200, map[string]any{"items": []any{}})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	var buf bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := principalContext(Principal{PAT: "mfp_secret", Grant: Grant{UserID: "u1", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)
	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_search", Arguments: map[string]any{"query": map[string]any{"limit": "0", "q": "private-query"}}})
	if e != nil {
		t.Fatal(e)
	}
	m := structured(t, r)
	r2, _ := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_search", Arguments: map[string]any{"query": map[string]any{"q": "x"}}})
	m2 := structured(t, r2)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || len(seen) != 2 || seen[0] == "" {
		t.Fatal("expected two upstream log lines", buf.String(), seen)
	}
	var l0, l1 map[string]any
	json.Unmarshal([]byte(lines[0]), &l0)
	json.Unmarshal([]byte(lines[1]), &l1)
	if m["request_id"] != seen[0] || l0["request_id"] != seen[0] || l0["tool"] != "metafusion_search" || l0["status"] != float64(400) || l0["error"] != "invalid_limit" || l0["method"] != "GET" || l0["path"] != "/api/catalog/entities" || l0["level"] != "WARN" {
		t.Fatal("error log mismatch", l0, m)
	}
	if _, ok := l0["duration_ms"]; !ok {
		t.Fatal("no duration")
	}
	if m2["ok"] != true || m2["request_id"] != seen[1] || l1["request_id"] != seen[1] || l1["status"] != float64(200) {
		t.Fatal("success log mismatch", l1, m2)
	}
	if strings.Contains(buf.String(), "mfp_secret") || strings.Contains(buf.String(), "private-query") {
		t.Fatal("log leaked credential or query")
	}
}

func TestAPICallArgumentValidationListsAllProblems(t *testing.T) {
	s := New(testConfig("http://127.0.0.1:1"), nil)
	s.Log = nil
	ctx := principalContext(Principal{PAT: "mfp_x", Grant: Grant{UserID: "u", Scopes: []string{"mcp.read"}}})
	cs := connect(t, s, ctx)
	r, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_api_call", Arguments: map[string]any{"method": "FETCH", "path": "catalog", "bogus": 1, "headers": map[string]any{"x": 2}}})
	if e != nil || r.IsError {
		t.Fatal(e)
	}
	m := structured(t, r)
	details := mustJSON(m["details"])
	for _, want := range []string{`"/method"`, `"/path"`, `"/bogus"`, `"/headers/x"`, "unknown property"} {
		if !strings.Contains(details, want) {
			t.Error("missing", want, details)
		}
	}
	if m["error"] != "invalid_tool_arguments" || m["status"] != float64(400) {
		t.Fatal(m)
	}
	r, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_api_call", Arguments: map[string]any{"path": "/api/catalog/entities"}})
	if !strings.Contains(mustJSON(structured(t, r)["details"]), `"/method"`) {
		t.Fatal("missing method not reported")
	}
}
