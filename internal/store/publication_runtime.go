package store

import (
	"context"
	"database/sql"
	"errors"
)

// EligibleCreationIntent observes the exact current, unconsumed owner approval.
// BindGrant remains the atomic authority check and consumption boundary.
func (c *CreationStore) EligibleCreationIntent(ctx context.Context, task string) (int64, CreationProposal, error) {
	var id int64
	var p CreationProposal
	err := c.db.sql.QueryRowContext(ctx, `SELECT i.id,p.hash,p.repository,p.source_task,p.relationship,p.artifacts,p.envelope FROM mcp_creation_intents i JOIN mcp_creation_current n ON n.source_task=i.source_task AND n.proposal_hash=i.proposal_hash AND n.revision=i.proposal_revision JOIN mcp_creation_proposals p ON p.hash=i.proposal_hash WHERE i.source_task=? AND i.expires_ns>? AND NOT EXISTS (SELECT 1 FROM mcp_creation_grants g WHERE g.intent_id=i.id) ORDER BY i.id DESC LIMIT 1`, task, c.now().UnixNano()).Scan(&id, &p.Hash, &p.Repository, &p.SourceTask, &p.Relationship, &p.Artifacts, &p.Envelope)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return id, p, err
}

// PendingCommentOperations enumerates bounded retained effects for factory observation.
func (db *DB) PendingCommentOperations(ctx context.Context, afterRun int64, afterAttempt, afterRequest string, limit int) ([]CommentOperation, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrResultAuthority
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+commentOperationColumns+` FROM comment_operations WHERE state='uncertain' AND (run_id,attempt,request_id)>(?,?,?) ORDER BY run_id,attempt,request_id LIMIT ?`, afterRun, afterAttempt, afterRequest, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommentOperation
	for rows.Next() {
		o, err := scanCommentOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PendingCreationOperation supplies the original observation scope without a lease.
type PendingCreationOperation struct {
	ID    int64
	Scope FamilyScope
}

// PendingCreationOperations never returns or recreates dispatch permits.
func (db *DB) PendingCreationOperations(ctx context.Context, after int64, limit int) ([]PendingCreationOperation, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrCreationAuthority
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT o.id,o.repository,p.source_task,g.run_id,g.attempt FROM mcp_creation_operations o JOIN mcp_creation_proposals p ON p.hash=o.proposal_hash JOIN mcp_creation_grants g ON g.id=o.grant_id WHERE o.id>? AND o.dispatched=1 AND o.state IN ('dispatched','uncertain') ORDER BY o.id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingCreationOperation
	for rows.Next() {
		var o PendingCreationOperation
		if err := rows.Scan(&o.ID, &o.Scope.Repository, &o.Scope.Task, &o.Scope.RunID, &o.Scope.Attempt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
