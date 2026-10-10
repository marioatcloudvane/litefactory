package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ChatReply contains only the durable command and delivery state, never tokens.
type ChatReply struct {
	Transport, MessageID, ChannelRef, Command, State, ReplyText, TargetRef string
	Attempts                                                               int
	NextAttemptAt, CreatedAt, FinishedAt, LastErrorAt                      time.Time
}

const chatColumns = `transport,message_id,channel_ref,command,state,reply_text,attempts,next_attempt_at,created_at,finished_at,last_error_at,target_ref`

func scanChat(row scanner) (ChatReply, error) {
	var r ChatReply
	var next, created string
	var finished, last sql.NullString
	err := row.Scan(&r.Transport, &r.MessageID, &r.ChannelRef, &r.Command, &r.State, &r.ReplyText, &r.Attempts, &next, &created, &finished, &last, &r.TargetRef)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
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
	result, err := db.sql.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,next_attempt_at,created_at) VALUES(?,?,?,?,'pending',?,?) ON CONFLICT(transport,message_id) DO NOTHING`, r.Transport, r.MessageID, r.ChannelRef, r.Command, FormatTime(r.CreatedAt), FormatTime(r.CreatedAt))
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
		r, err = scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND state IN ('pending','sent') AND next_attempt_at<=? ORDER BY created_at,message_id LIMIT 1`, transport, FormatTime(at)))
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
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET reply_text=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=?`, text, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// FinishChatReply records a terminal outcome and clears the answer.
func (db *DB) FinishChatReply(ctx context.Context, r ChatReply, failed bool, at time.Time) error {
	state := "confirmed"
	var last any
	if failed {
		state = "failed"
		last = FormatTime(at)
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state=?,reply_text='',target_ref='',finished_at=?,last_error_at=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=?`, state, FormatTime(at), last, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
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
