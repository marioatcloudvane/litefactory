package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// StageDecisionDelivery freezes a confirmed decision's public bytes and origin.
type StageDecisionDelivery struct {
	ExecutionID, ProposalID, TaskRef, Rendered, Digest string
	Principal                                          OwnerActionPrincipal
	State, Reason, CommentURL                          string
	NextAttemptAt                                      time.Time
}

const stageDeliveryColumns = `execution_id,proposal_id,task_ref,rendered,digest,principal,state,reason,comment_url,next_attempt_at`

func scanStageDelivery(row scanner) (StageDecisionDelivery, error) {
	var v StageDecisionDelivery
	var raw, next string
	err := row.Scan(&v.ExecutionID, &v.ProposalID, &v.TaskRef, &v.Rendered, &v.Digest, &raw, &v.State, &v.Reason, &v.CommentURL, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v.Principal)
	if err == nil && next != "" {
		v.NextAttemptAt, err = ParseTime(next)
	}
	return v, err
}

// EnsureStageDecisionDelivery is idempotent and requires a committed transition.
func (db *DB) EnsureStageDecisionDelivery(ctx context.Context, v StageDecisionDelivery, at time.Time) error {
	if v.ExecutionID == "" || v.ProposalID == "" || v.Rendered == "" || len(v.Rendered) > 65536 || v.Digest == "" {
		return ErrConnectorInvalid
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var applied string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM stage_decision_receipts WHERE execution_id=?`, v.ExecutionID).Scan(&applied); err != nil {
			return err
		}
		if applied != "applied" {
			return ErrGraphInvariant
		}
		old, err := scanStageDelivery(tx.QueryRowContext(ctx, `SELECT `+stageDeliveryColumns+` FROM stage_decision_deliveries WHERE execution_id=?`, v.ExecutionID))
		if err == nil {
			if old.Digest != v.Digest || old.TaskRef != v.TaskRef || old.Rendered != v.Rendered {
				return ErrWorkflowConflict
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		raw, err := json.Marshal(v.Principal)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO stage_decision_deliveries(execution_id,proposal_id,task_ref,rendered,digest,principal,state,reason,comment_url,next_attempt_at,created_at) VALUES(?,?,?,?,?,?,'pending','','','',?)`, v.ExecutionID, v.ProposalID, v.TaskRef, v.Rendered, v.Digest, string(raw), FormatTime(at))
		return err
	})
}

// ReadStageDecisionDelivery returns the retained summary and independent delivery state.
func (db *DB) ReadStageDecisionDelivery(ctx context.Context, key string) (StageDecisionDelivery, error) {
	return scanStageDelivery(db.sql.QueryRowContext(ctx, `SELECT `+stageDeliveryColumns+` FROM stage_decision_deliveries WHERE execution_id=?`, key))
}

// AdvanceStageDecisionDelivery never returns a possibly sent summary to pending.
func (db *DB) AdvanceStageDecisionDelivery(ctx context.Context, key, expected, state, reason, url string, due, at time.Time) error {
	valid := expected == "pending" && (state == "sending" || state == "failed") || (expected == "sending" || expected == "uncertain") && (state == "delivered" || state == "uncertain") || expected == "sending" && state == "failed" || expected == "sending" && state == "pending" && reason == "provider refused before send"
	if !valid || state == "delivered" && !strings.HasPrefix(url, "https://") {
		return ErrConnectorInvalid
	}
	next := ""
	if !due.IsZero() {
		next = FormatTime(due)
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if state == "sending" {
			v, err := scanStageDelivery(tx.QueryRowContext(ctx, `SELECT `+stageDeliveryColumns+` FROM stage_decision_deliveries WHERE execution_id=?`, key))
			if err != nil {
				return err
			}
			t, err := resolveConnectorDiscussionTarget(ctx, tx, v.TaskRef)
			if err != nil {
				return err
			}
			if err = ownerActionAuthority(ctx, tx, v.Principal, OwnerAction{Principal: v.Principal, RepositoryID: t.RepoID, RepositoryRef: t.RepositoryRef, Scope: "repository:actions"}, at); err != nil {
				return err
			}
		}
		query := `UPDATE stage_decision_deliveries SET state=?,reason=?,comment_url=?,next_attempt_at=? WHERE execution_id=? AND state=?`
		args := []any{state, reason, url, next, key, expected}
		if state == "sending" {
			query += ` AND (next_attempt_at='' OR next_attempt_at<=?)`
			args = append(args, FormatTime(at))
		}
		r, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err == nil && n != 1 {
			return ErrWorkflowRevision
		}
		return err
	})
}

// PendingStageDecisionDeliveries selects one due publication or receipt reconciliation.
func (db *DB) PendingStageDecisionDeliveries(ctx context.Context, at time.Time) ([]StageDecisionDelivery, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+stageDeliveryColumns+` FROM stage_decision_deliveries WHERE state IN ('pending','sending','uncertain') AND (next_attempt_at='' OR next_attempt_at<=?) ORDER BY created_at,execution_id LIMIT 1`, FormatTime(at))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StageDecisionDelivery
	for rows.Next() {
		v, e := scanStageDelivery(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
