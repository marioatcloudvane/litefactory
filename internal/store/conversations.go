package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// ErrConversationDrain means queued work must finish before switching providers.
var ErrConversationDrain = errors.New("conversation provider change requires drain")

// Conversation is a durable channel binding. Its name is a label, never a path.
type Conversation struct {
	ID, Transport, ChannelRef, OwnerID, Name, Provider, SessionHandle, StateDir, HealthReason string
	Revision, NextSequence                                                                    int64
	Active, DeliveryPaused                                                                    bool
}

const conversationColumns = `id,transport,channel_ref,owner_id,name,provider,session_handle,state_dir,health_reason,revision,next_sequence,active,delivery_paused`

func scanConversation(row scanner) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.Transport, &c.ChannelRef, &c.OwnerID, &c.Name, &c.Provider, &c.SessionHandle, &c.StateDir, &c.HealthReason, &c.Revision, &c.NextSequence, &c.Active, &c.DeliveryPaused)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}
func conversationID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}

// ReadConversation reads the active binding, including a paused binding.
func (db *DB) ReadConversation(ctx context.Context, transport, channel string) (Conversation, error) {
	return scanConversation(db.sql.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE transport=? AND channel_ref=? AND active=1`, transport, channel))
}

// ListConversations lists active bindings independently of current configuration.
func (db *DB) ListConversations(ctx context.Context) ([]Conversation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE active=1 ORDER BY transport,channel_ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		c, e := scanConversation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EnsureConversation retains identity on rename. A drained provider switch creates
// a fresh binding and keeps the former provider's native state untouched.
func (db *DB) EnsureConversation(ctx context.Context, c Conversation, at time.Time) (Conversation, error) {
	if c.Transport == "" || c.ChannelRef == "" || c.OwnerID == "" || c.Name == "" || len(c.Name) > 256 || c.StateDir == "" || (c.Provider != "claude" && c.Provider != "codex") || at.IsZero() {
		return c, errors.New("invalid conversation binding")
	}
	var out Conversation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, e := scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE transport=? AND channel_ref=? AND active=1`, c.Transport, c.ChannelRef))
		if e == nil {
			if old.OwnerID != c.OwnerID {
				return errors.New("conversation owner changed")
			}
			if old.Provider == c.Provider {
				_, e = tx.ExecContext(ctx, `UPDATE chat_conversations SET name=?,revision=revision+CASE WHEN name<>? THEN 1 ELSE 0 END,updated_at=? WHERE id=?`, c.Name, c.Name, FormatTime(at), old.ID)
				if e != nil {
					return e
				}
				out, e = scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id=?`, old.ID))
				return e
			}
			var n int
			if e = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM chat_conversation_turns WHERE conversation_id=? AND delivered_at IS NULL)+(SELECT count(*) FROM chat_natural_messages WHERE channel_ref=? AND state IN ('pending','staged'))+(SELECT count(*) FROM chat_natural_dialogues WHERE transport=? AND owner_id=? AND channel_ref=? AND (state IN ('authorized','executing') OR (state IN ('clarifying','awaiting_confirmation') AND expires_at>?)))+(SELECT count(*) FROM chat_inbox WHERE conversation_turn_id IN (SELECT id FROM chat_conversation_turns WHERE conversation_id=?) AND state IN ('pending','sent','answering','working'))`, old.ID, old.ChannelRef, old.Transport, old.OwnerID, old.ChannelRef, FormatTime(at), old.ID).Scan(&n); e != nil {
				return e
			}
			if n != 0 {
				return ErrConversationDrain
			}
			if _, e = tx.ExecContext(ctx, `UPDATE chat_conversations SET active=0,updated_at=? WHERE id=?`, FormatTime(at), old.ID); e != nil {
				return e
			}
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		if c.ID == "" {
			c.ID, e = conversationID()
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO chat_conversations(id,transport,channel_ref,owner_id,name,provider,state_dir,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, c.ID, c.Transport, c.ChannelRef, c.OwnerID, c.Name, c.Provider, c.StateDir, FormatTime(at), FormatTime(at))
		if e != nil {
			return e
		}
		out, e = scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id=?`, c.ID))
		return e
	})
	return out, err
}

// SetConversationHealth pauses a binding while preserving all pending state.
func (db *DB) SetConversationHealth(ctx context.Context, id, reason string, at time.Time) error {
	r, e := db.sql.ExecContext(ctx, `UPDATE chat_conversations SET health_reason=?,delivery_paused=CASE WHEN ?='' THEN 0 ELSE delivery_paused END,updated_at=? WHERE id=?`, reason, reason, FormatTime(at), id)
	return chatAffected(r, e)
}
