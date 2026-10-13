package server

import (
	"testing"
)

func TestRequestedBusinessScopesDropsImportSubmit(t *testing.T) {
	got, err := requestedBusinessScopes("openid mcp.read catalog.import.submit catalog.entity.edit")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s == "catalog.import.submit" {
			t.Fatal("import submit must be dropped silently")
		}
	}
	if len(got) != 2 || got[0] != "mcp.read" || got[1] != "catalog.entity.edit" {
		t.Fatalf("got %v", got)
	}
	if _, err := requestedBusinessScopes("catalog.import.submit"); err == nil {
		t.Fatal("import-only request must fail as invalid_scope (no remaining business scopes)")
	}
}

func TestSupportedScopesOmitImportSubmit(t *testing.T) {
	for _, s := range supportedScopes {
		if s == "catalog.import.submit" {
			t.Fatal("catalog.import.submit must not remain in supportedScopes")
		}
	}
}

func TestReadOperationExternalSourceQuery(t *testing.T) {
	if !readOperation("POST", "/api/external-sources/query") {
		t.Fatal("POST /api/external-sources/query must be read-only for mcp.read grants")
	}
	if readOperation("PUT", "/api/external-sources/query") {
		t.Fatal("PUT must not be treated as read")
	}
	// Same allowlist family as other read-only POSTs
	for _, p := range []string{"/api/catalog/entities/candidates/batch", "/api/catalog/quality/batch"} {
		if !readOperation("POST", p) {
			t.Fatalf("missing allowlist entry %s", p)
		}
	}
}

