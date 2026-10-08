package store

import (
	"context"
)

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
