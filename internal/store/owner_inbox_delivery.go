package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const inboxWithheldNotice = "This saved inbox response is no longer available because its reference expired or source access changed. Refresh the inbox to see current authorized results."

// SaveInboxWithheldReply records a safe delivery substitution under the current
// reply lease. Original answer bytes and browsing snapshots remain unchanged.
func (db *DB) SaveInboxWithheldReply(ctx context.Context, r ChatReply, at time.Time) (string, error) {
	var notice string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var current bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chat_inbox WHERE transport=? AND message_id=? AND channel_ref=? AND command IN ('conversation','conversation_details') AND state='sent' AND attempts=? AND next_attempt_at>?)`, r.Transport, r.MessageID, r.ChannelRef, r.Attempts, FormatTime(at)).Scan(&current)
		if err != nil {
			return err
		}
		if !current {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO owner_inbox_withheld_replies(transport,reply_id,notice,created_at) VALUES(?,?,?,?) ON CONFLICT(transport,reply_id) DO NOTHING`, r.Transport, r.MessageID, inboxWithheldNotice, FormatTime(at))
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT notice FROM owner_inbox_withheld_replies WHERE transport=? AND reply_id=?`, r.Transport, r.MessageID).Scan(&notice)
	})
	return notice, err
}

// ReadInboxWithheldReply preserves substituted delivery bytes across retries,
// even if source access is restored after the first attempted delivery.
func (db *DB) ReadInboxWithheldReply(ctx context.Context, r ChatReply) (string, error) {
	var notice string
	err := db.sql.QueryRowContext(ctx, `SELECT w.notice FROM owner_inbox_withheld_replies w JOIN chat_inbox c ON c.transport=w.transport AND c.message_id=w.reply_id WHERE w.transport=? AND w.reply_id=? AND c.channel_ref=? AND c.command IN ('conversation','conversation_details') AND c.state='sent' AND c.attempts=?`, r.Transport, r.MessageID, r.ChannelRef, r.Attempts).Scan(&notice)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return notice, err
}
