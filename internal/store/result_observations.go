package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ObserveResult appends a factory observation to a receipt. It never records
// story engineering/acceptance or queues publication. The current writer must
// already possess the separately established fact being observed.
func (db *DB) ObserveResult(ctx context.Context, b ResultBinding, runID int64, requestID, dimension, state string) (ResultReceipt, error) {
	var receipt ResultReceipt
	valid := false
	switch dimension {
	case "engineering":
		valid = submissionState(state, "pending", "recorded", "rejected", "not_applicable")
	case "acceptance":
		valid = submissionState(state, "pending", "accepted", "rejected", "unverified", "not_applicable")
	case "publication":
		valid = submissionState(state, "pending", "published", "delivered", "failed", "not_applicable")
	}
	if !valid {
		return receipt, fmt.Errorf("store: invalid result observation")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		r, err := resultReceiptTx(ctx, tx, b.ResultID, runID, requestID)
		if err != nil {
			return err
		}
		if r.InputState != "accepted" && submissionState(state, "recorded", "accepted", "published", "delivered") {
			return fmt.Errorf("store: rejected result cannot observe successful decisions")
		}
		if dimension == "acceptance" && state == "accepted" && b.Mode != "validate" && (b.Mode != "implement" || b.WorkflowVersion != 1) {
			return fmt.Errorf("store: result mode cannot observe acceptance")
		}
		if dimension == "engineering" && state == "recorded" && b.Mode != "implement" {
			return fmt.Errorf("store: result mode cannot observe engineering")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_observations(request_row,sequence,run_id,attempt,dimension,state) SELECT ?,COALESCE(MAX(sequence),0)+1,?,?,?,? FROM application_result_observations WHERE request_row=?`, r.ID, b.RunID, b.Attempt, dimension, state, r.ID); err != nil {
			return err
		}
		receipt, err = resultReceiptTx(ctx, tx, b.ResultID, runID, requestID)
		return err
	})
	return receipt, err
}
