package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// CreationRejection retains the immutable decision and exact delivery before I/O.
// Delivery state itself remains owned by report_deliveries.
type CreationRejection struct {
	Repository, SourceTask, CommandKind, Reason string
	SessionID, CommentID                        int64
	Delivery                                    ReportDelivery
}

// GetCreationRejection reads the source-bound decision and original delivery bytes.
func (db *DB) GetCreationRejection(ctx context.Context, sessionID, commentID int64) (CreationRejection, error) {
	var r CreationRejection
	var data []byte
	err := db.sql.QueryRowContext(ctx, `SELECT session_id,comment_id,repository,source_task,command_kind,reason,delivery FROM creation_rejections WHERE session_id=? AND comment_id=?`, sessionID, commentID).Scan(&r.SessionID, &r.CommentID, &r.Repository, &r.SourceTask, &r.CommandKind, &r.Reason, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(data, &r.Delivery)
	return r, err
}

// RetainCreationRejection is first-write-wins; a retry cannot rewrite its reason,
// safe bytes, bot identity or stable report association.
func (db *DB) RetainCreationRejection(ctx context.Context, r CreationRejection) (CreationRejection, error) {
	if r.SessionID < 1 || r.CommentID < 1 || r.Reason == "" || (r.CommandKind != "approval" && r.CommandKind != "preparation") || r.Delivery.SessionID != r.SessionID || r.Delivery.TriggerID != r.CommentID || r.Delivery.Task != r.SourceTask || r.Delivery.Destination != r.SourceTask || r.Delivery.Purpose != "creation-rejection" || r.Delivery.Body == "" || len(r.Delivery.Body) > 65536 || r.Delivery.Key == "" {
		return CreationRejection{}, ErrCreationAuthority
	}
	data, err := json.Marshal(r.Delivery)
	if err != nil {
		return CreationRejection{}, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions s JOIN repos p ON p.id=s.repo_id WHERE s.id=? AND s.task_ref=? AND p.owner || '/' || p.name=?`, r.SessionID, r.SourceTask, r.Repository).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrCreationAuthority
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO creation_rejections(session_id,comment_id,repository,source_task,command_kind,reason,delivery) VALUES(?,?,?,?,?,?,?) ON CONFLICT(session_id,comment_id) DO NOTHING`, r.SessionID, r.CommentID, r.Repository, r.SourceTask, r.CommandKind, r.Reason, data)
		return err
	})
	if err != nil {
		return CreationRejection{}, err
	}
	return db.GetCreationRejection(ctx, r.SessionID, r.CommentID)
}

// CreationPolicyRefusal is emitted only after a successful policy observation.
// An operational error that wraps ErrCreationAuthority is not this type.
type CreationPolicyRefusal struct{ Cause error }

func (*CreationPolicyRefusal) Error() string { return "store: confirmed creation policy refusal" }
func (e *CreationPolicyRefusal) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrCreationAuthority
}
