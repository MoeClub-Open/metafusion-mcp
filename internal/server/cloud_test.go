package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestUserReadWriteBudgetsAndAdmin(t *testing.T) {
	l := newLimiter()
	p := Principal{Grant: Grant{UserID: "one"}}
	for i := 0; i < 180; i++ {
		if !l.AllowAPI(p, "GET", "/api/catalog/entities") {
			t.Fatal("read budget below 180")
		}
	}
	if l.AllowAPI(p, "GET", "/api/catalog/entities/other") {
		t.Fatal("read budget was split by path")
	}
	for i := 0; i < 180; i++ {
		if !l.AllowAPI(p, "POST", "/api/catalog/entities") {
			t.Fatal("write budget shared with read")
		}
	}
	if l.AllowAPI(p, "POST", "/api/catalog/relations") {
		t.Fatal("write budget exceeded")
	}
	p.Grant.UserID = "two"
	if !l.AllowAPI(p, "GET", "/api/catalog/entities") {
		t.Fatal("different user shared budget")
	}
	p.Grant.UserID = "one"
	p.Groups = []string{"admin"}
	for i := 0; i < 1000; i++ {
		if !l.AllowAPI(p, "POST", "/api/catalog/entities") {
			t.Fatal("admin rate limited")
		}
	}
}

func TestPublicSourceAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "169.254.169.254", "10.0.0.1", "172.16.0.1", "192.168.0.1", "100.64.0.1", "::ffff:127.0.0.1", "198.18.0.1", "2001:db8::1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Errorf("private/reserved address accepted %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(raw)) {
			t.Errorf("public address rejected %s", raw)
		}
	}
	_, _, _, e := loadFile(context.Background(), fileInput{SourceURL: "https://127.0.0.1/a"})
	if e == nil {
		t.Fatal("source reached loopback")
	}
	f, digest, n, e := loadFile(context.Background(), fileInput{Base64: base64.StdEncoding.EncodeToString([]byte("hello"))})
	if e != nil {
		t.Fatal(e)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if n != 5 || digest != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatal("incorrect file digest")
	}
}

func TestCloudUploadNativeAPIAndNoCredentialToObjects(t *testing.T) {
	calls := []string{}
	object := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credential leaked to object store")
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != "hello" {
			t.Error("file changed")
		}
		w.Header().Set("ETag", "\"part\"")
		w.WriteHeader(200)
	}))
	defer object.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer mfp_upload" {
			t.Error("native permission identity lost")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/storage/upload/initiate":
			json.NewEncoder(w).Encode(map[string]any{"asset_id": "asset", "upload_id": "upload", "presigned_urls": []string{object.URL + "/object?signature=example"}})
		case "/api/storage/upload/complete":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			parts := body["parts"].([]any)
			if len(parts) != 1 || body["asset_id"] != "asset" {
				t.Error("incorrect completion")
			}
			io.WriteString(w, `{"asset":{"status":"complete"}}`)
		default:
			t.Error("unexpected operation")
		}
	}))
	defer upstream.Close()
	cfg := testConfig(upstream.URL)
	cfg.ObjectOrigins = []string{object.URL}
	s := New(cfg, nil)
	r := s.upload(principalContext(Principal{PAT: "mfp_upload", Grant: Grant{UserID: "owner", Scopes: []string{"storage.asset.upload"}}}), fileInput{FileName: "hello.txt", Base64: base64.StdEncoding.EncodeToString([]byte("hello"))})
	if r.IsError || len(calls) != 2 {
		t.Fatal("upload failed/replayed", r.StructuredContent, calls)
	}
	cfg.ObjectOrigins = nil
	s = New(cfg, nil)
	r = s.upload(principalContext(Principal{PAT: "mfp_upload", Grant: Grant{UserID: "owner", Scopes: []string{"storage.asset.upload"}}}), fileInput{FileName: "hello.txt", Base64: base64.StdEncoding.EncodeToString([]byte("hello"))})
	if !r.IsError || len(calls) != 3 {
		t.Fatal("unconfigured object origin accepted")
	}
}

func TestCloudCommitDraftOwnershipAndImmutablePush(t *testing.T) {
	store := testStore(t)
	var payload string
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		payload = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"applied":true}`)
	}))
	defer up.Close()
	s := New(testConfig(up.URL), store)
	ctx := principalContext(Principal{PAT: "mfp_edit", Grant: Grant{UserID: "draft-owner", Scopes: []string{"catalog.entity.edit"}}})
	a, b := mcp.NewInMemoryTransports()
	ss, e := s.MCP.Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "cloud", Version: "1"}, nil)
	cs, e := client.Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	draft, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit_prepare", Arguments: map[string]any{"definitions_etag": "etag", "edit_note": "evidence checked by Agent", "sources": []map[string]string{{"kind": "url", "url": "https://example.com", "citation": "Example"}}, "operations": []map[string]any{{"target": "entity", "action": "update", "id": "11111111-2222-4333-8444-555555555555", "base_version": 2, "patch": []map[string]string{{"path": "/title", "value": "edited"}}}}}})
	if e != nil || draft.IsError {
		t.Fatal("cloud prepare failed", e, draft)
	}
	var saved map[string]any
	json.Unmarshal([]byte(draft.Content[0].(*mcp.TextContent).Text), &saved)
	id := saved["id"].(string)
	t.Cleanup(func() { store.DB.Exec("DELETE FROM mcp.drafts WHERE id=$1", id) })
	preview, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit", Arguments: map[string]string{"id": id, "action": "preview"}})
	if e != nil || preview.IsError {
		t.Fatal("cloud preview failed", e, preview)
	}
	sealed := payload
	pushed, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "metafusion_commit", Arguments: map[string]string{"id": id, "action": "push"}})
	if e != nil || pushed.IsError || payload != sealed || calls != 2 {
		t.Fatal("cloud changed/replayed commit", e)
	}
	var n int
	store.DB.QueryRow("SELECT count(*) FROM mcp.drafts WHERE id=$1 AND user_id='someone-else'", id).Scan(&n)
	if n != 0 {
		t.Fatal("draft visible to another owner")
	}
	if !strings.Contains(payload, id) {
		t.Fatal("generated id lost")
	}
}
