package store

import (
	"context"
	"database/sql"
	"errors"
)

// LatestRejectedResultReceipt locates review content from this exact attempt.
// It does not enumerate a bounded history (which could hide older rejections),
// create a revision, or change any acceptance/publication observation. A later
// accepted submission supersedes earlier rejected review content in run reports.
func (db *DB) LatestRejectedResultReceipt(ctx context.Context, b ResultBinding) (ResultReceipt, error) {
	var receipt ResultReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		var requestID string
		err := tx.QueryRowContext(ctx, `SELECT request_id FROM application_result_requests WHERE result_id=? AND COALESCE(run_id,0)=? AND attempt=? AND input_state='rejected' AND applied_revision=0 AND id > COALESCE((SELECT MAX(id) FROM application_result_requests WHERE result_id=? AND COALESCE(run_id,0)=? AND attempt=? AND input_state='accepted'),0) ORDER BY id DESC LIMIT 1`, b.ResultID, b.RunID, b.Attempt, b.ResultID, b.RunID, b.Attempt).Scan(&requestID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		receipt, err = resultReceiptTx(ctx, tx, b.ResultID, b.RunID, requestID)
		return err
	})
	return receipt, err
}
