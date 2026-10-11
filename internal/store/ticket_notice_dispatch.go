package store

import (
	"context"
	"database/sql"
	"fmt"
)

// TicketNoticeDispatch retains auxiliary report bytes alongside their existing
// effect identity. Its owning workflow remains responsible for acknowledgement.
type TicketNoticeDispatch struct {
	Body      string
	Attempted bool
	ReplyID   string
}

func scanTicketNotice(row scanner) (TicketNoticeDispatch, error) {
	var n TicketNoticeDispatch
	err := row.Scan(&n.Body, &n.Attempted, &n.ReplyID)
	return n, err
}

// PrepareTicketNotice freezes bytes without consuming the dispatch right.
// Failed observations before dispatch can therefore be retried safely.
func (db *DB) PrepareTicketNotice(ctx context.Context, key, body string) (n TicketNoticeDispatch, err error) {
	if body == "" {
		return n, fmt.Errorf("empty ticket notice")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var remoteID, actorID string
		if e := tx.QueryRowContext(ctx, `SELECT remote_id,actor_id FROM ticket_effect_identities WHERE effect_key=?`, key).Scan(&remoteID, &actorID); e != nil {
			return e
		}
		if remoteID == "" || actorID == "" {
			return ErrTicketIdentityConflict
		}
		var e error
		n, e = scanTicketNotice(tx.QueryRowContext(ctx, `SELECT notice_body,notice_attempted,notice_reply_id FROM ticket_effect_identities WHERE effect_key=?`, key))
		if e != nil {
			return e
		}
		if n.Body != "" && n.Body != body {
			return ErrTicketIdentityConflict
		}
		if n.Body == "" {
			if _, e = tx.ExecContext(ctx, `UPDATE ticket_effect_identities SET notice_body=? WHERE effect_key=?`, body, key); e != nil {
				return e
			}
			n.Body = body
		}
		return nil
	})
	return
}

// ClaimTicketNotice freezes prepared bytes and grants at most one dispatch.
func (db *DB) ClaimTicketNotice(ctx context.Context, key, body string) (n TicketNoticeDispatch, claimed bool, err error) {
	if body == "" {
		return n, false, fmt.Errorf("empty ticket notice")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		var remoteID, actorID string
		if e = tx.QueryRowContext(ctx, `SELECT remote_id,actor_id FROM ticket_effect_identities WHERE effect_key=?`, key).Scan(&remoteID, &actorID); e != nil {
			return e
		}
		if remoteID == "" || actorID == "" {
			return ErrTicketIdentityConflict
		}
		n, e = scanTicketNotice(tx.QueryRowContext(ctx, `SELECT notice_body,notice_attempted,notice_reply_id FROM ticket_effect_identities WHERE effect_key=?`, key))
		if e != nil {
			return e
		}
		if n.Body != "" && n.Body != body {
			return ErrTicketIdentityConflict
		}
		if n.Attempted {
			return nil
		}
		_, e = tx.ExecContext(ctx, `UPDATE ticket_effect_identities SET notice_body=?,notice_attempted=1 WHERE effect_key=?`, body, key)
		if e != nil {
			return e
		}
		n.Body, n.Attempted, claimed = body, true, true
		return nil
	})
	return
}

// ConfirmTicketNotice retains the immutable receipt without advancing sources.
func (db *DB) ConfirmTicketNotice(ctx context.Context, key, replyID string) error {
	if replyID == "" {
		return ErrTicketIdentityConflict
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		n, err := scanTicketNotice(tx.QueryRowContext(ctx, `SELECT notice_body,notice_attempted,notice_reply_id FROM ticket_effect_identities WHERE effect_key=?`, key))
		if err != nil {
			return err
		}
		if !n.Attempted || n.Body == "" || (n.ReplyID != "" && n.ReplyID != replyID) {
			return ErrTicketIdentityConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_effect_identities SET notice_reply_id=? WHERE effect_key=?`, replyID, key)
		return err
	})
}
