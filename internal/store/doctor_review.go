package store

import (
	"context"
	"database/sql"
	"errors"
)

// DoctorRejectedReview returns the latest rejected diagnosis in this conversation
// generation. It does not change the accepted result, proposal or attempt state.
func (db *DB) DoctorRejectedReview(ctx context.Context, cid int64) (ResultReceipt, error) {
	var r ResultReceipt
	err := db.sql.QueryRowContext(ctx, `SELECT q.request_id,q.payload,q.diagnostics FROM application_result_requests q JOIN application_results ar ON ar.id=q.result_id JOIN doctor_attempts a ON a.id=q.doctor_attempt_id JOIN doctor_conversations c ON c.id=a.conversation_id WHERE c.id=? AND ar.work_context=('doctor:'||c.id||':generation:'||c.generation) AND q.input_state='rejected' AND NOT EXISTS(SELECT 1 FROM application_result_requests newer WHERE newer.result_id=q.result_id AND newer.id>q.id AND newer.input_state='accepted' AND newer.applied_revision>0) ORDER BY q.id DESC LIMIT 1`, cid).Scan(&r.RequestID, &r.Payload, &r.Diagnostics)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// DoctorSourceRejectedReview recovers only this source's inert rejected payload.
func (db *DB) DoctorSourceRejectedReview(ctx context.Context, source string) (DoctorAttempt, ResultReceipt, error) {
	var attempt DoctorAttempt
	var receipt ResultReceipt
	err := db.sql.QueryRowContext(ctx, `SELECT a.id,a.conversation_id,a.source_id,a.state,q.request_id,q.payload,q.diagnostics FROM doctor_attempts a JOIN application_result_requests q ON q.doctor_attempt_id=a.id WHERE a.source_id=? AND q.input_state='rejected' AND NOT EXISTS(SELECT 1 FROM application_result_requests accepted WHERE accepted.doctor_attempt_id=a.id AND accepted.input_state='accepted' AND accepted.applied_revision>0) ORDER BY q.id DESC LIMIT 1`, source).Scan(&attempt.ID, &attempt.ConversationID, &attempt.SourceID, &attempt.State, &receipt.RequestID, &receipt.Payload, &receipt.Diagnostics)
	if errors.Is(err, sql.ErrNoRows) {
		return attempt, receipt, ErrNotFound
	}
	return attempt, receipt, err
}
