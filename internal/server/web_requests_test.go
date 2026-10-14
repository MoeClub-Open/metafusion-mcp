package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type webRoundTrip func(*http.Request) (*http.Response, error)

func (f webRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func webResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func webContext(scopes ...string) context.Context {
	return principalContext(Principal{PAT: "platform-secret-must-not-leak", Grant: Grant{UserID: "u", Scopes: scopes}})
}

func TestHTTPToolThroughSDK(t *testing.T) {
	s := New(testConfig("https://platform.example"), nil)
	var calls int
	s.WebHTTP.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer explicit-third-party" || r.Header.Get("Cookie") != "" || r.Method != "POST" {
			t.Fatal("credential or method mismatch", r.Header, r.Method)
		}
		if r.URL.Query().Get("existing") != "1" || r.URL.Query().Get("q") != "二次検索" || r.URL.Query()["tag"][1] != "b" {
			t.Fatal(r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"name":"作品"}` || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal(string(b), r.Header)
		}
		resp := webResponse(201, `{"id":"x"}`)
		resp.Header.Set("Set-Cookie", "secret")
		resp.Header.Set("X-API-Key", "secret")
		resp.Header.Set("ETag", "v1")
		return resp, nil
	})
	ctx := webContext("mcp.read", "mcp.web.write")
	cs := connect(t, s, ctx)
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_http_request", Arguments: map[string]any{"url": "https://external.example/items?existing=1", "method": "post", "query": map[string]any{"q": "二次検索", "tag": []any{"a", "b"}}, "headers": map[string]string{"Authorization": "Bearer explicit-third-party"}, "body": map[string]any{"name": "作品"}}})
	if err != nil {
		t.Fatal(err)
	}
	m := structured(t, r)
	h := m["headers"].(map[string]any)
	if m["status"] != float64(201) || m["complete"] != true || m["data"].(map[string]any)["id"] != "x" || h["Set-Cookie"] != nil || h["X-API-Key"] != nil || calls != 1 {
		t.Fatal(m, calls)
	}
	d := sha256.Sum256([]byte(`{"id":"x"}`))
	if m["content_hash"] != hex.EncodeToString(d[:]) || m["fetched_at"] == "" {
		t.Fatal(m)
	}
}

func TestWebRequestsEnforceScopesAndURLBoundaries(t *testing.T) {
	s := New(testConfig("https://platform.example"), nil)
	s.WebHTTP.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		t.Fatal("blocked request reached network", r.URL)
		return nil, nil
	})
	for _, tc := range []struct {
		ctx  context.Context
		in   webInput
		code string
	}{
		{context.Background(), webInput{URL: "https://external.example"}, "authentication_required"},
		{webContext("catalog.entity.edit"), webInput{URL: "https://external.example"}, "insufficient_scope"},
		{webContext("mcp.read"), webInput{URL: "https://external.example", Method: "POST"}, "insufficient_scope"},
		{webContext("mcp.web.write"), webInput{URL: "https://external.example"}, "insufficient_scope"},
		{webContext("mcp.read"), webInput{URL: "https://platform.example/api/catalog/entities"}, "platform_url_requires_api_call"},
		{webContext("mcp.read"), webInput{URL: "https://127.0.0.1"}, "invalid_external_url"},
		{webContext("mcp.read"), webInput{URL: "https://external.example", Headers: map[string]string{"Host": "private"}}, "header_not_allowed"},
		{webContext("mcp.read"), webInput{URL: "https://external.example", Headers: map[string]string{"X-A": "a\r\nb"}}, "header_not_allowed"},
		{webContext("mcp.read"), webInput{URL: "https://external.example", Headers: map[string]string{"Cookie": "session"}}, "header_not_allowed"},
	} {
		m := structured(t, s.httpRequest(tc.ctx, tc.in))
		if m["error"] != tc.code {
			t.Fatal(tc.in, m)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "ftp://external.example", "http://localhost", "http://169.254.169.254", "https://10.0.0.1", "http://[::1]", "https://[::ffff:127.0.0.1]", "http://100.64.0.1", "https://example.com:8443", "https://user:secret@example.com", "https://example.com/#fragment", "https://example.com\\@127.0.0.1"} {
		u, _ := url.Parse(raw)
		if validWebURL(u) {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	client := publicClient()
	defer client.CloseIdleConnections()
	dial := client.Transport.(*http.Transport).DialContext
	if conn, err := dial(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		conn.Close()
		t.Fatal("private address reached dial")
	}
}

func TestHTTPRequestsPreserveRedirectsTruncationAndBinary(t *testing.T) {
	s := New(testConfig("https://platform.example"), nil)
	var calls int
	s.WebHTTP.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Fatal("platform PAT leaked")
		}
		switch r.URL.Path {
		case "/redirect":
			resp := webResponse(302, "")
			resp.Header.Set("Location", "http://127.0.0.1")
			return resp, nil
		case "/binary":
			return webResponse(200, string([]byte{0xff, 0x00})), nil
		case "/error":
			return webResponse(429, `{"message":"busy"}`), nil
		default:
			return webResponse(200, "abcdef"), nil
		}
	})
	ctx := webContext("mcp.read")
	m := structured(t, s.httpRequest(ctx, webInput{URL: "https://external.example/redirect"}))
	if m["status"] != float64(302) || calls != 1 || m["headers"].(map[string]any)["Location"] != "http://127.0.0.1" {
		t.Fatal(m, calls)
	}
	m = structured(t, s.httpRequest(ctx, webInput{URL: "https://external.example/text", MaxBytes: 3}))
	if m["text"] != "abc" || m["truncated"] != true || m["complete"] != false || m["hash_scope"] != "returned_bytes" {
		t.Fatal(m)
	}
	m = structured(t, s.httpRequest(ctx, webInput{URL: "https://external.example/binary"}))
	if m["base64"] != base64.StdEncoding.EncodeToString([]byte{0xff, 0x00}) {
		t.Fatal(m)
	}
	r := s.httpRequest(ctx, webInput{URL: "https://external.example/error"})
	m = structured(t, r)
	if !r.IsError || m["error"] != "external_http_error" || m["status"] != float64(429) {
		t.Fatal(m)
	}
}

func TestHTTPWriteBodiesAndUnknownOutcomeNeverReplay(t *testing.T) {
	s := New(testConfig("https://platform.example"), nil)
	if !s.WebHTTP.Transport.(*http.Transport).DisableKeepAlives {
		t.Fatal("pooled connections permit implicit transport retries")
	}
	var calls int
	s.WebHTTP.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.GetBody != nil {
			t.Fatal("write body permits automatic replay")
		}
		return nil, errors.New("request outcome unknown")
	})
	ctx := webContext("mcp.web.write")
	txt := "text"
	b64 := "dGV4dA=="
	invalid := "not base64"
	for _, in := range []webInput{{URL: "https://external.example", Method: "PUT", BodyText: &txt, BodyBase64: &b64}, {URL: "https://external.example", Method: "PATCH", BodyBase64: &invalid}, {URL: "https://external.example", Method: "DELETE", BodyText: &txt, Body: map[string]any{"x": 1}}} {
		if m := structured(t, s.httpRequest(ctx, in)); m["error"] != "invalid_body" {
			t.Fatal(m)
		}
	}
	for _, in := range []webInput{{URL: "https://external.example", Method: "PUT", BodyText: &txt}, {URL: "https://external.example", Method: "PATCH", BodyBase64: &b64}, {URL: "https://external.example", Method: "DELETE"}} {
		m := structured(t, s.httpRequest(ctx, in))
		if m["outcome"] != "unknown" || m["retry_automatically"] != false {
			t.Fatal(m)
		}
	}
	if calls != 3 {
		t.Fatal("write was replayed", calls)
	}
}

func TestHTTPNullJSONBodyAndSchemaLimits(t *testing.T) {
	s := New(testConfig("https://platform.example"), nil)
	var calls int
	s.WebHTTP.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if string(body) != "null" {
			t.Fatal("JSON null body was lost", string(body))
		}
		return webResponse(200, "null"), nil
	})
	ctx := webContext("mcp.read", "mcp.web.write")
	cs := connect(t, s, ctx)
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_http_request", Arguments: map[string]any{"url": "https://external.example", "method": "POST", "body": nil}})
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	for _, args := range []map[string]any{
		{"url": "https://external.example", "max_bytes": maxWebBytes + 1},
		{"url": "https://external.example", "timeout_seconds": 61},
		{"url": "https://external.example", "follow_redirects": true},
	} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_http_request", Arguments: args})
		if err != nil || structured(t, r)["error"] != "invalid_tool_arguments" {
			t.Fatal(r, err)
		}
	}
	if calls != 1 {
		t.Fatal("invalid arguments reached network", calls)
	}
}

func TestWebWriteScopeNeverEntersPlatformPATOrAuthorizesAPIWrites(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Scopes []string `json:"scopes"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if !reflect.DeepEqual(in.Scopes, []string{"mcp.connect"}) {
			t.Fatal(in.Scopes)
		}
		writeJSON(w, 201, map[string]any{"token": "test-token", "item": map[string]string{"id": "test-id"}})
	}))
	defer up.Close()
	s := New(testConfig(up.URL), nil)
	if _, _, err := s.mintPAT(context.Background(), "test-cookie", "test-client", []string{"mcp.read", "mcp.web.write"}); err != nil {
		t.Fatal(err)
	}
	m := structured(t, s.callAPI(webContext("mcp.read", "mcp.web.write"), APIInput{Method: "POST", Path: "/api/catalog/entities", Body: map[string]any{}}))
	if m["error"] != "insufficient_scope" {
		t.Fatal("web scope widened platform writes", m)
	}
	if slices.Contains(defaultScopes, "mcp.web.write") {
		t.Fatal("external writes must be explicitly requested")
	}
}
