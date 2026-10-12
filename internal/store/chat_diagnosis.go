package store

import (
	"context"
	"database/sql"
	"time"
)

// DeferChatDiagnosis keeps only the canonical target until its summary is ready.
func (db *DB) DeferChatDiagnosis(ctx context.Context, transport, id, target string, at time.Time) error {
	return db.DeferChatDiagnosisRepository(ctx, transport, id, target, at, 0)
}

// DeferChatDiagnosisRepository preserves genuine source ownership while the
// asynchronous diagnosis runs; no database cursor crosses the provider call.
func (db *DB) DeferChatDiagnosisRepository(ctx context.Context, transport, id, target string, at time.Time, repoID int64) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='working',target_ref=?,arguments='{}',next_attempt_at=?,resolved_repo_id=NULLIF(?,0) WHERE transport=? AND message_id=? AND state='sent' AND command='diagnose'`, target, FormatTime(at), repoID, transport, id)
	return chatAffected(result, err)
}

// RecoverChatDiagnoses restores abandoned leases only at daemon startup.
func (db *DB) RecoverChatDiagnoses(ctx context.Context, transport string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET next_attempt_at=? WHERE transport=? AND state='working' AND command='diagnose'`, FormatTime(at), transport)
	return err
}

// ClaimChatDiagnosis fences one provider invocation. The existing bounded
// generation counter also bounds a diagnosis to one restart retry.
func (db *DB) ClaimChatDiagnosis(ctx context.Context, transport string, at time.Time) (r ChatReply, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND command='diagnose' AND state='working' AND next_attempt_at<=? ORDER BY created_at,message_id LIMIT 1`, transport, FormatTime(at)))
		if e != nil {
			return e
		}
		if r.AnswerAttempts < 2 {
			r.AnswerAttempts++
		} else {
			r.State = "exhausted"
		}
		_, e = tx.ExecContext(ctx, `UPDATE chat_inbox SET answer_attempts=?,next_attempt_at=? WHERE transport=? AND message_id=?`, r.AnswerAttempts, FormatTime(at.Add(24*time.Hour)), r.Transport, r.MessageID)
		return e
	})
	return r, err
}

// RequeueChatDiagnosisBusy spends no restart allowance when no work began.
func (db *DB) RequeueChatDiagnosisBusy(ctx context.Context, r ChatReply, at time.Time) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET answer_attempts=answer_attempts-1,next_attempt_at=? WHERE transport=? AND message_id=? AND state='working' AND answer_attempts=?`, FormatTime(at), r.Transport, r.MessageID, r.AnswerAttempts)
	return chatAffected(result, err)
}

// CompleteChatDiagnosis makes the final summary eligible for ordinary delivery.
func (db *DB) CompleteChatDiagnosis(ctx context.Context, transport, id string, attempt int, text string, at time.Time) error {
	return db.CompleteChatDiagnosisResult(ctx, transport, id, attempt, text, at, "uncertain")
}

// CompleteChatDiagnosisResult retains the source-declared diagnostic status.
func (db *DB) CompleteChatDiagnosisResult(ctx context.Context, transport, id string, attempt int, text string, at time.Time, status string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='pending',reply_text=?,next_attempt_at=?,outcome_status=? WHERE transport=? AND message_id=? AND state='working' AND answer_attempts=?`, text, FormatTime(at), status, transport, id, attempt)
	return chatAffected(result, err)
}

// ListChatResearchClaims finds notes still owed, even after a claim was allocated.
func (db *DB) ListChatResearchClaims(ctx context.Context, limit int) ([]GraphOwnerWait, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE reply_source='chat' AND reply_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM report_deliveries r WHERE r.delivery_key='chat-owner:'||graph_owner_waits.delivery_key AND r.completed=1) ORDER BY rowid LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GraphOwnerWait
	for rows.Next() {
		r, e := scanGraphOwnerWait(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
