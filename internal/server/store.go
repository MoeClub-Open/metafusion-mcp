package server

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	_ "github.com/lib/pq"
	"time"
)

//go:embed schema.sql
var schema string
var ErrInvalidGrant = errors.New("invalid_grant")

type Client struct {
	ID        string   `json:"client_id"`
	Name      string   `json:"client_name"`
	Redirects []string `json:"redirect_uris"`
}
type Grant struct {
	ID         string     `json:"id"`
	UserID     string     `json:"-"`
	ClientID   string     `json:"client_id"`
	ClientName string     `json:"client_name"`
	Scopes     []string   `json:"scopes"`
	PATID      string     `json:"pat_id,omitempty"`
	Cipher     []byte     `json:"-"`
	Expires    time.Time  `json:"expires_at"`
	Revoked    *time.Time `json:"revoked_at"`
}
type Store struct{ DB *sql.DB }

func OpenStore(dsn string) (*Store, error) {
	d, e := sql.Open("postgres", dsn)
	if e != nil {
		return nil, e
	}
	d.SetMaxOpenConns(32)
	d.SetMaxIdleConns(16)
	return &Store{d}, nil
}
func (s *Store) Migrate(ctx context.Context) error { _, e := s.DB.ExecContext(ctx, schema); return e }
func (s *Store) Check(ctx context.Context) error {
	var v int
	e := s.DB.QueryRowContext(ctx, "SELECT version FROM mcp.schema_version").Scan(&v)
	if e != nil {
		return e
	}
	if v != 1 {
		return errors.New("unsupported MCP schema")
	}
	_, e = s.DB.ExecContext(ctx, "SELECT id FROM mcp.drafts LIMIT 0")
	if e != nil {
		return e
	}
	return nil
}
func (s *Store) SaveClient(ctx context.Context, c Client) error {
	b, _ := json.Marshal(c.Redirects)
	_, e := s.DB.ExecContext(ctx, "INSERT INTO mcp.clients(id,name,redirects) VALUES($1,$2,$3)", c.ID, c.Name, string(b))
	return e
}
func (s *Store) Client(ctx context.Context, id string) (Client, error) {
	var c Client
	var b []byte
	e := s.DB.QueryRowContext(ctx, "SELECT id,name,redirects FROM mcp.clients WHERE id=$1", id).Scan(&c.ID, &c.Name, &b)
	if e == nil {
		e = json.Unmarshal(b, &c.Redirects)
	}
	return c, e
}
func (s *Store) SaveCode(ctx context.Context, g Grant, code, redirect, pkce string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	b, _ := json.Marshal(g.Scopes)
	_, e = tx.ExecContext(ctx, "INSERT INTO mcp.grants(id,user_id,client_id,scopes,pat_id,pat_cipher,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)", g.ID, g.UserID, g.ClientID, string(b), g.PATID, g.Cipher, g.Expires)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO mcp.codes(hash,grant_id,redirect_uri,challenge,expires_at) VALUES($1,$2,$3,$4,now()+interval '5 minutes')", hash(code), g.ID, redirect, pkce)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO mcp.audit(user_id,grant_id,action) VALUES($1,$2,'grant.created')", g.UserID, g.ID)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Exchange(ctx context.Context, client, code, redirect, verifier, access, refresh string) (Grant, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Grant{}, e
	}
	defer tx.Rollback()
	var id string
	e = tx.QueryRowContext(ctx, `UPDATE mcp.codes c SET used_at=now() FROM mcp.grants g WHERE c.hash=$1 AND c.grant_id=g.id AND g.client_id=$2 AND c.redirect_uri=$3 AND c.challenge=$4 AND c.used_at IS NULL AND c.expires_at>now() AND g.revoked_at IS NULL AND g.expires_at>now() RETURNING g.id`, hash(code), client, redirect, challenge(verifier)).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return Grant{}, ErrInvalidGrant
	}
	if e != nil {
		return Grant{}, e
	}
	g, e := scanGrant(tx.QueryRowContext(ctx, grantSelect+" WHERE g.id=$1", id))
	if e != nil {
		return g, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO mcp.tokens(access_hash,refresh_hash,grant_id,access_expires_at,refresh_expires_at) VALUES($1,$2,$3,LEAST(now()+interval '15 minutes',$4),$4)`, hash(access), hash(refresh), id, g.Expires)
	if e != nil {
		return g, e
	}
	return g, tx.Commit()
}

const grantSelect = `SELECT g.id,g.user_id,g.client_id,c.name,g.scopes,g.pat_id,g.pat_cipher,g.expires_at,g.revoked_at FROM mcp.grants g JOIN mcp.clients c ON c.id=g.client_id`

type scanner interface{ Scan(...any) error }

func scanGrant(row scanner) (Grant, error) {
	var g Grant
	var b []byte
	e := row.Scan(&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &b, &g.PATID, &g.Cipher, &g.Expires, &g.Revoked)
	if e == nil {
		e = json.Unmarshal(b, &g.Scopes)
	}
	return g, e
}
func (s *Store) Access(ctx context.Context, token string) (Grant, error) {
	return scanGrant(s.DB.QueryRowContext(ctx, grantSelect+` JOIN mcp.tokens t ON t.grant_id=g.id WHERE t.access_hash=$1 AND t.revoked_at IS NULL AND t.access_expires_at>now() AND g.revoked_at IS NULL AND g.expires_at>now()`, hash(token)))
}
func (s *Store) Refresh(ctx context.Context, client, token, access, refresh string) (Grant, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Grant{}, e
	}
	defer tx.Rollback()
	var id string
	e = tx.QueryRowContext(ctx, `UPDATE mcp.tokens t SET revoked_at=now() FROM mcp.grants g WHERE t.refresh_hash=$1 AND t.grant_id=g.id AND g.client_id=$2 AND t.revoked_at IS NULL AND t.refresh_expires_at>now() AND g.revoked_at IS NULL AND g.expires_at>now() RETURNING g.id`, hash(token), client).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return Grant{}, ErrInvalidGrant
	}
	if e != nil {
		return Grant{}, e
	}
	g, e := scanGrant(tx.QueryRowContext(ctx, grantSelect+" WHERE g.id=$1", id))
	if e != nil {
		return g, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO mcp.tokens(access_hash,refresh_hash,grant_id,access_expires_at,refresh_expires_at) VALUES($1,$2,$3,LEAST(now()+interval '15 minutes',$4),$4)`, hash(access), hash(refresh), id, g.Expires)
	if e != nil {
		return g, e
	}
	return g, tx.Commit()
}
func (s *Store) Grants(ctx context.Context, user string) ([]Grant, error) {
	rows, e := s.DB.QueryContext(ctx, grantSelect+" WHERE g.user_id=$1 ORDER BY g.created_at DESC LIMIT 100", user)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		g, e := scanGrant(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) Revoke(ctx context.Context, user, id string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var found string
	e = tx.QueryRowContext(ctx, "UPDATE mcp.grants SET revoked_at=COALESCE(revoked_at,now()) WHERE user_id=$1 AND id=$2 RETURNING id", user, id).Scan(&found)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO mcp.audit(user_id,grant_id,action) VALUES($1,$2,'grant.revoked')", user, id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) RevokeToken(ctx context.Context, client, token string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE mcp.tokens t SET revoked_at=COALESCE(t.revoked_at,now()) FROM mcp.grants g WHERE t.grant_id=g.id AND g.client_id=$1 AND (t.access_hash=$2 OR t.refresh_hash=$2)`, client, hash(token))
	return e
}
