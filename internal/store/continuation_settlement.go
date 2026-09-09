package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// settleContinuationRun runs only after both terminal evidence and any fresh
// continuation have been written. The original schedule owns dependency edges
// throughout the chain; a consumed automatic action never becomes a new edge.
func settleContinuationRun(ctx context.Context, tx *sql.Tx, run Run) error {
	c, err := scanContinuation(tx.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE allocated_run_id=?`, run.ID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if c.Status != "running" {
		return nil
	}
	status := "failed"
	if run.Succeeded() {
		status = "succeeded"
	}
	next, err := getContinuationBySource(ctx, tx, run.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if run.SemanticOutcome == "interrupted" && err == nil && (next.Status == "pending" || next.Status == "reserved" || next.Status == "running") {
		status = "interrupted"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE automatic_continuations SET status=?, finished_at=? WHERE id=? AND status='running'`, status, FormatTime(run.FinishedAt), c.ID); err != nil {
		return err
	}
	if status == "interrupted" {
		return nil
	}
	return settleContinuationPrerequisite(ctx, tx, c, run.ID, run.Succeeded(), run.FinishedAt)
}

func settleContinuationPrerequisite(ctx context.Context, tx *sql.Tx, c Continuation, runID int64, success bool, at time.Time) error {
	if c.PrerequisiteID == 0 {
		return nil
	}
	status := ScheduleFailed
	if success {
		status = ScheduleSucceeded
	}
	result, err := tx.ExecContext(ctx, `UPDATE schedules SET status=?, run_id=COALESCE(?,run_id), finished_at=?, updated_at=? WHERE id=? AND status='suspended' AND recovery_root_run_id=?`, string(status), nullID(runID), FormatTime(at), FormatTime(at), c.PrerequisiteID, c.RootRunID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	if !success {
		return blockDescendants(ctx, tx, c.PrerequisiteID, at)
	}
	return resolveDependentDueTimes(ctx, tx, c.PrerequisiteID, at)
}

// Called in the orphan transaction after allocated runs have been reconciled.
// Reservations without allocations spent no attempt; allocated runs may never
// be replayed just because their worker vanished before updating queue state.
func reconcileContinuations(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE automatic_continuations SET status='pending', reserved_at=NULL WHERE status='reserved' AND allocated_run_id IS NULL`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE status='running' ORDER BY id`)
	if err != nil {
		return err
	}
	var actions []Continuation
	for rows.Next() {
		c, scanErr := scanContinuation(rows)
		if scanErr != nil {
			return errors.Join(scanErr, rows.Close())
		}
		actions = append(actions, c)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, c := range actions {
		run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, c.AllocatedRunID))
		if err != nil {
			return fmt.Errorf("read allocated continuation %d: %w", c.ID, err)
		}
		if err := settleContinuationRun(ctx, tx, run); err != nil {
			return fmt.Errorf("settle continuation %d: %w", c.ID, err)
		}
	}
	return nil
}
