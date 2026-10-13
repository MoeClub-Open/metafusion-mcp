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
