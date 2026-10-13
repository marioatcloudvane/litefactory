package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ClaimConversationReply leases the earliest unresolved reply of an unpaused
// channel. A delayed predecessor blocks only its channel, allowing others ahead.
func (db *DB) ClaimConversationReply(ctx context.Context, transport string, at time.Time) (ChatReply, error) {
	var out ChatReply
	e := db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND command='conversation' AND state IN ('pending','sent') AND next_attempt_at<=? AND EXISTS(SELECT 1 FROM chat_conversation_turns t JOIN chat_conversations c ON c.id=t.conversation_id WHERE t.reply_id=chat_inbox.message_id AND c.transport=chat_inbox.transport AND c.delivery_paused=0 AND NOT EXISTS(SELECT 1 FROM chat_conversation_turns p JOIN chat_conversations pc ON pc.id=p.conversation_id WHERE pc.transport=c.transport AND pc.channel_ref=c.channel_ref AND p.delivered_at IS NULL AND (pc.created_at<c.created_at OR (pc.id=c.id AND p.sequence<t.sequence)))) ORDER BY created_at,message_id LIMIT 1`, transport, FormatTime(at)))
		if e != nil {
			return e
		}
		r.Attempts++
		r.State = "sent"
		r.NextAttemptAt = at.Add(time.Minute)
		_, e = tx.ExecContext(ctx, `UPDATE chat_inbox SET state='sent',attempts=?,next_attempt_at=? WHERE transport=? AND message_id=?`, r.Attempts, FormatTime(r.NextAttemptAt), r.Transport, r.MessageID)
		out = r
		return e
	})
	return out, e
}

// FinishConversationReply records remote acknowledgment while keeping stable
// delivery metadata for replay protection. Reply bodies use normal retention.
func (db *DB) FinishConversationReply(ctx context.Context, r ChatReply, discordID string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, e := tx.ExecContext(ctx, `UPDATE chat_inbox SET state='confirmed',finished_at=? WHERE transport=? AND message_id=? AND command='conversation' AND state='sent' AND attempts=?`, FormatTime(at), r.Transport, r.MessageID, r.Attempts)
		if e = chatAffected(result, e); e != nil {
			return e
		}
		result, e = tx.ExecContext(ctx, `UPDATE chat_conversation_turns SET delivered_at=?,discord_message_id=? WHERE reply_id=? AND delivered_at IS NULL AND conversation_id IN (SELECT id FROM chat_conversations WHERE transport=?)`, FormatTime(at), discordID, r.MessageID, r.Transport)
		return chatAffected(result, e)
	})
}

// PauseConversationReply preserves the saved answer after permanent refusal.
func (db *DB) PauseConversationReply(ctx context.Context, r ChatReply, reason string, at time.Time) error {
	if reason == "" {
		return errors.New("conversation pause needs a reason")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, e := tx.ExecContext(ctx, `UPDATE chat_inbox SET state='pending',last_error_at=? WHERE transport=? AND message_id=? AND command='conversation' AND state='sent' AND attempts=?`, FormatTime(at), r.Transport, r.MessageID, r.Attempts)
		if e = chatAffected(result, e); e != nil {
			return e
		}
		result, e = tx.ExecContext(ctx, `UPDATE chat_conversations SET health_reason=?,delivery_paused=1,updated_at=? WHERE transport=? AND id IN (SELECT conversation_id FROM chat_conversation_turns WHERE reply_id=?)`, reason, FormatTime(at), r.Transport, r.MessageID)
		return chatAffected(result, e)
	})
}

// PurgeConversationBodies removes completed, delivered prompt snapshots, never native
// state, pending work, source identities, sequences, or delivery metadata.
func (db *DB) PurgeConversationBodies(ctx context.Context, at time.Time) error {
	_, e := db.sql.ExecContext(ctx, `UPDATE chat_conversation_turns SET prompt='' WHERE state='completed' AND delivered_at IS NOT NULL AND delivered_at<=? AND NOT EXISTS(SELECT 1 FROM chat_conversation_actions a JOIN chat_natural_dialogues d ON d.code=a.dialogue_id AND d.revision=a.dialogue_revision WHERE a.turn_id=chat_conversation_turns.id AND EXISTS(SELECT 1 FROM chat_conversations c WHERE c.id=chat_conversation_turns.conversation_id AND c.transport=d.transport AND c.owner_id=d.owner_id AND c.channel_ref=d.channel_ref) AND d.state IN ('awaiting_confirmation','authorized','executing'))`, FormatTime(at.Add(-24*time.Hour)))
	return e
}
