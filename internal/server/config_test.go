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

func TestConcurrencyAndRateLimitConfiguration(t *testing.T) {
	t.Setenv("MCP_PUBLIC_URL", "https://example.com")
	t.Setenv("MCP_API_URL", "http://gateway")
	t.Setenv("MCP_AUTH_URL", "http://auth:8081")
	t.Setenv("MCP_DATABASE_URL", "test-only-dsn")
	t.Setenv("MCP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))

	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("default config failed: %v", err)
	}
	if !c.RateLimit.Configured || c.RateLimit.ReadRPM != 180 || c.RateLimit.WriteRPM != 180 || c.RateLimit.OAuthRPM != 180 {
		t.Fatalf("unexpected default rate limit: %+v", c.RateLimit)
	}
	if !c.FileConcurrencyConfigured || c.FileConcurrency != 64 || c.HTTPMaxIdleConns != 1024 || c.HTTPMaxIdleConnsPerHost != 256 || c.HTTPTimeoutSeconds != 45 {
		t.Fatalf("unexpected default concurrency: %+v", c)
	}

	t.Setenv("MCP_RATE_LIMIT_READ_RPM", "0")
	t.Setenv("MCP_RATE_LIMIT_WRITE_RPM", "1200")
	t.Setenv("MCP_RATE_LIMIT_OAUTH_RPM", "600")
	t.Setenv("MCP_FILE_CONCURRENCY", "0")
	t.Setenv("MCP_HTTP_MAX_IDLE_CONNS", "4096")
	t.Setenv("MCP_HTTP_MAX_IDLE_CONNS_PER_HOST", "512")
	t.Setenv("MCP_HTTP_TIMEOUT_SECONDS", "90")
	c, err = LoadConfig()
	if err != nil {
		t.Fatalf("high concurrency config failed: %v", err)
	}
	if c.RateLimit.ReadRPM != 0 || c.RateLimit.WriteRPM != 1200 || c.RateLimit.OAuthRPM != 600 || c.FileConcurrency != 0 || c.HTTPMaxIdleConns != 4096 || c.HTTPMaxIdleConnsPerHost != 512 || c.HTTPTimeoutSeconds != 90 {
		t.Fatalf("unexpected custom config: %+v", c)
	}

	for _, bad := range []struct{ k, v string }{
		{"MCP_RATE_LIMIT_READ_RPM", "-1"},
		{"MCP_RATE_LIMIT_WRITE_RPM", "abc"},
		{"MCP_FILE_CONCURRENCY", "-5"},
		{"MCP_HTTP_MAX_IDLE_CONNS", "0"},
		{"MCP_HTTP_MAX_IDLE_CONNS_PER_HOST", "0"},
		{"MCP_HTTP_TIMEOUT_SECONDS", "0"},
	} {
		t.Setenv(bad.k, bad.v)
		if _, err := LoadConfig(); err == nil {
			t.Fatalf("expected invalid %s=%s to fail", bad.k, bad.v)
		}
		t.Setenv(bad.k, "")
	}

	t.Setenv("MCP_DB_MAX_OPEN_CONNS", "64")
	t.Setenv("MCP_DB_MAX_IDLE_CONNS", "32")
	s, err := OpenStore("postgres://user:pass@127.0.0.1:5432/db?sslmode=disable")
	if err != nil {
		t.Fatalf("OpenStore valid pool failed: %v", err)
	}
	s.DB.Close()

	t.Setenv("MCP_DB_MAX_OPEN_CONNS", "0")
	if _, err := OpenStore("postgres://user:pass@127.0.0.1:5432/db?sslmode=disable"); err == nil {
		t.Fatal("expected invalid MCP_DB_MAX_OPEN_CONNS=0 to fail")
	}
	t.Setenv("MCP_DB_MAX_OPEN_CONNS", "32")
	t.Setenv("MCP_DB_MAX_IDLE_CONNS", "64")
	if _, err := OpenStore("postgres://user:pass@127.0.0.1:5432/db?sslmode=disable"); err == nil {
		t.Fatal("expected MCP_DB_MAX_IDLE_CONNS > MCP_DB_MAX_OPEN_CONNS to fail")
	}
}
