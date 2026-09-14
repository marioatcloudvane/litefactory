package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"
)

// CommentOperation retains the exact prepared intent before any outward dispatch.
// Uncertain is also the crash-safe initial state: reservation is never reusable.
type CommentOperation struct {
	Scope       FamilyScope
	RequestID   string
	IssueNumber int
	BotID       int64
	Body        string
	State       string
	CommentID   int64
}

const commentOperationColumns = `run_id,attempt,request_id,repository,task,issue_number,bot_id,body,state,comment_id`

func scanCommentOperation(row interface{ Scan(...interface{}) error }) (CommentOperation, error) {
	var o CommentOperation
	err := row.Scan(&o.Scope.RunID, &o.Scope.Attempt, &o.RequestID, &o.Scope.Repository, &o.Scope.Task, &o.IssueNumber, &o.BotID, &o.Body, &o.State, &o.CommentID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return o, err
}

// ReserveCommentDispatch elects at most one dispatcher across processes. No SQL
// transaction survives this call and no subsequent call can recover a POST right.
func (db *DB) ReserveCommentDispatch(ctx context.Context, b ResultBinding, o CommentOperation) (CommentOperation, bool, error) {
	var result CommentOperation
	first := false
	if !familyScopeMatches(o.Scope, b) || o.RequestID == "" || len(o.RequestID) > 128 || o.IssueNumber < 1 || o.BotID < 1 || len(o.Body) == 0 || len(o.Body) > 65536 || !utf8.ValidString(o.Body) {
		return result, false, fmt.Errorf("store: invalid comment intent")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		old, err := scanCommentOperation(tx.QueryRowContext(ctx, `SELECT `+commentOperationColumns+` FROM comment_operations WHERE run_id=? AND attempt=? AND request_id=?`, b.RunID, b.Attempt, o.RequestID))
		if err == nil {
			if old.Scope != o.Scope || old.IssueNumber != o.IssueNumber || old.BotID != o.BotID || old.Body != o.Body {
				return ErrResultCorrelation
			}
			result = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		// A bound family admission is required; an operation cannot bypass its budget.
		var tool string
		if err := tx.QueryRowContext(ctx, `SELECT tool FROM family_requests WHERE run_id=? AND attempt=? AND request_id=?`, b.RunID, b.Attempt, o.RequestID).Scan(&tool); err != nil {
			return err
		}
		if tool != "issues_comment" {
			return ErrResultCorrelation
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO comment_operations (`+commentOperationColumns+`) VALUES(?,?,?,?,?,?,?,?,'uncertain',0)`, b.RunID, b.Attempt, o.RequestID, o.Scope.Repository, o.Scope.Task, o.IssueNumber, o.BotID, o.Body)
		if err != nil {
			return err
		}
		o.State = "uncertain"
		o.CommentID = 0
		result = o
		first = true
		return nil
	})
	return result, first, err
}

// GetCommentOperation is an internal observation accessor. The caller authorizes
// the reader scope before invoking it; it provides no dispatch capability.
func (db *DB) GetCommentOperation(ctx context.Context, s FamilyScope, requestID string) (CommentOperation, error) {
	return scanCommentOperation(db.sql.QueryRowContext(ctx, `SELECT `+commentOperationColumns+` FROM comment_operations WHERE run_id=? AND attempt=? AND request_id=? AND repository=? AND task=?`, s.RunID, s.Attempt, requestID, s.Repository, s.Task))
}

// ConfirmCommentOperation records a fact from an authenticated POST or separately
// authorized exact-match observation, even after the original attempt is revoked.
func (db *DB) ConfirmCommentOperation(ctx context.Context, o CommentOperation, id int64) error {
	if id < 1 {
		return fmt.Errorf("store: invalid confirmed comment identity")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE comment_operations SET state='confirmed',comment_id=? WHERE run_id=? AND attempt=? AND request_id=? AND repository=? AND task=? AND issue_number=? AND bot_id=? AND body=? AND (comment_id=0 OR comment_id=?)`, id, o.Scope.RunID, o.Scope.Attempt, o.RequestID, o.Scope.Repository, o.Scope.Task, o.IssueNumber, o.BotID, o.Body, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrResultCorrelation
	}
	return nil
}
