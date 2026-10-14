package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"
)

// ConversationAnswer freezes presentation identity separately from retained evidence.
type ConversationAnswer struct {
	ID, TurnID, Transport, OwnerID, ChannelRef, Blocks string
	Version, ProposalPart                              int
	CreatedAt, CompletedAt, ExpiredAt                  time.Time
}

// ConversationAnswerPart holds exact saved delivery bytes and acknowledgment metadata.
type ConversationAnswerPart struct {
	ReplyID, Text, Attachment, DiscordMessageID string
	Ordinal                                     int
	AcknowledgedAt                              time.Time
}

const answerColumns = `id,turn_id,transport,owner_id,channel_ref,blocks,version,proposal_part,created_at,completed_at,expired_at`

func scanConversationAnswer(row scanner) (ConversationAnswer, error) {
	var a ConversationAnswer
	var created string
	var completed, expired sql.NullString
	err := row.Scan(&a.ID, &a.TurnID, &a.Transport, &a.OwnerID, &a.ChannelRef, &a.Blocks, &a.Version, &a.ProposalPart, &created, &completed, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	for _, v := range []struct {
		dst *time.Time
		src sql.NullString
	}{{&a.CreatedAt, sql.NullString{String: created, Valid: true}}, {&a.CompletedAt, completed}, {&a.ExpiredAt, expired}} {
		*v.dst, err = scanTime(v.src)
		if err != nil {
			return a, err
		}
	}
	return a, nil
}

// CompleteConversationAnswer persists every immutable part before any network send.
func (db *DB) CompleteConversationAnswer(ctx context.Context, t ConversationTurn, state string, a ConversationAnswer, parts []ConversationAnswerPart, at time.Time) error {
	return db.completeConversationAnswer(ctx, t, state, a, parts, at, false)
}

// CompleteConversationHandledAnswer saves a factory response without launching a provider.
func (db *DB) CompleteConversationHandledAnswer(ctx context.Context, t ConversationTurn, a ConversationAnswer, parts []ConversationAnswerPart, at time.Time) error {
	return db.completeConversationAnswer(ctx, t, "completed", a, parts, at, true)
}

func (db *DB) completeConversationAnswer(ctx context.Context, t ConversationTurn, state string, a ConversationAnswer, parts []ConversationAnswerPart, at time.Time, handled bool) error {
	if a.ID == "" || len(a.ID) > 128 || a.Version < 1 || len(parts) < 1 || len(parts) > 1024 || a.ProposalPart < 0 || a.ProposalPart > len(parts) || at.IsZero() || (state != "completed" && state != "interrupted" && state != "unavailable") {
		return errors.New("invalid conversation answer")
	}
	for _, p := range parts {
		if p.Text == "" || len(utf16.Encode([]rune(p.Text))) > 2000 || len(p.Attachment) > 65536 {
			return errors.New("invalid conversation answer part")
		}
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		c, err := scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id=?`, t.ConversationID))
		if err != nil {
			return err
		}
		if handled {
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversation_exchanges WHERE turn_id=? AND kind='provider'`, t.ID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return errors.New("factory handled turn contains provider launch")
			}
		}
		if state == "completed" && c.SessionHandle == "" && !handled {
			return errors.New("conversation completed without session identity")
		}
		first := fmt.Sprintf("answer:%s:1", a.ID)
		r, err := tx.ExecContext(ctx, `UPDATE chat_conversation_turns SET state=?,reply_id=?,finished_at=?,lease_until=NULL WHERE id=? AND conversation_id=? AND state='running' AND generation=? AND worker=?`, state, first, FormatTime(at), t.ID, t.ConversationID, t.Generation, t.Worker)
		if err = chatAffected(r, err); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_answers(id,turn_id,transport,owner_id,channel_ref,blocks,version,proposal_part,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, a.ID, t.ID, c.Transport, c.OwnerID, c.ChannelRef, a.Blocks, a.Version, a.ProposalPart, FormatTime(at))
		if err != nil {
			return err
		}
		for i, p := range parts {
			id := fmt.Sprintf("answer:%s:%d", a.ID, i+1)
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,next_attempt_at,reply_text,reply_attachment,created_at,conversation_turn_id) VALUES(?,?,?,'conversation','pending',?,?,?,?,?)`, c.Transport, id, c.ChannelRef, FormatTime(at), p.Text, p.Attachment, FormatTime(at), t.ID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_answer_parts(answer_id,ordinal,transport,reply_id) VALUES(?,?,?,?)`, a.ID, i+1, c.Transport, id)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// ReadConversationAnswer scopes explicit references and fixes latest to acknowledged answers.
func (db *DB) ReadConversationAnswer(ctx context.Context, transport, owner, channel, selector string) (ConversationAnswer, error) {
	if selector != "" {
		return scanConversationAnswer(db.sql.QueryRowContext(ctx, `SELECT `+answerColumns+` FROM chat_conversation_answers WHERE transport=? AND owner_id=? AND channel_ref=? AND id=?`, transport, owner, channel, selector))
	}
	return scanConversationAnswer(db.sql.QueryRowContext(ctx, `SELECT `+answerColumns+` FROM chat_conversation_answers WHERE transport=? AND owner_id=? AND channel_ref=? AND EXISTS(SELECT 1 FROM chat_conversation_answer_parts p WHERE p.answer_id=chat_conversation_answers.id AND p.acknowledged_at IS NOT NULL) ORDER BY created_at DESC,rowid DESC LIMIT 1`, transport, owner, channel))
}

// ConversationReplyAnswer identifies an immutable delivery part and its owning answer.
func (db *DB) ConversationReplyAnswer(ctx context.Context, r ChatReply) (ConversationAnswer, int, error) {
	var id string
	var ordinal int
	err := db.sql.QueryRowContext(ctx, `SELECT answer_id,ordinal FROM chat_conversation_answer_parts WHERE transport=? AND reply_id=?`, r.Transport, r.MessageID).Scan(&id, &ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationAnswer{}, 0, ErrNotFound
	}
	if err != nil {
		return ConversationAnswer{}, 0, err
	}
	a, err := scanConversationAnswer(db.sql.QueryRowContext(ctx, `SELECT `+answerColumns+` FROM chat_conversation_answers WHERE id=?`, id))
	return a, ordinal, err
}

// ReadConversationAnswerParts lists saved answer parts in reading order.
func (db *DB) ReadConversationAnswerParts(ctx context.Context, id string) ([]ConversationAnswerPart, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT p.reply_id,p.ordinal,COALESCE(r.reply_text,''),COALESCE(r.reply_attachment,''),p.discord_message_id,p.acknowledged_at FROM chat_conversation_answer_parts p LEFT JOIN chat_inbox r ON r.transport=p.transport AND r.message_id=p.reply_id WHERE p.answer_id=? ORDER BY p.ordinal`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationAnswerPart
	for rows.Next() {
		var p ConversationAnswerPart
		var ack sql.NullString
		if err = rows.Scan(&p.ReplyID, &p.Ordinal, &p.Text, &p.Attachment, &p.DiscordMessageID, &ack); err != nil {
			return nil, err
		}
		p.AcknowledgedAt, err = scanTime(ack)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AdmitConversationDetails fixes latest selection in the same transaction as admission.
// The empty identity deliberately preserves the legacy command route.
func (db *DB) AdmitConversationDetails(ctx context.Context, r ChatReply, owner, selector string, at time.Time) (bool, error) {
	var admitted bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_inbox WHERE transport=? AND message_id=?`, r.Transport, r.MessageID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return nil
		}
		id := selector
		if selector == "" {
			err := tx.QueryRowContext(ctx, `SELECT id FROM chat_conversation_answers WHERE transport=? AND owner_id=? AND channel_ref=? AND EXISTS(SELECT 1 FROM chat_conversation_answer_parts p WHERE p.answer_id=chat_conversation_answers.id AND p.acknowledged_at IS NOT NULL) ORDER BY created_at DESC,rowid DESC LIMIT 1`, r.Transport, owner, r.ChannelRef).Scan(&id)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,arguments,state,next_attempt_at,created_at,presentation_state) VALUES(?,?,?,'details','null','pending',?,?,?)`, r.Transport, r.MessageID, r.ChannelRef, FormatTime(at), FormatTime(at), r.PresentationState)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_details(transport,request_id,answer_id) VALUES(?,?,?)`, r.Transport, r.MessageID, id)
		admitted = err == nil
		return err
	})
	return admitted, err
}

// ConversationDetailsSelection reads the selector fixed at command admission.
func (db *DB) ConversationDetailsSelection(ctx context.Context, r ChatReply) (string, error) {
	var id string
	err := db.sql.QueryRowContext(ctx, `SELECT answer_id FROM chat_conversation_details WHERE transport=? AND request_id=?`, r.Transport, r.MessageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// SaveConversationDetails saves the complete export once, and finishes its request.
func (db *DB) SaveConversationDetails(ctx context.Context, r ChatReply, parts []ConversationAnswerPart, at time.Time) error {
	if len(parts) < 1 || len(parts) > 1024 {
		return errors.New("invalid details parts")
	}
	for _, p := range parts {
		if p.Text == "" || len(utf16.Encode([]rune(p.Text))) > 2000 || len(p.Attachment) > 65536 {
			return errors.New("invalid details part")
		}
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE chat_inbox SET state='confirmed',finished_at=? WHERE transport=? AND message_id=? AND command='details' AND state='sent' AND attempts=? AND EXISTS(SELECT 1 FROM chat_conversation_details d WHERE d.transport=chat_inbox.transport AND d.request_id=chat_inbox.message_id)`, FormatTime(at), r.Transport, r.MessageID, r.Attempts)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		for i, p := range parts {
			id := fmt.Sprintf("details:%s:%d", r.MessageID, i+1)
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,next_attempt_at,created_at,reply_text,reply_attachment) VALUES(?,?,?,'conversation_details','pending',?,?,?,?)`, r.Transport, id, r.ChannelRef, FormatTime(at), FormatTime(at), p.Text, p.Attachment)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_details_parts(transport,request_id,reply_id,ordinal) VALUES(?,?,?,?)`, r.Transport, r.MessageID, id, i+1)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceConversationDetailsParts freezes inline fallback only after definite rejection.
// A fresh fenced claim cannot rewrite already acknowledged export bytes.
func (db *DB) ReplaceConversationDetailsParts(ctx context.Context, r ChatReply, parts []ConversationAnswerPart, at time.Time) error {
	if len(parts) < 1 || len(parts) > 1024 {
		return errors.New("invalid details fallback")
	}
	for _, p := range parts {
		if p.Attachment != "" || p.Text == "" || len(utf16.Encode([]rune(p.Text))) > 2000 {
			return errors.New("invalid inline details part")
		}
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var request string
		var ordinal int
		err := tx.QueryRowContext(ctx, `SELECT request_id,ordinal FROM chat_conversation_details_parts WHERE transport=? AND reply_id=? AND acknowledged_at IS NULL`, r.Transport, r.MessageID).Scan(&request, &ordinal)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var attempts int
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT attempts,state FROM chat_inbox WHERE transport=? AND message_id=?`, r.Transport, r.MessageID).Scan(&attempts, &state); err != nil {
			return err
		}
		if state != "sent" || attempts != r.Attempts {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM chat_inbox WHERE transport=? AND message_id IN (SELECT reply_id FROM chat_conversation_details_parts WHERE transport=? AND request_id=? AND ordinal>=? AND acknowledged_at IS NULL)`, r.Transport, r.Transport, request, ordinal)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM chat_conversation_details_parts WHERE transport=? AND request_id=? AND ordinal>=? AND acknowledged_at IS NULL`, r.Transport, request, ordinal)
		if err != nil {
			return err
		}
		for i, p := range parts {
			id := fmt.Sprintf("details:%s:inline:%d", request, ordinal+i)
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,next_attempt_at,created_at,reply_text) VALUES(?,?,?,'conversation_details','pending',?,?,?)`, r.Transport, id, r.ChannelRef, FormatTime(at), FormatTime(at), p.Text)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_details_parts(transport,request_id,reply_id,ordinal) VALUES(?,?,?,?)`, r.Transport, request, id, ordinal+i)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// ReadConversationDetailsParts returns saved bytes from the current unacknowledged part.
func (db *DB) ReadConversationDetailsParts(ctx context.Context, r ChatReply) ([]ConversationAnswerPart, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT p.reply_id,p.ordinal,r.reply_text,r.reply_attachment FROM chat_conversation_details_parts p JOIN chat_inbox r ON r.transport=p.transport AND r.message_id=p.reply_id WHERE p.transport=? AND p.request_id=(SELECT request_id FROM chat_conversation_details_parts WHERE transport=? AND reply_id=?) AND p.acknowledged_at IS NULL ORDER BY p.ordinal`, r.Transport, r.Transport, r.MessageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationAnswerPart
	for rows.Next() {
		var p ConversationAnswerPart
		if err = rows.Scan(&p.ReplyID, &p.Ordinal, &p.Text, &p.Attachment); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ConversationDetailsReplySelection identifies the selected answer for an export part.
func (db *DB) ConversationDetailsReplySelection(ctx context.Context, r ChatReply) (string, error) {
	var id string
	err := db.sql.QueryRowContext(ctx, `SELECT d.answer_id FROM chat_conversation_details d JOIN chat_conversation_details_parts p ON p.transport=d.transport AND p.request_id=d.request_id WHERE p.transport=? AND p.reply_id=?`, r.Transport, r.MessageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
