package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// RetainedCreationProposal returns the exact canonical proposal retained under
// hash, or ErrNotFound. The factory presents these bytes to the owner, so the
// caller must never reassemble them from anything else.
func (c *CreationStore) RetainedCreationProposal(ctx context.Context, hash string) (CreationProposal, error) {
	var p CreationProposal
	err := c.db.sql.QueryRowContext(ctx, `SELECT hash,repository,source_task,relationship,artifacts,envelope FROM mcp_creation_proposals WHERE hash=?`, hash).Scan(&p.Hash, &p.Repository, &p.SourceTask, &p.Relationship, &p.Artifacts, &p.Envelope)
	if errors.Is(err, sql.ErrNoRows) {
		return CreationProposal{}, ErrNotFound
	}
	if err != nil {
		return CreationProposal{}, fmt.Errorf("store: read retained creation proposal: %w", err)
	}
	return p, nil
}
