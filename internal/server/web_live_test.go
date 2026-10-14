package server

import (
	"os"
	"testing"
)

// Explicit opt-in: real public GETs only, never third-party writes or platform data.
func TestPublicWebLive(t *testing.T) {
	if os.Getenv("MCP_TEST_WEB_LIVE") != "1" {
		t.Skip("MCP_TEST_WEB_LIVE=1 enables public read-only smoke checks")
	}
	s := New(testConfig("https://platform.example"), nil)
	defer s.WebHTTP.CloseIdleConnections()
	ctx := webContext("mcp.read")
	t.Run("http", func(t *testing.T) {
		m := structured(t, s.httpRequest(ctx, webInput{URL: "https://example.org", Timeout: 15}))
		if m["ok"] != true || m["complete"] != true {
			t.Fatalf("public HTTP smoke: status=%v error=%v", m["status"], m["error"])
		}
		t.Logf("public HTTP status=%v complete=%v", m["status"], m["complete"])
	})
	t.Run("search", func(t *testing.T) {
		m := structured(t, s.webSearch(ctx, webSearchInput{Query: "MusicBrainz official music database", Limit: 3}))
		if m["ok"] != true {
			t.Fatalf("public search smoke: status=%v error=%v", m["status"], m["error"])
		}
		if len(m["items"].([]any)) == 0 {
			t.Fatal("public search returned no results")
		}
		t.Logf("public search provider=%v results=%v", m["provider"], m["returned"])
	})
}
