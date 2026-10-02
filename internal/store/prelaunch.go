package store

import (
	"context"
	"database/sql"
	"fmt"
)

// StartRun claims the execution boundary once. A crash after this write is
// conservatively reconciled as interrupted, never retried as preparation.
func (db *DB) StartRun(ctx context.Context, runID int64) (Run, error) {
	var run Run
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		run, err = scanRun(tx.QueryRowContext(ctx, `UPDATE runs SET status='running' WHERE id=? AND status='prelaunch' RETURNING `+runColumns, runID))
		if err != nil {
			return err
		}
		if IsAutomaticTrigger(run.TriggerCommentID) {
			if err := validateAutomaticAllocation(ctx, tx, AllocateRunParams{SessionID: run.SessionID, SegmentID: run.SegmentID, Agent: run.Agent, Mode: run.Mode, TriggerCommentID: run.TriggerCommentID}); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE automatic_continuations SET status='running',allocated_run_id=? WHERE id=? AND status='reserved'`, run.ID, run.TriggerCommentID-automaticTriggerBase)
		}
		if err == nil && IsGraphTrigger(run.TriggerCommentID) {
			err = bindGraphDispatch(ctx, tx, run)
		}
		return err
	})
	if err != nil {
		return Run{}, fmt.Errorf("store: start run %d: %w", runID, err)
	}
	return run, nil
}

// Reportable excludes both retryable preparation and owned execution.
func (r Run) Reportable() bool {
	return r.Status == RunSucceeded || r.Status == RunFailed || r.Status == RunReported
}
