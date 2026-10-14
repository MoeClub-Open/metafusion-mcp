CREATE SCHEMA IF NOT EXISTS mcp;
CREATE TABLE IF NOT EXISTS mcp.schema_version (version integer PRIMARY KEY CHECK(version=1));
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
CREATE TABLE IF NOT EXISTS mcp.drafts (
 id uuid PRIMARY KEY, user_id text NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '30 days'
);
CREATE INDEX IF NOT EXISTS drafts_owner ON mcp.drafts(user_id, created_at DESC);
INSERT INTO mcp.schema_version(version) VALUES(1) ON CONFLICT DO NOTHING;
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
