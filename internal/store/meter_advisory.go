package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// StartAdvisoryMeterAttempt attributes one real private execution to the shared
// ledger without borrowing its source task's workflow, judge or run identity.
func (db *DB) StartAdvisoryMeterAttempt(ctx context.Context, execution, provider string) (MeterAttempt, error) {
	if meterIdentity(execution, provider) != nil {
		return MeterAttempt{}, ErrConnectorInvalid
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), AdvisoryExecutionID: execution, CodingAgent: provider}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM durable_workflow_attempts a JOIN durable_workflow_worker w ON w.attempt_id=a.id AND w.generation=a.fence WHERE a.execution_id=? AND a.admission_kind='advisory' AND a.state='running' AND a.deadline>? AND w.expires_at>?)`, execution, ownerActionTime(nowUTC()), ownerActionTime(nowUTC())).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrResultAuthority
		}
		var pinned sql.NullInt64
		var observedProvider string
		err := tx.QueryRowContext(ctx, `SELECT price_revision_id,provider FROM task_advisory_invocations WHERE execution_id=? AND outcome='running'`, execution).Scan(&pinned, &observedProvider)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if pinned.Valid && observedProvider != provider {
			return ErrResultAuthority
		}
		// The dedicated launch guard already pins current prices before execution.
		// Reuse that catalog if a price edit lands between these two local writes.
		return tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,advisory_execution_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,1,?,?,'running','unknown','[]',COALESCE(?,(SELECT MAX(id) FROM meter_price_revisions))) RETURNING ordinal,price_revision_id`, a.ID, execution, provider, FormatTime(nowUTC()), pinned).Scan(&a.Ordinal, &a.PriceRevisionID)
	})
	return a, err
}

// AdvisoryMeterAttempt recovers the sole canonical identity without launching work.
func (db *DB) AdvisoryMeterAttempt(ctx context.Context, execution string) (MeterAttempt, error) {
	var id string
	err := db.sql.QueryRowContext(ctx, `SELECT id FROM meter_attempts WHERE advisory_execution_id=?`, execution).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return MeterAttempt{}, ErrNotFound
	}
	if err != nil {
		return MeterAttempt{}, err
	}
	return db.GetMeterAttempt(ctx, id)
}

// settleAdvisoryMeterTx closes crash/stop leftovers without inventing provider
// completion or observations. Already finalized collections stay unchanged.
func settleAdvisoryMeterTx(ctx context.Context, tx *sql.Tx, execution, state string, at time.Time) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM meter_attempts WHERE advisory_execution_id=? AND finished_at IS NULL`, execution).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	outcome := "failure"
	if state == "cancelled" {
		outcome = "cancelled"
	}
	if err = meterAttemptDiagnostic(ctx, tx, id, "advisory_completion_unverified"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=?,outcome=? WHERE id=? AND finished_at IS NULL`, FormatTime(at), outcome, id)
	return err
}
