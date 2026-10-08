package store

import (
	"context"
	"database/sql"
	"errors"
)

// CreationPublicationArtifact records known facts, never dispatch authority.
type CreationPublicationArtifact struct {
	Ordinal      int
	Dispatched   bool
	IssueID      int64
	IssueNumber  int
	Relationship string
}

// CreationPublicationContext returns retained exact proposal bytes and scope.
func (db *DB) CreationPublicationContext(ctx context.Context, id int64) (CreationProposal, error) {
	var p CreationProposal
	err := db.sql.QueryRowContext(ctx, `SELECT p.hash,p.repository,p.source_task,p.relationship,p.artifacts,p.envelope FROM mcp_creation_operations o JOIN mcp_creation_proposals p ON p.hash=o.proposal_hash WHERE o.id=?`, id).Scan(&p.Hash, &p.Repository, &p.SourceTask, &p.Relationship, &p.Artifacts, &p.Envelope)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

// CreationPublicationRequest resolves crash recovery through the original grant,
// only for a creation-family request in that grant's immutable scope.
func (db *DB) CreationPublicationRequest(ctx context.Context, s FamilyScope, request string) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT o.id FROM family_requests f JOIN creation_publication_requests l ON l.run_id=f.run_id AND l.attempt=f.attempt AND l.request_id=f.request_id JOIN mcp_creation_operations o ON o.id=l.operation_id JOIN mcp_creation_proposals p ON p.hash=o.proposal_hash WHERE f.run_id=? AND f.attempt=? AND f.request_id=? AND f.repository=? AND f.task=? AND f.tool='issues_create_batch' AND p.repository=f.repository AND p.source_task=f.task`, s.RunID, s.Attempt, request, s.Repository, s.Task).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return id, err
}

// SaveCreationArtifact retains monotonic confirmed identities and relation facts.
func (db *DB) SaveCreationArtifact(ctx context.Context, id int64, a CreationPublicationArtifact) error {
	if a.IssueID < 1 || a.IssueNumber < 1 || !submissionState(a.Relationship, "pending", "uncertain", "linked", "attached") {
		return ErrCreationConflict
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE creation_publication_artifacts SET issue_id=?,issue_number=?,relationship=? WHERE operation_id=? AND ordinal=? AND dispatched=1 AND (issue_id=0 OR issue_id=?) AND (issue_number=0 OR issue_number=?) AND (relationship IN ('pending','uncertain') OR relationship=?)`, a.IssueID, a.IssueNumber, a.Relationship, id, a.Ordinal, a.IssueID, a.IssueNumber, a.Relationship)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrCreationConflict
	}
	return nil
}

// CreationPublicationArtifacts returns retained artifact identities and relationship outcomes in ordinal order.
func (db *DB) CreationPublicationArtifacts(ctx context.Context, id int64) ([]CreationPublicationArtifact, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT ordinal,dispatched,issue_id,issue_number,relationship FROM creation_publication_artifacts WHERE operation_id=? ORDER BY ordinal LIMIT 5`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CreationPublicationArtifact
	for rows.Next() {
		var a CreationPublicationArtifact
		if err := rows.Scan(&a.Ordinal, &a.Dispatched, &a.IssueID, &a.IssueNumber, &a.Relationship); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
