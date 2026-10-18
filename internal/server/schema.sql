CREATE SCHEMA IF NOT EXISTS mcp;
CREATE TABLE IF NOT EXISTS mcp.schema_version (version integer PRIMARY KEY);
DO $$
DECLARE
  version_count bigint;
  current_version integer;
BEGIN
  SELECT count(*), min(version) INTO version_count, current_version FROM mcp.schema_version;
  IF version_count > 1 OR (version_count = 1 AND current_version NOT IN (1, 2)) THEN
    RAISE EXCEPTION 'unsupported MCP schema version';
  END IF;
END;
$$;
CREATE TABLE IF NOT EXISTS mcp.clients (
 id text PRIMARY KEY, name text NOT NULL, redirects jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mcp.grants (
 id text PRIMARY KEY, user_id text NOT NULL, client_id text NOT NULL REFERENCES mcp.clients(id),
 scopes jsonb NOT NULL, pat_id text NOT NULL DEFAULT '', pat_cipher bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz,
 revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS grants_user ON mcp.grants(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS mcp.codes (
 hash text PRIMARY KEY, grant_id text NOT NULL REFERENCES mcp.grants(id),
 redirect_uri text NOT NULL, challenge text NOT NULL, expires_at timestamptz NOT NULL,
 used_at timestamptz
);
CREATE TABLE IF NOT EXISTS mcp.tokens (
 access_hash text PRIMARY KEY, refresh_hash text UNIQUE NOT NULL,
 grant_id text NOT NULL REFERENCES mcp.grants(id), access_expires_at timestamptz NOT NULL,
 refresh_expires_at timestamptz NOT NULL, revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS tokens_grant ON mcp.tokens(grant_id);
CREATE TABLE IF NOT EXISTS mcp.audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, user_id text NOT NULL,
 grant_id text NOT NULL, action text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
-- Preserve every legacy proposal, including expired ones. The archive is only
-- for explicit owner-scoped export; no runtime draft interface remains.
DO $$
BEGIN
  IF to_regclass('mcp.drafts') IS NOT NULL THEN
    IF to_regclass('mcp.archived_commit_proposals') IS NOT NULL THEN
      RAISE EXCEPTION 'both legacy drafts and archived commit proposals exist; refusing to overwrite either';
    END IF;
    ALTER TABLE mcp.drafts RENAME TO archived_commit_proposals;
    ALTER TABLE mcp.archived_commit_proposals ADD COLUMN archived_at timestamptz NOT NULL DEFAULT now();
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'mcp.archived_commit_proposals'::regclass AND conname = 'drafts_pkey') THEN
      ALTER TABLE mcp.archived_commit_proposals RENAME CONSTRAINT drafts_pkey TO archived_commit_proposals_pkey;
    END IF;
    ALTER INDEX IF EXISTS mcp.drafts_owner RENAME TO archived_commit_proposals_owner;
  END IF;
END;
$$;
CREATE OR REPLACE FUNCTION mcp.reject_archived_commit_proposal_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'archived commit proposals are read-only';
END;
$$;
DO $$
BEGIN
  IF to_regclass('mcp.archived_commit_proposals') IS NOT NULL THEN
    DROP TRIGGER IF EXISTS archived_commit_proposals_read_only ON mcp.archived_commit_proposals;
    CREATE TRIGGER archived_commit_proposals_read_only
      BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON mcp.archived_commit_proposals
      FOR EACH STATEMENT EXECUTE FUNCTION mcp.reject_archived_commit_proposal_mutation();
  END IF;
END;
$$;
-- Grant authorizations no longer expire: clear legacy deadlines so existing rows stay valid until revoked.
ALTER TABLE mcp.grants ALTER COLUMN expires_at DROP NOT NULL;
UPDATE mcp.grants SET expires_at = NULL WHERE expires_at IS NOT NULL;
-- Retired permission catalog.import.submit: strip from grant scopes (no alias).
UPDATE mcp.grants
SET scopes = COALESCE((
  SELECT jsonb_agg(s)
  FROM jsonb_array_elements_text(scopes) AS s
  WHERE s <> 'catalog.import.submit'
), '[]'::jsonb)
WHERE scopes @> '["catalog.import.submit"]'::jsonb;
ALTER TABLE mcp.schema_version DROP CONSTRAINT IF EXISTS schema_version_version_check;
UPDATE mcp.schema_version SET version = 2 WHERE version = 1;
INSERT INTO mcp.schema_version(version) VALUES(2) ON CONFLICT DO NOTHING;
ALTER TABLE mcp.schema_version ADD CONSTRAINT schema_version_version_check CHECK(version = 2);
