package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// ExportArchivedProposals is an explicit operator export of one owner's retired
// MCP proposals. It does not apply or rewrite them, and is not exposed to MCP.
func (s *Store) ExportArchivedProposals(ctx context.Context, owner string, out io.Writer) (int, error) {
	if strings.TrimSpace(owner) == "" {
		return 0, errors.New("archive owner is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT to_regclass('mcp.archived_commit_proposals') IS NOT NULL").Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id,payload,created_at,expires_at,archived_at
FROM mcp.archived_commit_proposals WHERE user_id=$1 ORDER BY created_at,id`, owner)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	enc := json.NewEncoder(out)
	count := 0
	for rows.Next() {
		var proposal struct {
			ID         string          `json:"id"`
			OwnerID    string          `json:"owner_id"`
			Payload    json.RawMessage `json:"payload"`
			CreatedAt  time.Time       `json:"created_at"`
			ExpiresAt  time.Time       `json:"expires_at"`
			ArchivedAt time.Time       `json:"archived_at"`
		}
		if err = rows.Scan(&proposal.ID, &proposal.OwnerID, &proposal.Payload, &proposal.CreatedAt, &proposal.ExpiresAt, &proposal.ArchivedAt); err != nil {
			return count, err
		}
		if err = enc.Encode(proposal); err != nil {
			return count, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return count, err
	}
	return count, tx.Commit()
}
