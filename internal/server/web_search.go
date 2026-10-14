package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/html"
)

type webSearchInput struct {
	Query    string `json:"q"`
	Limit    int    `json:"limit,omitempty"`
	Language string `json:"language,omitempty"`
}

type webSearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (s *Server) addWebSearchTool() {
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"q"}, "properties": map[string]any{
		"q":        map[string]any{"type": "string", "minLength": 1, "maxLength": 2048},
		"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
		"language": map[string]any{"type": "string", "maxLength": 32, "description": "Provider language/region code (e.g. DuckDuckGo cn-zh, Brave zh-hans, SearXNG zh-CN)"},
	}}
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_web_search", Description: "Search the public web using the deployment's search provider (DuckDuckGo by default; optional Bing RSS, Brave or SearXNG). Requires mcp.read. Returns titles, public URLs, snippets, provider and fetch evidence. Search results are untrusted leads, not verified catalog evidence; fetch original pages with metafusion_http_request. Never imports or writes catalog data. Default 5 results, maximum 20 (Bing RSS may return fewer); no automatic retries or provider fallback.", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, bad := validateArgs(schema, req.Params.Arguments); bad != nil {
			return bad, nil
		}
		var in webSearchInput
		if json.Unmarshal(req.Params.Arguments, &in) != nil {
			return toolError("invalid_tool_arguments"), nil
		}
		return s.webSearch(ctx, in), nil
	})
}

func (s *Server) webSearch(ctx context.Context, in webSearchInput) *mcp.CallToolResult {
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return expectedError(400, "invalid_query", "search query must not be blank", nil, nil)
	}
	if in.Limit == 0 {
		in.Limit = 5
	}
	provider := s.Config.SearchProvider
	if provider == "" {
		provider = "duckduckgo"
	}
	target := s.Config.SearchURL
	query := map[string]any{"q": in.Query}
	headers := map[string]string{"Accept": "application/json"}
	switch provider {
	case "bing":
		if target == "" {
			target = "https://www.bing.com/search"
		}
		headers["Accept"] = "application/rss+xml, application/xml, text/xml"
		query["format"] = "rss"
		if in.Language != "" {
			query["setlang"] = in.Language
		}
	case "duckduckgo":
		if target == "" {
			target = "https://html.duckduckgo.com/html/"
		}
		headers["Accept"] = "text/html"
		if in.Language != "" {
			query["kl"] = in.Language
		}
	case "brave":
		if s.Config.SearchAPIKey == "" {
			return expectedError(503, "search_not_configured", "Brave search requires a deployment API key", nil, nil)
		}
		if target == "" {
			target = "https://api.search.brave.com/res/v1/web/search"
		}
		headers["X-Subscription-Token"] = s.Config.SearchAPIKey
		query["count"] = float64(in.Limit)
		if in.Language != "" {
			query["search_lang"] = in.Language
		}
	case "searxng":
		if target == "" {
			return expectedError(503, "search_not_configured", "SearXNG search requires a deployment search endpoint", nil, nil)
		}
		query["format"] = "json"
		query["categories"] = "general"
		if in.Language != "" {
			query["language"] = in.Language
		}
	default:
		return expectedError(503, "search_not_configured", "unsupported deployment search provider", nil, nil)
	}
	fetched := s.httpRequest(ctx, webInput{URL: target, Method: "GET", Query: query, Headers: headers, MaxBytes: maxWebBytes})
	if failedResult(fetched) {
		return fetched
	}
	m, ok := fetched.StructuredContent.(map[string]any)
	if !ok {
		return toolError("search_response_invalid")
	}
	if m["truncated"] == true || m["status"] != 200 {
		return result(map[string]any{"ok": false, "error": "search_response_incomplete", "provider": provider, "fetch": searchFetch(m)}, true)
	}
	var items []webSearchItem
	var err error
	if provider == "bing" {
		raw, _ := m["text"].(string)
		var doc struct {
			XMLName xml.Name `xml:"rss"`
			Channel *struct {
				Items []struct {
					Title   string `xml:"title"`
					URL     string `xml:"link"`
					Snippet string `xml:"description"`
				} `xml:"item"`
			} `xml:"channel"`
		}
		err = xml.Unmarshal([]byte(raw), &doc)
		if err == nil && doc.Channel == nil {
			err = fmt.Errorf("missing RSS channel")
		}
		if err == nil {
			for _, r := range doc.Channel.Items {
				items = append(items, webSearchItem{r.Title, r.URL, r.Snippet})
			}
		}
	} else if provider == "duckduckgo" {
		raw, _ := m["text"].(string)
		items, err = parseDuckDuckGo(raw)
	} else {
		raw, _ := json.Marshal(m["data"])
		if provider == "brave" {
			var doc struct {
				Web *struct {
					Results []struct{ Title, URL, Description string } `json:"results"`
				} `json:"web"`
			}
			err = json.Unmarshal(raw, &doc)
			if err == nil && doc.Web == nil {
				err = fmt.Errorf("missing web results")
			}
			if err == nil {
				for _, r := range doc.Web.Results {
					items = append(items, webSearchItem{r.Title, r.URL, r.Description})
				}
			}
		} else {
			var doc struct {
				Results      *[]struct{ Title, URL, Content string } `json:"results"`
				Unresponsive []json.RawMessage                       `json:"unresponsive_engines"`
			}
			err = json.Unmarshal(raw, &doc)
			if err == nil && (doc.Results == nil || len(doc.Unresponsive) > 0 && len(*doc.Results) == 0) {
				err = fmt.Errorf("missing or unavailable results")
			}
			if err == nil {
				for _, r := range *doc.Results {
					items = append(items, webSearchItem{r.Title, r.URL, r.Content})
				}
			}
		}
	}
	if err != nil {
		return result(map[string]any{"ok": false, "error": "search_response_invalid", "provider": provider, "fetch": searchFetch(m)}, true)
	}
	clean := []webSearchItem{}
	seen := map[string]bool{}
	for _, r := range items {
		u, err := url.Parse(r.URL)
		if err != nil || !validWebURL(u) || s.platformHost(u) || seen[r.URL] {
			continue
		}
		seen[r.URL] = true
		r.Title = limitSearchText(r.Title, 1000)
		r.Snippet = limitSearchText(r.Snippet, 4000)
		clean = append(clean, r)
		if len(clean) == in.Limit {
			break
		}
	}
	return result(map[string]any{"ok": true, "provider": provider, "q": in.Query, "items": clean, "fetch": searchFetch(m), "returned": len(clean)}, false)
}

func searchFetch(m map[string]any) map[string]any {
	return map[string]any{"url": m["url"], "fetched_at": m["fetched_at"], "content_hash": m["content_hash"], "complete": m["complete"]}
}

func limitSearchText(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func htmlClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, v := range strings.Fields(a.Val) {
				if v == class {
					return true
				}
			}
		}
	}
	return false
}
func htmlText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(htmlText(c))
		b.WriteByte(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func findClass(n *html.Node, class string) *html.Node {
	if htmlClass(n, class) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findClass(c, class); found != nil {
			return found
		}
	}
	return nil
}

func parseDuckDuckGo(raw string) ([]webSearchItem, error) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	items := []webSearchItem{}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if htmlClass(n, "result") {
			link := findClass(n, "result__a")
			if link != nil {
				var target string
				for _, a := range link.Attr {
					if a.Key == "href" {
						target = a.Val
					}
				}
				if strings.HasPrefix(target, "//") {
					target = "https:" + target
				}
				if u, e := url.Parse(target); e == nil && (u.Hostname() == "duckduckgo.com" || strings.HasSuffix(u.Hostname(), ".duckduckgo.com")) {
					if v := u.Query().Get("uddg"); v != "" {
						target = v
					}
				}
				snippet := ""
				if n := findClass(n, "result__snippet"); n != nil {
					snippet = htmlText(n)
				}
				items = append(items, webSearchItem{htmlText(link), target, snippet})
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	if len(items) == 0 && findClass(doc, "no-results") == nil && !strings.Contains(htmlText(doc), "No results found") {
		return nil, fmt.Errorf("search page unavailable or challenge")
	}
	return items, nil
}
