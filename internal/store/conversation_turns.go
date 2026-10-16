package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ConversationTurn journals the launch boundary and durable delivery identity.
type ConversationTurn struct {
	ID, ConversationID, SourceID, Source, ReplyTo, Prompt, State, Worker, ReplyID, DiscordMessageID string
	Sequence, Generation                                                                            int64
	LeaseUntil, CreatedAt, FinishedAt, DeliveredAt                                                  time.Time
}

const conversationTurnColumns = `id,conversation_id,source_id,source,reply_to,prompt,state,worker,reply_id,discord_message_id,sequence,generation,lease_until,created_at,finished_at,delivered_at`

func scanConversationTurn(row scanner) (ConversationTurn, error) {
	var t ConversationTurn
	var lease, created, finished, delivered sql.NullString
	e := row.Scan(&t.ID, &t.ConversationID, &t.SourceID, &t.Source, &t.ReplyTo, &t.Prompt, &t.State, &t.Worker, &t.ReplyID, &t.DiscordMessageID, &t.Sequence, &t.Generation, &lease, &created, &finished, &delivered)
	if errors.Is(e, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if e != nil {
		return t, e
	}
	for _, f := range []struct {
		dst *time.Time
		src sql.NullString
	}{{&t.LeaseUntil, lease}, {&t.CreatedAt, created}, {&t.FinishedAt, finished}, {&t.DeliveredAt, delivered}} {
		*f.dst, e = scanTime(f.src)
		if e != nil {
			return t, e
		}
	}
	return t, nil
}

// AdmitConversationTurn consumes exactly the oldest released intake row and
// snapshots its redacted envelope atomically. Replay never creates another turn.
func (db *DB) AdmitConversationTurn(ctx context.Context, id, source, prompt string, at time.Time) (ConversationTurn, error) {
	var out ConversationTurn
	if prompt == "" || len(prompt) > 262144 || at.IsZero() {
		return out, errors.New("invalid conversation prompt")
	}
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id=? AND active=1`, id))
		if e != nil {
			return e
		}
		out, e = scanConversationTurn(tx.QueryRowContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE conversation_id=? AND source_id=?`, id, source))
		if e == nil {
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var oldest, owner, sourceText, replyTo string
		if e = tx.QueryRowContext(ctx, `SELECT source_id,owner_id,content,reply_to FROM chat_natural_messages WHERE channel_ref=? AND state='pending' ORDER BY length(source_id),source_id LIMIT 1`, c.ChannelRef).Scan(&oldest, &owner, &sourceText, &replyTo); errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if oldest != source || owner != c.OwnerID {
			return errors.New("conversation admission order or owner mismatch")
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversation_turns WHERE conversation_id=? AND delivered_at IS NULL`, id).Scan(&n); e != nil {
			return e
		}
		if n >= 1000 {
			return errors.New("conversation queue backpressure")
		}
		turnID, e := conversationID()
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO chat_conversation_turns(id,conversation_id,source_id,source,reply_to,sequence,prompt,created_at) VALUES(?,?,?,?,?,?,?,?)`, turnID, id, source, sourceText, replyTo, c.NextSequence, prompt, FormatTime(at))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE chat_conversations SET next_sequence=next_sequence+1 WHERE id=?`, id)
		if e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `UPDATE chat_natural_messages SET state='finished',content='',reply_to='',finished_at=? WHERE channel_ref=? AND source_id=? AND state='pending'`, FormatTime(at), c.ChannelRef, source)
		if e = chatAffected(r, e); e != nil {
			return e
		}
		out, e = scanConversationTurn(tx.QueryRowContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE id=?`, turnID))
		return e
	})
	return out, e
}

// ClaimConversationTurn never overtakes an unresolved predecessor. Expired
// running turns are recovered explicitly, never automatically replayed.
func (db *DB) ClaimConversationTurn(ctx context.Context, id, worker string, at time.Time, lease time.Duration) (ConversationTurn, error) {
	var out ConversationTurn
	if worker == "" || lease <= 0 {
		return out, errors.New("invalid conversation lease")
	}
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		t, e := scanConversationTurn(tx.QueryRowContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE conversation_id=? AND state='pending' AND NOT EXISTS(SELECT 1 FROM chat_conversation_turns previous WHERE previous.conversation_id=chat_conversation_turns.conversation_id AND previous.sequence<chat_conversation_turns.sequence AND previous.delivered_at IS NULL) AND EXISTS(SELECT 1 FROM chat_conversations c WHERE c.id=conversation_id AND c.active=1 AND c.health_reason='') ORDER BY sequence LIMIT 1`, id))
		if e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `UPDATE chat_conversation_turns SET state='running',worker=?,generation=generation+1,lease_until=? WHERE id=? AND state='pending'`, worker, FormatTime(at.Add(lease)), t.ID)
		if e = chatAffected(r, e); e != nil {
			return e
		}
		out, e = scanConversationTurn(tx.QueryRowContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE id=?`, t.ID))
		return e
	})
	return out, e
}

// SaveConversationSession fences identity persistence before accepting output.
func (db *DB) SaveConversationSession(ctx context.Context, t ConversationTurn, handle string) error {
	if handle == "" || len(handle) > 256 {
		return errors.New("invalid conversation session")
	}
	r, e := db.sql.ExecContext(ctx, `UPDATE chat_conversations SET session_handle=? WHERE id=? AND session_handle IN ('',?) AND EXISTS(SELECT 1 FROM chat_conversation_turns WHERE id=? AND conversation_id=? AND state='running' AND generation=? AND worker=?)`, handle, t.ConversationID, handle, t.ID, t.ConversationID, t.Generation, t.Worker)
	return chatAffected(r, e)
}

// CompleteConversationTurn saves exact reply bytes and outbox identity in one
// fenced transaction. Only a completed answer requires a known native session.
func (db *DB) CompleteConversationTurn(ctx context.Context, t ConversationTurn, state, reply string, at time.Time) error {
	return db.completeConversationTurn(ctx, t, state, reply, at, false, false)
}

// CompleteConversationHandledTurn saves a deterministic factory response before
// a native session exists. It refuses turns with any provider launch journal.
func (db *DB) CompleteConversationHandledTurn(ctx context.Context, t ConversationTurn, reply string, at time.Time) error {
	return db.completeConversationTurn(ctx, t, "completed", reply, at, true, false)
}
func (db *DB) completeConversationTurn(ctx context.Context, t ConversationTurn, state, reply string, at time.Time, handled, preStart bool) error {
	if (state != "completed" && state != "interrupted" && state != "unavailable") || reply == "" || len(reply) > 65536 {
		return errors.New("invalid conversation outcome")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id=?`, t.ConversationID))
		if e != nil {
			return e
		}
		if handled {
			var n int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversation_exchanges WHERE turn_id=? AND kind='provider'`, t.ID).Scan(&n); e != nil {
				return e
			}
			if n != 0 {
				return errors.New("factory handled turn contains provider launch")
			}
		}
		if state == "completed" && c.SessionHandle == "" && !handled {
			return errors.New("conversation completed without session identity")
		}
		replyID := "conversation:" + t.ID
		r, e := tx.ExecContext(ctx, `UPDATE chat_conversation_turns SET state=?,reply_id=?,finished_at=?,lease_until=NULL WHERE id=? AND conversation_id=? AND state='running' AND generation=? AND worker=?`, state, replyID, FormatTime(at), t.ID, t.ConversationID, t.Generation, t.Worker)
		if e = chatAffected(r, e); e != nil {
			return e
		}
		if state == "unavailable" || (state == "interrupted" && c.SessionHandle == "" && !preStart) {
			_, e = tx.ExecContext(ctx, `UPDATE chat_conversations SET health_reason=CASE WHEN health_reason='' THEN 'Conversation continuity unavailable; native session recovery is required.' ELSE health_reason END,updated_at=? WHERE id=?`, FormatTime(at), c.ID)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,next_attempt_at,reply_text,created_at) VALUES(?,?,?,'conversation','pending',?,?,?)`, c.Transport, replyID, c.ChannelRef, FormatTime(at), reply, FormatTime(at))
		return e
	})
}

// RecoverConversationTurns returns unfinished launches. The caller must first
// establish that the previous native process has exited, then fence completion
// with the returned ownership identity. A lease alone is never proof of exit.
func (db *DB) RecoverConversationTurns(ctx context.Context, id string, at time.Time) ([]ConversationTurn, error) {
	rows, e := db.sql.QueryContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE conversation_id=? AND state='running' AND lease_until<=? ORDER BY sequence`, id, FormatTime(at))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ConversationTurn
	for rows.Next() {
		t, e := scanConversationTurn(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// LatestConversationTurn reads the latest admitted turn without claiming or
// changing it, so subsequent prompts can describe an interruption honestly.
func (db *DB) LatestConversationTurn(ctx context.Context, conversationID string) (ConversationTurn, error) {
	return scanConversationTurn(db.sql.QueryRowContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE conversation_id=? ORDER BY sequence DESC LIMIT 1`, conversationID))
}

// RenewConversationTurn verifies fenced launch ownership under the provider's
// native state lock. A completed or recovered turn cannot launch another input.
func (db *DB) RenewConversationTurn(ctx context.Context, turn ConversationTurn, at time.Time, lease time.Duration) error {
	if at.IsZero() || lease <= 0 {
		return errors.New("invalid conversation lease")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET lease_until=? WHERE id=? AND conversation_id=? AND state='running' AND generation=? AND worker=?`, FormatTime(at.Add(lease)), turn.ID, turn.ConversationID, turn.Generation, turn.Worker)
	return chatAffected(result, err)
}

// DeferConversationTurn yields after a proven pre-launch failure or a journaled
// factory boundary whose completed provider inputs will not be replayed.
// It must never be used when provider completion is uncertain.
func (db *DB) DeferConversationTurn(ctx context.Context, turn ConversationTurn) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET state='pending',worker='',lease_until=NULL WHERE id=? AND conversation_id=? AND state='running' AND generation=? AND worker=?`, turn.ID, turn.ConversationID, turn.Generation, turn.Worker)
	return chatAffected(result, err)
}

// CompleteConversationPreStartTurn persists a failed input without pausing native
// continuity. The caller must have affirmative proof no process started.
func (db *DB) CompleteConversationPreStartTurn(ctx context.Context, t ConversationTurn, reply string, at time.Time) error {
	return db.completeConversationTurn(ctx, t, "interrupted", reply, at, false, true)
}
