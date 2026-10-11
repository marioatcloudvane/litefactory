package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// RecoverChatAnswers is called only by the owning daemon at startup. Completed
// delivery text is untouched; abandoned generation is rerun with fresh facts.
func (db *DB) RecoverChatAnswers(ctx context.Context, transport string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET next_attempt_at=? WHERE transport=? AND (state='answering' OR (state='sent' AND command IN ('ask','draft','confirm') AND reply_text=''))`, FormatTime(at), transport)
	return err
}

// BeginChatAnswer fences a delivery claim before a fresh answer attempt. Answer
// text is never reused after a crash during generation; only completed replies
// are saved. An exhausted row is left for the caller to give a failure reply.
func (db *DB) BeginChatAnswer(ctx context.Context, r ChatReply, repoID int64, at time.Time) (ChatReply, error) {
	if (r.Command != "ask" && r.Command != "draft" && r.Command != "confirm") || r.AnswerAttempts >= 2 {
		return r, errors.New("chat answer attempts exhausted")
	}
	var repository any
	if repoID > 0 {
		repository = repoID
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='answering',answer_attempts=answer_attempts+1,resolved_repo_id=?,next_attempt_at=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=? AND answer_attempts<2`, repository, FormatTime(at.Add(5*time.Minute)), r.Transport, r.MessageID, r.Attempts)
	if err := chatAffected(result, err); err != nil {
		return r, err
	}
	r.State = "answering"
	r.AnswerAttempts++
	r.ResolvedRepoID = repoID
	return r, nil
}

// CompleteChatAnswer transitions a fenced answering row to ordinary delivery.
func (db *DB) CompleteChatAnswer(ctx context.Context, r ChatReply, text string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='sent',reply_text=?,outcome_status=? WHERE transport=? AND message_id=? AND state='answering' AND attempts=? AND answer_attempts=?`, text, r.OutcomeStatus, r.Transport, r.MessageID, r.Attempts, r.AnswerAttempts)
	return chatAffected(result, err)
}

// ResolveChatProject retains only the repository identity during generation.
func (db *DB) ResolveChatProject(ctx context.Context, r ChatReply, repoID int64) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET resolved_repo_id=? WHERE transport=? AND message_id=? AND state='answering' AND attempts=?`, repoID, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// ListTaskSessionsByRepo reads a bounded current task catalog. Reading one extra
// row lets callers explicitly report omissions without an unbounded SQL cursor.
func (db *DB) ListTaskSessionsByRepo(ctx context.Context, repoID int64, limit int) ([]Session, bool, error) {
	if repoID < 1 || limit < 1 || limit > 1000 {
		return nil, false, errors.New("invalid project task read")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE repo_id=? ORDER BY updated_at DESC,id DESC LIMIT ?`, repoID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, false, err
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(sessions) > limit
	return sessions[:min(limit, len(sessions))], more, nil
}

// LatestTaskRun reads one latest run without retaining a database cursor.
func (db *DB) LatestTaskRun(ctx context.Context, sessionID int64) (Run, error) {
	return scanRun(db.sql.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE session_id=? ORDER BY ordinal DESC LIMIT 1`, sessionID))
}

// StartChatMeterAttempt uses the same ledger and pinned catalog as other
// invocations, but has a repository owner and neither a run nor a task owner.
func (db *DB) StartChatMeterAttempt(ctx context.Context, repoID int64, provider string) (MeterAttempt, error) {
	if repoID < 1 || meterIdentity(provider) != nil {
		return MeterAttempt{}, errors.New("invalid chat meter identity")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), ChatRepoID: repoID, CodingAgent: provider}
	err := db.sql.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,chat_repo_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,1,?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING price_revision_id`, a.ID, repoID, provider, FormatTime(nowUTC())).Scan(&a.PriceRevisionID)
	a.Ordinal = 1
	return a, err
}
