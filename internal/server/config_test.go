package server

import (
	"encoding/base64"
	"testing"
)

func TestObjectOriginsAreExplicitDeploymentTrust(t *testing.T) {
	t.Setenv("MCP_PUBLIC_URL", "https://example.com")
	t.Setenv("MCP_API_URL", "http://gateway")
	t.Setenv("MCP_AUTH_URL", "http://auth:8081")
	t.Setenv("MCP_DATABASE_URL", "test-only-dsn")
	t.Setenv("MCP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MCP_OBJECT_ORIGINS", "http://rustfs:9000,https://objects.example.com")
	c, err := LoadConfig()
	if err != nil || len(c.ObjectOrigins) != 2 {
		t.Fatalf("trusted internal object origin: %v %+v", err, c.ObjectOrigins)
	}
	for _, value := range []string{"http://user:password@rustfs:9000", "http://rustfs:9000/path", "http://rustfs:9000?redirect=x", "ftp://rustfs:9000"} {
		t.Setenv("MCP_OBJECT_ORIGINS", value)
		if _, err := LoadConfig(); err == nil {
			t.Fatalf("invalid object origin accepted: %s", value)
		}
	}
}

func TestSearchConfiguration(t *testing.T) {
	t.Setenv("MCP_PUBLIC_URL", "https://example.com")
	t.Setenv("MCP_API_URL", "http://gateway")
	t.Setenv("MCP_AUTH_URL", "http://auth:8081")
	t.Setenv("MCP_DATABASE_URL", "test-only-dsn")
	t.Setenv("MCP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MCP_SEARCH_PROVIDER", "")
	t.Setenv("MCP_SEARCH_URL", "")
	t.Setenv("MCP_SEARCH_API_KEY", "")
	if c, err := LoadConfig(); err != nil || c.SearchProvider != "duckduckgo" {
		t.Fatal(c.SearchProvider, err)
	}
	t.Setenv("MCP_SEARCH_PROVIDER", "bing")
	if c, err := LoadConfig(); err != nil || c.SearchURL != "https://www.bing.com/search" {
		t.Fatal(c.SearchURL, err)
	}
	t.Setenv("MCP_SEARCH_PROVIDER", "brave")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("Brave without a key accepted")
	}
	t.Setenv("MCP_SEARCH_API_KEY", "test-only")
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCP_SEARCH_PROVIDER", "searxng")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("SearXNG without an endpoint accepted")
	}
	for _, raw := range []string{"http://localhost/search", "http://10.0.0.1/search", "https://search.example:8443/search", "https://user:secret@search.example/search", "file:///search"} {
		t.Setenv("MCP_SEARCH_URL", raw)
		if _, err := LoadConfig(); err == nil {
			t.Fatal("unsafe search endpoint accepted")
		}
	}
	t.Setenv("MCP_SEARCH_URL", "https://search.example/search")
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
}
