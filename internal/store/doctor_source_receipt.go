package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// DoctorSourceReceipt identifies the original diagnosis attempt and its exact
// accepted snapshot, independently of the current conversation generation.
type DoctorSourceReceipt struct {
	AttemptID string
	State     string
	Snapshot  json.RawMessage
}

// DoctorDiagnosisBySource reads only the named source's accepted result. It
// never adopts a result, advances a conversation, or selects a newer attempt.
func (db *DB) DoctorDiagnosisBySource(ctx context.Context, target, source string) (DoctorSourceReceipt, error) {
	var receipt DoctorSourceReceipt
	if target == "" || source == "" {
		return receipt, ErrNotFound
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT a.id,a.state FROM doctor_attempts a JOIN doctor_conversations c ON c.id=a.conversation_id WHERE a.source_id=? AND c.task_ref=?`, source, target).Scan(&receipt.AttemptID, &receipt.State); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var size int
		var snapshotHash string
		err := tx.QueryRowContext(ctx, `SELECT length(r.snapshot),substr(r.snapshot,1,65537),r.snapshot_hash FROM application_result_requests q JOIN application_result_revisions r ON r.result_id=q.result_id AND r.revision=q.applied_revision AND r.request_row=q.id AND r.attempt=q.attempt AND r.doctor_attempt_id=q.doctor_attempt_id WHERE q.doctor_attempt_id=? AND q.attempt=? AND q.input_state='accepted' AND q.applied_revision>0 ORDER BY q.id DESC LIMIT 1`, receipt.AttemptID, receipt.AttemptID).Scan(&size, &receipt.Snapshot, &snapshotHash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if size > 65536 {
			return errors.New("retained diagnosis exceeds display limit")
		}
		if snapshotHash != submissionDigest(receipt.Snapshot) {
			return errors.New("retained diagnosis integrity check failed")
		}
		return nil
	})
	return receipt, err
}
