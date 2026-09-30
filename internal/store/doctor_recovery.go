package store

import (
	"context"
	"database/sql"
	"errors"
)

// DoctorSourceReceipt looks up the source's immutable accepted result, rather
// than the conversation's potentially newer current findings.
func (db *DB) DoctorSourceReceipt(ctx context.Context, source string) (DoctorAttempt, string, []byte, error) {
	var a DoctorAttempt
	var request string
	var data []byte
	err := db.sql.QueryRowContext(ctx, `SELECT a.id,a.conversation_id,a.source_id,a.state,a.evidence,q.request_id,r.snapshot FROM doctor_attempts a JOIN application_result_requests q ON q.doctor_attempt_id=a.id JOIN application_result_revisions r ON r.request_row=q.id WHERE a.source_id=? AND q.input_state='accepted' ORDER BY q.applied_revision DESC LIMIT 1`, source).Scan(&a.ID, &a.ConversationID, &a.SourceID, &a.State, &a.Evidence, &request, &data)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, request, data, err
}

// DoctorConversationByID reads a retained generation for source recovery.
func (db *DB) DoctorConversationByID(ctx context.Context, id int64) (DoctorConversation, error) {
	return scanDoctorConversation(db.sql.QueryRowContext(ctx, `SELECT id,task_ref,generation,state,revision FROM doctor_conversations WHERE id=?`, id))
}
