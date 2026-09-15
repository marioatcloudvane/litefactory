package store

import (
	"context"
	"database/sql"
)

// ValidationCheckpoints returns the valid inputs for one currently bound story.
// Run storage quotas bound this materialized result; no cursor escapes the store.
func (db *DB) ValidationCheckpoints(ctx context.Context, runID int64, attempt, story, revision string) ([]SubmissionReceipt, error) {
	var receipts []SubmissionReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := currentSubmissionBinding(ctx, tx, runID, attempt, story, revision); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE run_id=? AND attempt=? AND story_key=? AND revision=? AND input_state='valid' ORDER BY sequence`, runID, attempt, story, revision)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			receipt, err := scanSubmissionReceipt(rows)
			if err != nil {
				return err
			}
			receipts = append(receipts, receipt)
		}
		return rows.Err()
	})
	return receipts, err
}
