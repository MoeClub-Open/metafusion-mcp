package server

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestPostgresArchivedProposalExportOwnerIsolation(t *testing.T) {
	s := isolatedMigrationStore(t)
	execMigrationFixture(t, s, legacyDraftSchema)
	execMigrationFixture(t, s, `INSERT INTO mcp.drafts(id,user_id,payload,expires_at) VALUES
('00000000-0000-0000-0000-000000000001','alice','{"edit_note":"unfinished alice","operations":[]}','2020-01-01'),
('00000000-0000-0000-0000-000000000002','bob','{"edit_note":"private bob"}','2020-01-01')`)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	n, err := s.ExportArchivedProposals(context.Background(), "alice", &out)
	if err != nil || n != 1 {
		t.Fatal("owner export failed", n, err)
	}
	var proposal map[string]any
	if err = json.Unmarshal(out.Bytes(), &proposal); err != nil {
		t.Fatal(err)
	}
	payload, _ := proposal["payload"].(map[string]any)
	if proposal["owner_id"] != "alice" || payload["edit_note"] != "unfinished alice" || bytes.Contains(out.Bytes(), []byte("private bob")) {
		t.Fatal("export lost proposal or exposed another owner's data")
	}
	if _, err = s.ExportArchivedProposals(context.Background(), " ", &out); err == nil {
		t.Fatal("unscoped archive export accepted")
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM mcp.archived_commit_proposals").Scan(&count); err != nil || count != 2 {
		t.Fatal("export modified archive", count, err)
	}
}

func TestPostgresArchivedProposalExportFreshInstallation(t *testing.T) {
	s := isolatedMigrationStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if n, err := s.ExportArchivedProposals(context.Background(), "alice", &out); err != nil || n != 0 || out.Len() != 0 {
		t.Fatal("fresh installation archive export failed", n, err)
	}
}
