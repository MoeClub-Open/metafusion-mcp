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
