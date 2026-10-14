package server

import (
	"testing"
)

func TestRequestedBusinessScopesRejectsImportSubmit(t *testing.T) {
	// No silent drop / no alias: retired catalog.import.submit is invalid_scope.
	if _, err := requestedBusinessScopes("openid mcp.read catalog.import.submit catalog.entity.edit"); err == nil {
		t.Fatal("mixed request containing catalog.import.submit must be invalid_scope")
	}
	if _, err := requestedBusinessScopes("catalog.import.submit"); err == nil {
		t.Fatal("import-only request must be invalid_scope")
	}
	got, err := requestedBusinessScopes("openid mcp.read catalog.entity.edit")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "mcp.read" || got[1] != "catalog.entity.edit" {
		t.Fatalf("got %v", got)
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
