package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ChatReply contains only the durable command and delivery state, never tokens.
type ChatReply struct {
	OutcomeStatus                                                          string
	Transport, MessageID, ChannelRef, Command, State, ReplyText, TargetRef string
	PresentationState, PresentationOutcome, ReplyAttachment                string
	PresentationGeneration, PresentationCheck                              bool
	Arguments                                                              map[string]string
	Question, ProjectOption                                                string
	ResolvedRepoID                                                         int64
	AnswerAttempts                                                         int
	Attempts                                                               int
	NextAttemptAt, CreatedAt, FinishedAt, LastErrorAt                      time.Time
}

const chatColumns = `transport,message_id,channel_ref,command,arguments,state,reply_text,attempts,next_attempt_at,created_at,finished_at,last_error_at,question,project_option,COALESCE(resolved_repo_id,0),answer_attempts,target_ref,presentation_state,presentation_outcome,presentation_generation,presentation_check,reply_attachment,outcome_status`

func scanChat(row scanner) (ChatReply, error) {
	var r ChatReply
	var next, created, arguments string
	var finished, last sql.NullString
	err := row.Scan(&r.Transport, &r.MessageID, &r.ChannelRef, &r.Command, &arguments, &r.State, &r.ReplyText, &r.Attempts, &next, &created, &finished, &last, &r.Question, &r.ProjectOption, &r.ResolvedRepoID, &r.AnswerAttempts, &r.TargetRef, &r.PresentationState, &r.PresentationOutcome, &r.PresentationGeneration, &r.PresentationCheck, &r.ReplyAttachment, &r.OutcomeStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(arguments), &r.Arguments); err != nil {
		return r, err
	}
	for _, f := range []struct {
		dst *time.Time
		src sql.NullString
	}{{&r.NextAttemptAt, sql.NullString{String: next, Valid: true}}, {&r.CreatedAt, sql.NullString{String: created, Valid: true}}, {&r.FinishedAt, finished}, {&r.LastErrorAt, last}} {
		if *f.dst, err = scanTime(f.src); err != nil {
			return r, err
		}
	}
	return r, nil
}

// AdmitChatReply admits an interaction once. Replays change nothing.
func (db *DB) AdmitChatReply(ctx context.Context, r ChatReply) (bool, error) {
	if r.Transport == "" || r.MessageID == "" || r.ChannelRef == "" || r.Command == "" || r.CreatedAt.IsZero() {
		return false, errors.New("invalid chat admission")
	}
	state := "pending"
	question, project := "", ""
	if r.Command == "ask" || r.Command == "draft" || r.Command == "confirm" {
		question, project = r.Arguments["question"], r.Arguments["project"]
		if r.Command == "draft" {
			question = r.Arguments["note"]
		}
		if r.Command == "confirm" {
			question = r.Arguments["code"]
		}
		if r.ReplyText == "" && len(question) <= 8192 && len(project) <= 256 && (question != "" || r.Command == "draft") {
			state = "answering"
		} else {
			question, project = "", ""
			r.Arguments = nil
			if r.ReplyText == "" {
				r.ReplyText = "Please give a question up to 8192 bytes and a project up to 256 bytes."
			}
		}
		// Chat model inputs and confirm codes use temporary columns, never a generic argument archive.
		r.Arguments = nil
	}
	arguments, err := json.Marshal(r.Arguments)
	if err != nil {
		return false, err
	}
	result, err := db.sql.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,arguments,state,next_attempt_at,created_at,question,project_option,reply_text,presentation_state,outcome_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(transport,message_id) DO NOTHING`, r.Transport, r.MessageID, r.ChannelRef, r.Command, string(arguments), state, FormatTime(r.CreatedAt), FormatTime(r.CreatedAt), question, project, r.ReplyText, r.PresentationState, r.OutcomeStatus)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// ReadChatReply returns one durable reply or ErrNotFound.
func (db *DB) ReadChatReply(ctx context.Context, transport, id string) (ChatReply, error) {
	return scanChat(db.sql.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND message_id=?`, transport, id))
}

// ClaimChatReply leases a due reply. The attempt number fences stale workers.
func (db *DB) ClaimChatReply(ctx context.Context, transport string, at time.Time) (ChatReply, error) {
	var r ChatReply
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		r, err = scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND command<>'conversation' AND state IN ('answering','pending','sent') AND next_attempt_at<=? ORDER BY created_at,message_id LIMIT 1`, transport, FormatTime(at)))
		if err != nil {
			return err
		}
		r.Attempts++
		r.State = "sent"
		r.NextAttemptAt = at.Add(time.Minute)
		_, err = tx.ExecContext(ctx, `UPDATE chat_inbox SET state='sent',attempts=?,next_attempt_at=? WHERE transport=? AND message_id=?`, r.Attempts, FormatTime(r.NextAttemptAt), r.Transport, r.MessageID)
		return err
	})
	return r, err
}

// SaveChatReply fixes the answer before external I/O so retries use the same text.
func (db *DB) SaveChatReply(ctx context.Context, r ChatReply, text string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET reply_text=?,outcome_status=?,resolved_repo_id=COALESCE(NULLIF(?,0),resolved_repo_id) WHERE transport=? AND message_id=? AND command<>'conversation' AND state='sent' AND attempts=? AND presentation_outcome='' AND presentation_state IN ('','pending')`, text, r.OutcomeStatus, r.ResolvedRepoID, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// FinishChatReply records a terminal outcome and clears the answer. A
// delivered /ask reply keeps its question, project and reply text until the
// retention purge, so follow-up /ask turns can read local history. Every
// delivered reply keeps its raw details until the purge for /details.
func (db *DB) FinishChatReply(ctx context.Context, r ChatReply, failed bool, at time.Time) error {
	state := "confirmed"
	var last any
	if failed {
		state = "failed"
		last = FormatTime(at)
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state=?1,reply_text=CASE WHEN ?1='confirmed' AND command='ask' THEN reply_text ELSE '' END,target_ref='',arguments='{}',question=CASE WHEN ?1='confirmed' AND command='ask' THEN question ELSE '' END,project_option=CASE WHEN ?1='confirmed' AND command='ask' THEN project_option ELSE '' END,resolved_repo_id=NULL,presentation_state='',presentation_outcome='',presentation_generation=0,presentation_check=0,reply_attachment=CASE WHEN ?1='confirmed' THEN reply_attachment ELSE '' END,outcome_status='',finished_at=?2,last_error_at=?3 WHERE transport=?4 AND message_id=?5 AND command<>'conversation' AND state='sent' AND attempts=?6`, state, FormatTime(at), last, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// ReadAskReplies returns delivered /ask rows of one transport channel, newest
// first. Only rows within the existing retention window remain.
func (db *DB) ReadAskReplies(ctx context.Context, transport, channel string, limit int) ([]ChatReply, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND channel_ref=? AND command='ask' AND state='confirmed' ORDER BY finished_at DESC,message_id DESC LIMIT ?`, transport, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatReply
	for rows.Next() {
		r, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReadLatestReplyDetails returns the raw details of the newest delivered reply
// in one transport channel, excluding /details replies themselves, or
// ErrNotFound. Only rows within the existing retention window remain.
func (db *DB) ReadLatestReplyDetails(ctx context.Context, transport, channel string) (string, error) {
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT reply_attachment FROM chat_inbox WHERE transport=? AND channel_ref=? AND state='confirmed' AND reply_attachment<>'' AND command<>'details' ORDER BY finished_at DESC,message_id DESC LIMIT 1`, transport, channel).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return raw, err
}

// RetryChatReply records timing only; remote errors may contain credentials.
func (db *DB) RetryChatReply(ctx context.Context, r ChatReply, next, at time.Time) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='pending',next_attempt_at=?,last_error_at=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=?`, FormatTime(next), FormatTime(at), r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// PurgeChatReplies removes terminal rows after 24 hours.
func (db *DB) PurgeChatReplies(ctx context.Context, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM chat_inbox WHERE state IN ('confirmed','failed') AND finished_at<=?`, FormatTime(at.Add(-24*time.Hour)))
	return err
}
func chatAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}
