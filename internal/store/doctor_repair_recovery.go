package store

import (
	"context"
	"database/sql"
	"errors"
)

// ReconcileDoctorFileObservation retains an engine reread after a possible file
// write. It never dispatches another write, clears a hold or authorizes restart.
func (db *DB) ReconcileDoctorFileObservation(ctx context.Context, id string, number int, observed string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		step, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if (step.Kind != "session_file" && step.Kind != "worktree_file") || (step.State != "write_intent" && step.State != "uncertain") {
			return errors.New("doctor file has no unresolved write intent")
		}
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM doctor_repairs WHERE id=?`, id).Scan(&state); err != nil {
			return err
		}
		if state == "restart_pending" || state == "recovered" {
			return errors.New("verified handoff cannot be reconciled as an interrupted write")
		}
		settled := "uncertain"
		if observed == doctorDigest([]byte(step.After)) {
			settled = "verified"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET state=?,observed_after=? WHERE repair_id=? AND number=?`, settled, observed, id, number); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='needs_review',fenced=1,failure='Interrupted file write observed; no automatic continuation',revision=revision+1 WHERE id=?`, id)
		return err
	})
}
