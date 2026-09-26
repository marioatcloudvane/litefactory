package store

import (
	"context"
	"database/sql"
	"errors"
)

// ClaimBugWork serializes repair, saving and child/check work for one task.
// Ownership survives process failure until the owning domain confirms quiescence.
func (d *DB) ClaimBugWork(ctx context.Context, session int64, owner, kind string) (bool, error) {
	if session <= 0 || owner == "" || len(owner) > 256 {
		return false, errors.New("store: invalid Bug work owner")
	}
	switch kind {
	case "repair", "checks", "child", "save":
	default:
		return false, errors.New("store: invalid Bug work kind")
	}
	var claimed bool
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		claimed, err = claimBugWorkTx(ctx, tx, session, owner, kind)
		return err
	})
	return claimed, err
}
func claimBugWorkTx(ctx context.Context, tx *sql.Tx, session int64, owner, kind string) (bool, error) {
	if kind != "repair" {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_repairs WHERE session_id=? AND state IN ('reserved','dispatched')`, session).Scan(&active); err != nil {
			return false, err
		}
		if active != 0 {
			return false, nil
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bug_work_guards(session_id,owner,kind) VALUES(?,?,?) ON CONFLICT(session_id) DO NOTHING`, session, owner, kind); err != nil {
		return false, err
	}
	var oldOwner, oldKind string
	err := tx.QueryRowContext(ctx, `SELECT owner,kind FROM bug_work_guards WHERE session_id=?`, session).Scan(&oldOwner, &oldKind)
	return oldOwner == owner && oldKind == kind, err
}

// ReleaseBugWork requires exact ownership; callers must first confirm cleanup.
func (d *DB) ReleaseBugWork(ctx context.Context, session int64, owner string) error {
	result, err := d.sql.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=?`, session, owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("store: Bug work ownership mismatch")
	}
	return nil
}
