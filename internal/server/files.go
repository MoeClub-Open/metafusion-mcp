package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const maxFileBytes int64 = 1 << 30
const uploadPartSize int64 = 8 << 20

var errPrivateSourceAddress = errors.New("private_source_address")

func decodeArguments(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32", "::/96"} {
		if netip.MustParsePrefix(prefix).Contains(a) {
			return false
		}
	}
	return true
}
func publicClient() *http.Client {
	t := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, errors.New("no_address")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errPrivateSourceAddress
			}
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}}
	return &http.Client{Transport: t, Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || !validSourceURL(r.URL) {
			return errors.New("invalid_redirect")
		}
		return nil
	}}
}
func validSourceURL(u *url.URL) bool {
	return u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443")
}

type fileInput struct {
	SourceURL   string `json:"source_url"`
	Base64      string `json:"base64"`
	FileName    string `json:"file_name"`
	MIME        string `json:"mime_type"`
	TargetID    string `json:"target_entity_id"`
	BindingRole string `json:"binding_role"`
}

// Content is spooled in the service's private temp directory, never in the Agent's environment.
func loadFile(ctx context.Context, in fileInput) (*os.File, string, int64, error) {
	if (in.SourceURL == "") == (in.Base64 == "") {
		return nil, "", 0, errors.New("exactly_one_source_required")
	}
	var source io.ReadCloser
	if in.SourceURL != "" {
		u, e := url.Parse(in.SourceURL)
		if e != nil || !validSourceURL(u) {
			return nil, "", 0, errors.New("invalid_source_url")
		}
		r, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if e != nil {
			return nil, "", 0, e
		}
		client := publicClient()
		defer client.CloseIdleConnections()
		resp, e := client.Do(r)
		if e != nil {
			return nil, "", 0, errors.New("source_unavailable")
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, "", 0, errors.New("source_unavailable")
		}
		source = resp.Body
	} else {
		source = io.NopCloser(base64.NewDecoder(base64.StdEncoding, strings.NewReader(in.Base64)))
	}
	defer source.Close()
	f, e := os.CreateTemp("", "mf-mcp-upload-*")
	if e != nil {
		return nil, "", 0, e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(source, maxFileBytes+1))
	if e != nil || n > maxFileBytes {
		f.Close()
		os.Remove(f.Name())
		return nil, "", 0, errors.New("invalid_or_oversized_file")
	}
	_, e = f.Seek(0, 0)
	if e != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, "", 0, e
	}
	return f, hex.EncodeToString(h.Sum(nil)), n, nil
}
func dataObject(r *mcp.CallToolResult) (map[string]any, bool) {
	m, ok := r.StructuredContent.(map[string]any)
	if !ok || failedResult(r) {
		return nil, false
	}
	v, ok := m["data"].(map[string]any)
	return v, ok
}

func (s *Server) addFileTools() {
	props := map[string]any{}
	for _, k := range []string{"source_url", "base64", "file_name", "mime_type", "target_entity_id", "binding_role"} {
		props[k] = map[string]string{"type": "string"}
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_file_inspect", Description: "Compute exact SHA-256 and size of a public HTTPS file or base64 content in the cloud. Limit 1 GiB; base64 requests are bounded by the 8 MiB MCP envelope. No client runtime required.", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, ok := currentPrincipal(ctx)
		if !ok {
			return toolError("authentication_required"), nil
		}
		if !s.limiter.AllowAPI(p, "GET", "/mcp/files") {
			return toolError("rate_limited"), nil
		}
		select {
		case s.Files <- struct{}{}:
			defer func() { <-s.Files }()
		default:
			return toolError("file_workers_busy"), nil
		}
		var in fileInput
		if decodeArguments(r.Params.Arguments, &in) != nil {
			return toolError("invalid_arguments"), nil
		}
		f, digest, size, e := loadFile(ctx, in)
		if e != nil {
			return toolError(e.Error()), nil
		}
		defer os.Remove(f.Name())
		defer f.Close()
		return result(map[string]any{"sha256": digest, "size": size}, false), nil
	})
	s.MCP.AddTool(&mcp.Tool{Name: "metafusion_upload", Description: "Upload a public HTTPS file or base64 content entirely in the cloud: calculate hash/size, initiate, transfer parts and complete through the native storage API. Supports instant reuse and optional entity binding. Supply file_name. Limit 1 GiB. Never automatically retries writes; on failure retain asset_id and inspect/recover upstream before another upload.", InputSchema: schema}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in fileInput
		if decodeArguments(r.Params.Arguments, &in) != nil || strings.TrimSpace(in.FileName) == "" {
			return toolError("invalid_arguments"), nil
		}
		return s.upload(ctx, in), nil
	})
}
func (s *Server) upload(ctx context.Context, in fileInput) *mcp.CallToolResult {
	p, ok := currentPrincipal(ctx)
	if !ok {
		return toolError("authentication_required")
	}
	if !slices.Contains(p.Grant.Scopes, "storage.asset.upload") && !slices.Contains(p.Grant.Scopes, "*") {
		return toolError("insufficient_scope")
	}
	select {
	case s.Files <- struct{}{}:
		defer func() { <-s.Files }()
	default:
		return toolError("file_workers_busy")
	}
	f, digest, size, e := loadFile(ctx, in)
	if e != nil {
		return toolError(e.Error())
	}
	defer os.Remove(f.Name())
	defer f.Close()
	parts := max(1, int((size+uploadPartSize-1)/uploadPartSize))
	initial := s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/storage/upload/initiate", Body: map[string]any{"file_name": in.FileName, "file_size": size, "sha256_hash": digest, "mime_type": in.MIME, "part_count": parts, "target_entity_id": in.TargetID, "binding_role": in.BindingRole}})
	data, ok := dataObject(initial)
	if !ok {
		return result(map[string]any{"ok": false, "sha256": digest, "size": size, "response": initial.StructuredContent, "retry_automatically": false}, initial.IsError)
	}
	id, _ := data["asset_id"].(string)
	if instant, _ := data["is_instant_upload"].(bool); instant {
		return initial
	}
	failed := func() *mcp.CallToolResult {
		return result(map[string]any{"error": "upload_transfer_failed", "asset_id": id, "sha256": digest, "size": size, "outcome": "unknown", "retry_automatically": false}, true)
	}
	completed := []map[string]any{}
	if direct, _ := data["direct_upload_url"].(string); direct != "" {
		safe, e := safeAPIPath(direct)
		if e != nil || safe != "/api/storage/upload/stream/"+id {
			return failed()
		}
		if !s.limiter.AllowAPI(p, "PUT", direct) {
			return failed()
		}
		req, e := http.NewRequestWithContext(ctx, "PUT", s.Config.APIURL+safe, f)
		if e != nil {
			return failed()
		}
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Authorization", "Bearer "+p.PAT)
		resp, e := s.HTTP.Do(req)
		if e != nil {
			return failed()
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return failed()
		}
	} else {
		urls, ok := data["presigned_urls"].([]any)
		if !ok || len(urls) != parts {
			return failed()
		}
		for n, v := range urls {
			target, ok := v.(string)
			if !ok {
				return failed()
			}
			u, e := url.Parse(target)
			if e != nil || u.User != nil || u.Fragment != "" || !slices.Contains(s.Config.ObjectOrigins, u.Scheme+"://"+u.Host) {
				return failed()
			}
			length := min(uploadPartSize, size-int64(n)*uploadPartSize)
			req, e := http.NewRequestWithContext(ctx, "PUT", u.String(), io.NewSectionReader(f, int64(n)*uploadPartSize, length))
			if e != nil {
				return failed()
			}
			req.ContentLength = length
			// Presigned object requests never contain the PAT, cookie, or MCP bearer token.
			resp, e := s.HTTP.Do(req)
			if e != nil {
				return failed()
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if resp.StatusCode >= 400 || resp.Header.Get("ETag") == "" {
				return failed()
			}
			completed = append(completed, map[string]any{"part_number": n + 1, "etag": resp.Header.Get("ETag")})
		}
	}
	out := s.callAPI(ctx, APIInput{Method: "POST", Path: "/api/storage/upload/complete", Body: map[string]any{"asset_id": id, "upload_id": data["upload_id"], "parts": completed}})
	if failedResult(out) {
		return result(map[string]any{"ok": false, "asset_id": id, "sha256": digest, "response": out.StructuredContent, "retry_automatically": false}, out.IsError)
	}
	return result(map[string]any{"ok": true, "asset_id": id, "sha256": digest, "size": size, "response": out.StructuredContent}, false)
}
