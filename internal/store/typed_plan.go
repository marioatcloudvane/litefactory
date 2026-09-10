package store

import (
	"context"
	"fmt"
)

// FinalizeTypedPlanPublication records publication only after the caller's
// success-only save has restored readiness for this exact generation. The
// broker may already be closed; a newer subprocess attempt invalidates this one.
func (db *DB) FinalizeTypedPlanPublication(ctx context.Context, receiptID int64) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE submission_receipts SET plan_state='published',effect_state='delivered' WHERE id=? AND input_state='valid' AND plan_state IN ('pending','published') AND EXISTS(SELECT 1 FROM submission_attempts a JOIN runs r ON r.id=a.run_id JOIN managed_features f ON f.parent_task=a.parent_task WHERE a.run_id=submission_receipts.run_id AND a.attempt=submission_receipts.attempt AND a.stage='plan' AND f.generation=a.generation AND f.ready=1 AND f.state='planning' AND r.status IN ('running','succeeded') AND NOT EXISTS(SELECT 1 FROM submission_attempts newer WHERE newer.run_id=a.run_id AND newer.rowid>a.rowid))`, receiptID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("typed Plan publication receipt is stale or not ready")
	}
	return nil
}
