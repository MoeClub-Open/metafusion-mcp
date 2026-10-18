package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Migration fixtures must never replace the mcp schema of the supplied database.
// Each test creates and drops its own random database through that connection.
func isolatedMigrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MCP_TEST_DATABASE_URL required for isolated PostgreSQL migration tests")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("open migration test database connection:", err)
	}
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = admin.PingContext(ctx); err != nil {
		t.Fatal("connect to migration test database:", err)
	}
	var token [12]byte
	if _, err = rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	name := "mcp_migration_test_" + hex.EncodeToString(token[:])
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "42501" {
			t.Skip("migration tests need CREATEDB permission; supplied database remains untouched")
		}
		t.Fatal("create isolated migration database:", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE "+pq.QuoteIdentifier(name)); err != nil {
			t.Error("drop isolated migration database:", err)
		}
	})
	isolatedDSN := dsn + " dbname='" + name + "'"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("parse migration test connection URL")
		}
		u.Path = "/" + name
		u.RawPath = ""
		q := u.Query()
		q.Del("dbname")
		u.RawQuery = q.Encode()
		isolatedDSN = u.String()
	}
	s, err := OpenStore(isolatedDSN)
	if err != nil {
		t.Fatal("open isolated migration database:", err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func execMigrationFixture(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal("migration fixture:", err)
	}
}

func migrationTableExists(t *testing.T, s *Store, name string) bool {
	t.Helper()
	var exists bool
	if err := s.DB.QueryRowContext(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

const legacyDraftSchema = `CREATE SCHEMA mcp;
CREATE TABLE mcp.schema_version (version integer PRIMARY KEY CHECK(version=1));
INSERT INTO mcp.schema_version VALUES(1);
CREATE TABLE mcp.drafts (
 id uuid PRIMARY KEY, user_id text NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 days'
);
CREATE INDEX drafts_owner ON mcp.drafts(user_id, created_at DESC);`

func TestPostgresMigrationFreshAndIdempotent(t *testing.T) {
	s := isolatedMigrationStore(t)
	for i := 0; i < 2; i++ {
		if err := s.Migrate(context.Background()); err != nil {
			t.Fatal("migrate fresh schema or repeat migration:", err)
		}
		if err := s.Check(context.Background()); err != nil {
			t.Fatal("check schema 2:", err)
		}
	}
	if migrationTableExists(t, s, "mcp.drafts") {
		t.Fatal("fresh schema created retired draft storage")
	}
	if migrationTableExists(t, s, "mcp.archived_commit_proposals") {
		t.Fatal("fresh schema created an unnecessary archive")
	}
	var count, version int
	if err := s.DB.QueryRow("SELECT count(*), min(version) FROM mcp.schema_version").Scan(&count, &version); err != nil {
		t.Fatal(err)
	}
	if count != 1 || version != 2 {
		t.Fatalf("unexpected schema version rows: %d, version %d", count, version)
	}
}

func TestPostgresMigrationArchivesAllLegacyProposals(t *testing.T) {
	s := isolatedMigrationStore(t)
	execMigrationFixture(t, s, legacyDraftSchema)
	created := time.Date(2024, 1, 1, 12, 13, 14, 0, time.UTC)
	expired := time.Date(2024, 2, 1, 12, 13, 14, 0, time.UTC)
	ids := []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"}
	owners := []string{"alice", "bob"}
	payloads := []string{`{"request":{"message":"expired work","operations":[{"type":"create_entity"}]},"sealed":true}`, `{"request":{"message":"unfinished work"},"sealed":true}`}
	deadlines := []time.Time{expired, time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)}
	for i := range ids {
		execMigrationFixture(t, s, "INSERT INTO mcp.drafts(id,user_id,payload,created_at,expires_at) VALUES($1,$2,$3,$4,$5)", ids[i], owners[i], payloads[i], created, deadlines[i])
	}
	if err := s.Check(context.Background()); err == nil {
		t.Fatal("startup accepted the legacy schema")
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if migrationTableExists(t, s, "mcp.drafts") {
		t.Fatal("legacy draft table still exists")
	}
	var archivedAt time.Time
	for i := range ids {
		var owner string
		var payload []byte
		var gotCreated, gotExpires, archived time.Time
		if err := s.DB.QueryRow(`SELECT user_id,payload,created_at,expires_at,archived_at FROM mcp.archived_commit_proposals WHERE id=$1`, ids[i]).Scan(&owner, &payload, &gotCreated, &gotExpires, &archived); err != nil {
			t.Fatal(err)
		}
		var want, got any
		if json.Unmarshal([]byte(payloads[i]), &want) != nil || json.Unmarshal(payload, &got) != nil || !reflect.DeepEqual(want, got) {
			t.Fatal("legacy proposal payload changed during archival")
		}
		if owner != owners[i] || !gotCreated.Equal(created) || !gotExpires.Equal(deadlines[i]) || archived.IsZero() {
			t.Fatal("legacy proposal ownership or timestamps changed")
		}
		archivedAt = archived
	}
	for _, query := range []string{
		`INSERT INTO mcp.archived_commit_proposals(id,user_id,payload) VALUES('00000000-0000-0000-0000-000000000003','alice','{}')`,
		`UPDATE mcp.archived_commit_proposals SET payload='{}'`,
		`DELETE FROM mcp.archived_commit_proposals`,
		`TRUNCATE mcp.archived_commit_proposals`,
	} {
		if _, err := s.DB.Exec(query); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("archive write was not rejected as read-only: %v", err)
		}
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal("repeat archive migration:", err)
	}
	var count int
	var finalArchivedAt time.Time
	if err := s.DB.QueryRow(`SELECT count(*), min(archived_at) FROM mcp.archived_commit_proposals`).Scan(&count, &finalArchivedAt); err != nil {
		t.Fatal(err)
	}
	if count != len(ids) || !finalArchivedAt.Equal(archivedAt) {
		t.Fatal("repeat migration modified archived records")
	}
}

func TestPostgresMigrationArchiveCollisionRollsBack(t *testing.T) {
	s := isolatedMigrationStore(t)
	execMigrationFixture(t, s, legacyDraftSchema)
	execMigrationFixture(t, s, `INSERT INTO mcp.drafts(id,user_id,payload) VALUES('00000000-0000-0000-0000-000000000001','alice','{"original":true}'); CREATE TABLE mcp.archived_commit_proposals(marker text); INSERT INTO mcp.archived_commit_proposals VALUES('existing archive')`)
	if err := s.Migrate(context.Background()); err == nil {
		t.Fatal("migration overwrote an existing archive")
	}
	var version int
	if err := s.DB.QueryRow("SELECT version FROM mcp.schema_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration advanced schema version: %d, %v", version, err)
	}
	if !migrationTableExists(t, s, "mcp.drafts") || migrationTableExists(t, s, "mcp.clients") {
		t.Fatal("failed migration left partial schema changes")
	}
	var marker string
	if err := s.DB.QueryRow("SELECT marker FROM mcp.archived_commit_proposals").Scan(&marker); err != nil || marker != "existing archive" {
		t.Fatal("failed migration changed existing archive")
	}
}

func TestPostgresMigrationLateFailureRestoresLegacyProposals(t *testing.T) {
	s := isolatedMigrationStore(t)
	execMigrationFixture(t, s, legacyDraftSchema)
	execMigrationFixture(t, s, `ALTER TABLE mcp.schema_version ADD CONSTRAINT fixture_version_lock CHECK(version=1);
INSERT INTO mcp.drafts(id,user_id,payload) VALUES('00000000-0000-0000-0000-000000000001','alice','{"unfinished":true}')`)
	if err := s.Migrate(context.Background()); err == nil {
		t.Fatal("migration ignored the deliberate final version constraint failure")
	}
	if !migrationTableExists(t, s, "mcp.drafts") || migrationTableExists(t, s, "mcp.archived_commit_proposals") {
		t.Fatal("failed migration did not restore the original proposal table")
	}
	var owner string
	var payload []byte
	if err := s.DB.QueryRow("SELECT user_id,payload FROM mcp.drafts").Scan(&owner, &payload); err != nil {
		t.Fatal("original proposal was lost:", err)
	}
	var got map[string]bool
	if owner != "alice" || json.Unmarshal(payload, &got) != nil || !got["unfinished"] {
		t.Fatal("failed migration modified the original proposal")
	}
	var version int
	if err := s.DB.QueryRow("SELECT version FROM mcp.schema_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("failed migration did not restore schema version 1")
	}
}

func TestPostgresMigrationRejectsUnknownVersion(t *testing.T) {
	s := isolatedMigrationStore(t)
	execMigrationFixture(t, s, "CREATE SCHEMA mcp; CREATE TABLE mcp.schema_version(version integer PRIMARY KEY); INSERT INTO mcp.schema_version VALUES(3)")
	if err := s.Migrate(context.Background()); err == nil {
		t.Fatal("migration accepted an unknown schema version")
	}
	if err := s.Check(context.Background()); err == nil {
		t.Fatal("startup accepted an unknown schema version")
	}
	if migrationTableExists(t, s, "mcp.clients") {
		t.Fatal("unknown-version migration changed schema")
	}
}

func TestPostgresSchemaCheckReadOnlyAndRetiredTable(t *testing.T) {
	s := isolatedMigrationStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.DB.SetMaxOpenConns(1)
	execMigrationFixture(t, s, "SET default_transaction_read_only = on")
	if err := s.Check(context.Background()); err != nil {
		t.Fatal("startup check requires database writes:", err)
	}
	execMigrationFixture(t, s, "SET default_transaction_read_only = off")
	execMigrationFixture(t, s, "CREATE TABLE mcp.drafts(id uuid)")
	execMigrationFixture(t, s, "SET default_transaction_read_only = on")
	if err := s.Check(context.Background()); err == nil {
		t.Fatal("startup accepted the retired table beside schema 2")
	}
	if !migrationTableExists(t, s, "mcp.drafts") {
		t.Fatal("startup check removed the retired table")
	}
}
