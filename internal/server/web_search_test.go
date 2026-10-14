package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWebSearchProvidersThroughSDK(t *testing.T) {
	for _, provider := range []string{"duckduckgo", "bing", "brave", "searxng"} {
		t.Run(provider, func(t *testing.T) {
			c := testConfig("https://platform.example")
			c.SearchProvider = provider
			c.SearchURL = "https://search.example/search"
			c.SearchAPIKey = "test-provider-key"
			s := New(c, nil)
			var calls int
			s.WebHTTP.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Query().Get("q") != "音乐 元数据" || r.Header.Get("Authorization") != "" {
					t.Fatal(r.URL, r.Header)
				}
				switch provider {
				case "bing":
					if r.URL.Query().Get("format") != "rss" {
						t.Fatal(r.URL)
					}
					return webResponse(200, `<rss><channel><item><title>Music</title><link>https://music.example/a</link><description>Evidence</description></item></channel></rss>`), nil
				case "duckduckgo":
					return webResponse(200, `<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fmusic.example%2Fa">Music <b>&amp; data</b></a><a class="result__snippet">Original evidence</a></div><div class="result"><a class="result__a" href="http://127.0.0.1">private</a></div>`), nil
				case "brave":
					if r.Header.Get("X-Subscription-Token") != "test-provider-key" || r.URL.Query().Get("count") != "1" {
						t.Fatal(r.Header, r.URL)
					}
					return webResponse(200, `{"web":{"results":[{"title":"Music","url":"https://music.example/a","description":"Evidence"}]}}`), nil
				default:
					if r.URL.Query().Get("format") != "json" {
						t.Fatal(r.URL)
					}
					return webResponse(200, `{"results":[{"title":"Music","url":"https://music.example/a","content":"Evidence"}]}`), nil
				}
			})
			ctx := webContext("mcp.read")
			cs := connect(t, s, ctx)
			r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_web_search", Arguments: map[string]any{"q": "音乐 元数据", "limit": 1}})
			if err != nil {
				t.Fatal(err)
			}
			m := structured(t, r)
			items, ok := m["items"].([]any)
			if m["ok"] != true || !ok || len(items) != 1 || items[0].(map[string]any)["url"] != "https://music.example/a" || m["provider"] != provider || calls != 1 {
				t.Fatal(m, calls)
			}
			fetch := m["fetch"].(map[string]any)
			if fetch["content_hash"] == "" || fetch["fetched_at"] == "" || fetch["complete"] != true {
				t.Fatal(fetch)
			}
		})
	}
}

func TestSearchChallengeMalformedAndEmptyResults(t *testing.T) {
	for _, tc := range []struct {
		provider, body string
		success        bool
	}{
		{"duckduckgo", `<form>Unfortunately, bots use DuckDuckGo too.</form>`, false},
		{"duckduckgo", `<div class="no-results">No results found</div>`, true},
		{"bing", `<html><body>Search challenge</body></html>`, false},
		{"bing", `<rss><channel/></rss>`, true},
		{"brave", `{"error":"quota"}`, false},
		{"brave", `{"web":{"results":[]}}`, true},
		{"searxng", `{"results":[],"unresponsive_engines":[["bing","timeout"]]}`, false},
		{"searxng", `{"results":[]}`, true},
	} {
		c := testConfig("https://platform.example")
		c.SearchProvider = tc.provider
		c.SearchURL = "https://search.example/search"
		c.SearchAPIKey = "test"
		s := New(c, nil)
		s.WebHTTP.Transport = webRoundTrip(func(*http.Request) (*http.Response, error) { return webResponse(200, tc.body), nil })
		m := structured(t, s.webSearch(webContext("mcp.read"), webSearchInput{Query: "data"}))
		if (m["ok"] == true) != tc.success {
			t.Fatal(tc, m)
		}
	}
	s := New(testConfig("https://platform.example"), nil)
	s.WebHTTP.Transport = webRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid/authless search reached network")
		return nil, nil
	})
	if m := structured(t, s.webSearch(context.Background(), webSearchInput{Query: "data"})); m["error"] != "authentication_required" {
		t.Fatal(m)
	}
	if m := structured(t, s.webSearch(webContext("mcp.read"), webSearchInput{Query: strings.Repeat(" ", 3)})); m["error"] != "invalid_query" {
		t.Fatal(m)
	}
}
