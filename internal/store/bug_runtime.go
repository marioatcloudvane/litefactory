package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LatestBugRepair returns a retained identity for runtime resolution. Callers
// still use the domain's current approval and completion guards before effects.
func (d *DB) LatestBugRepair(ctx context.Context, session int64) (BugRepair, error) {
	var out BugRepair
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var request string
		if err := tx.QueryRowContext(ctx, `SELECT request FROM bug_repairs WHERE session_id=? ORDER BY ordinal DESC LIMIT 1`, session).Scan(&request); err != nil {
			return err
		}
		var err error
		out, err = bugRepairTx(ctx, tx, session, request)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

// LatestBugTestCall loads the most recently allocated child call for the task.
func (d *DB) LatestBugTestCall(ctx context.Context, session int64) (BugTestCall, error) {
	return scanBugTest(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE session_id=? ORDER BY id DESC LIMIT 1`, session))
}

// CurrentBugDeliveryReview loads the active immutable delivery review.
func (d *DB) CurrentBugDeliveryReview(ctx context.Context, session int64) (BugDeliveryReview, error) {
	var out BugDeliveryReview
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var digest string
		if err := tx.QueryRowContext(ctx, `SELECT digest FROM bug_delivery_reviews WHERE session_id=? AND active=1`, session).Scan(&digest); err != nil {
			return err
		}
		var err error
		out, err = bugDeliveryReviewTx(ctx, tx, session, digest)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

// PendingBugLifecycleReports includes confirmed transport whose lifecycle
// acknowledgment was interrupted. Reading it never turns an acknowledgment into
// completion; the report guard is checked again when the factory settles it.
func (d *DB) PendingBugLifecycleReports(ctx context.Context) ([]BugLifecycleReport, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT session_id,COALESCE(attempt_id,0),event,kind,body,delivery_key,reported FROM bug_lifecycle_events WHERE reported=0 ORDER BY session_id,event`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugLifecycleReport
	for rows.Next() {
		var r BugLifecycleReport
		if err = rows.Scan(&r.SessionID, &r.AttemptID, &r.Event, &r.Kind, &r.Body, &r.DeliveryKey, &r.Reported); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingBugRemoteContinuations selects observation-only recovery checkpoints.
// ClaimBugContinuation rechecks each domain operation and the lifecycle revision.
func (d *DB) PendingBugRemoteContinuations(ctx context.Context) ([]BugParentContinuation, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT l.session_id,l.parent_instance,l.revision,CASE WHEN c.operation IS NOT NULL THEN 'close-reconcile' ELSE 'merge-reconcile' END,COALESCE(c.operation,m.operation) FROM bug_lifecycles l JOIN graph_instances i ON i.id=l.parent_instance LEFT JOIN bug_closures c ON c.session_id=l.session_id AND c.outcome='pending' AND c.dispatched=1 AND c.conflict=0 LEFT JOIN bug_merges m ON m.session_id=l.session_id AND m.outcome='pending' AND m.dispatched=1 AND m.exception='' WHERE l.cancelled=0 AND i.status IN ('succeeded','failed') AND (c.operation IS NOT NULL OR m.operation IS NOT NULL)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugParentContinuation
	for rows.Next() {
		var v BugParentContinuation
		var parent int64
		if err = rows.Scan(&v.SessionID, &parent, &v.ExpectedRevision, &v.Cause, &v.Reference); err != nil {
			return nil, err
		}
		v.Operation = fmt.Sprintf("remote-reconcile:%d:%d", v.SessionID, parent)
		out = append(out, v)
	}
	return out, rows.Err()
}
