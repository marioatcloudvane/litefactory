package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MCPLease adds live transport authority to the existing factory launch identity.
// It is factory-held state, never a caller-selectable transport argument.
type MCPLease struct {
	Binding                    ResultBinding
	Instance, PermissionDigest string
	ExpiresAt                  time.Time
}

type mcpLeaseKey struct{}

// WithMCPLease requires the same live lease at every result transaction, including
// historical reads and exact replay. Unstamped internal recovery is unchanged.
func WithMCPLease(ctx context.Context, lease MCPLease) context.Context {
	return context.WithValue(ctx, mcpLeaseKey{}, lease)
}

// StartMCPInstance invalidates all prior runtime authority without deleting history.
// Only the factory runtime startup path may call this, never agent requests.
func (db *DB) StartMCPInstance(ctx context.Context, instance string) error {
	if !validSubmissionDigest(instance) {
		return ErrResultAuthority
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO mcp_runtime(singleton,instance) VALUES(1,?) ON CONFLICT(singleton) DO UPDATE SET instance=excluded.instance`, instance)
		return err
	})
}

// BindMCPLease can attach authority exactly once to an existing live launch.
// Deadline policy belongs to the factory; there is no universal run timeout.
func (db *DB) BindMCPLease(ctx context.Context, l MCPLease) error {
	if !validSubmissionDigest(l.Instance) || !validSubmissionDigest(l.PermissionDigest) || !nowUTC().Before(l.ExpiresAt) {
		return ErrResultAuthority
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, l.Binding, true); err != nil {
			return err
		}
		var instance string
		if err := tx.QueryRowContext(ctx, `SELECT instance FROM mcp_runtime WHERE singleton=1`).Scan(&instance); err != nil {
			return err
		}
		if instance != l.Instance {
			return ErrResultAuthority
		}
		parentID, err := planMemberParentTx(ctx, tx, l.Binding.RunID)
		if err != nil {
			return err
		}
		if parentID != 0 {
			_, err = tx.ExecContext(ctx, `UPDATE mcp_attempt_leases SET state='replaced' WHERE state='active' AND run_id IN (SELECT older.run_id FROM plan_group_members older JOIN plan_group_members current ON current.run_id=? WHERE older.parent_attempt_id=current.parent_attempt_id AND older.role=current.role AND (older.sequence<current.sequence OR older.run_id=current.run_id))`, l.Binding.RunID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE mcp_attempt_leases SET state='replaced' WHERE state='active' AND (run_id,attempt) IN (SELECT a.run_id,a.attempt FROM application_result_attempts a JOIN application_results r ON r.id=a.result_id WHERE r.task=?)`, l.Binding.Task)
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO mcp_attempt_leases(run_id,attempt,instance,permission_digest,expires_ns,state,bound_ns) VALUES(?,?,?,?,?,'active',?)`, l.Binding.RunID, l.Binding.Attempt, l.Instance, l.PermissionDigest, l.ExpiresAt.UnixNano(), nowUTC().UnixNano())
		return err
	})
}

func checkMCPLease(ctx context.Context, tx *sql.Tx, l MCPLease, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var instance, digest, state, runtime string
	var expiry int64
	err := tx.QueryRowContext(ctx, `SELECT l.instance,l.permission_digest,l.expires_ns,l.state,r.instance FROM mcp_attempt_leases l CROSS JOIN mcp_runtime r WHERE l.run_id=? AND l.attempt=? AND r.singleton=1`, l.Binding.RunID, l.Binding.Attempt).Scan(&instance, &digest, &expiry, &state, &runtime)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrResultAuthority
	}
	if err != nil {
		return err
	}
	if state != "active" || instance != l.Instance || runtime != instance || digest != l.PermissionDigest || expiry != l.ExpiresAt.UnixNano() || now.UnixNano() >= expiry {
		return ErrResultAuthority
	}
	return nil
}

// CheckMCPLease checks fresh bookkeeping time and the original immutable binding.
func (db *DB) CheckMCPLease(ctx context.Context, l MCPLease) error {
	return db.withTx(WithMCPLease(ctx, l), func(tx *sql.Tx) error { return checkResultAccess(WithMCPLease(ctx, l), tx, l.Binding, true) })
}

// RevokeMCPLease is a short durable transaction; it never waits for external I/O.
func (db *DB) RevokeMCPLease(ctx context.Context, l MCPLease, reason string) error {
	if !submissionState(reason, "replaced", "cancelled", "closed") {
		return ErrResultAuthority
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE mcp_attempt_leases SET state=? WHERE run_id=? AND attempt=? AND instance=? AND permission_digest=?`, reason, l.Binding.RunID, l.Binding.Attempt, l.Instance, l.PermissionDigest)
		return err
	})
}
