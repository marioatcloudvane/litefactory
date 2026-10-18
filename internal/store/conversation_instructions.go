package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxConversationCaptureBytes = 16 << 20

func validConversationCapture(raw []byte) error {
	if len(raw) == 0 || len(raw) > maxConversationCaptureBytes || !json.Valid(raw) {
		return errors.New("invalid conversation instruction capture encoding")
	}
	return nil
}

// ReadConversationCapture reads retained bytes without consulting current sources.
// The caller restores and validates the complete instruction-package capture.
func (db *DB) ReadConversationCapture(ctx context.Context, id string) ([]byte, bool, error) {
	var raw []byte
	var legacy bool
	err := db.sql.QueryRowContext(ctx, `SELECT capture,legacy_baseline FROM chat_conversation_instructions WHERE conversation_id=?`, id).Scan(&raw, &legacy)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if err = validConversationCapture(raw); err != nil {
		return nil, false, err
	}
	return raw, legacy, nil
}

// ListLegacyConversations identifies only bindings present when the capture
// schema was installed. Missing captures on newer bindings are never backfilled.
func (db *DB) ListLegacyConversations(ctx context.Context) ([]Conversation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id IN (SELECT conversation_id FROM chat_conversation_instruction_legacy) ORDER BY id`)
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

// ConversationLegacyInstructions are the exact pinned pre-migration values.
// They are used only to check available saved instruction evidence for conflicts.
type ConversationLegacyInstructions struct{ Native, Factory, Correction, ActionOutcome string }

// BackfillLegacyConversationCapture retains the pinned baseline only for a
// migration-marked binding. Conflicting saved instruction evidence pauses that
// binding and preserves its native session and all pending work.
func (db *DB) BackfillLegacyConversationCapture(ctx context.Context, id string, raw []byte, expected ConversationLegacyInstructions, at time.Time) error {
	if err := validConversationCapture(raw); err != nil {
		return err
	}
	if at.IsZero() || expected.Native == "" || expected.Factory == "" || expected.Correction == "" || expected.ActionOutcome == "" {
		return errors.New("invalid conversation legacy baseline")
	}
	var conflict bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var marked int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversation_instruction_legacy WHERE conversation_id=?`, id).Scan(&marked); err != nil {
			return err
		}
		if marked != 1 {
			var retained []byte
			var legacy bool
			err := tx.QueryRowContext(ctx, `SELECT capture,legacy_baseline FROM chat_conversation_instructions WHERE conversation_id=?`, id).Scan(&retained, &legacy)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if legacy && string(retained) == string(raw) {
				return nil
			}
			return errors.New("conversation instruction baseline conflicts with retained capture")
		}
		rows, err := tx.QueryContext(ctx, `SELECT prompt FROM chat_conversation_turns WHERE conversation_id=? UNION ALL SELECT e.request FROM chat_conversation_exchanges e JOIN chat_conversation_turns t ON t.id=e.turn_id WHERE t.conversation_id=? AND e.kind IN ('provider','correction')`, id, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var prompt string
			if err = rows.Scan(&prompt); err != nil {
				break
			}
			if legacyPromptConflict(prompt, expected) {
				conflict = true
			}
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if conflict {
			_, err = tx.ExecContext(ctx, `UPDATE chat_conversations SET health_reason='conversation instruction legacy baseline conflicts with saved instructions',updated_at=? WHERE id=?`, FormatTime(at), id)
			return err
		}
		var retained []byte
		var legacy bool
		retainedErr := tx.QueryRowContext(ctx, `SELECT capture,legacy_baseline FROM chat_conversation_instructions WHERE conversation_id=?`, id).Scan(&retained, &legacy)
		if retainedErr == nil {
			if !legacy || string(retained) != string(raw) {
				return errors.New("conversation instruction baseline conflicts with retained capture")
			}
		} else if !errors.Is(retainedErr, sql.ErrNoRows) {
			return retainedErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_instructions(conversation_id,capture,legacy_baseline,created_at) VALUES(?,?,1,?) ON CONFLICT(conversation_id) DO NOTHING`, id, raw, FormatTime(at))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM chat_conversation_instruction_legacy WHERE conversation_id=?`, id)
		return err
	})
	if err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("conversation instruction legacy baseline conflicts with saved instructions")
	}
	return nil
}

func legacyPromptConflict(prompt string, expected ConversationLegacyInstructions) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(prompt), &fields) != nil {
		return false
	}
	for key, raw := range fields {
		var value string
		switch strings.ToLower(key) {
		case "instructions":
			if json.Unmarshal(raw, &value) != nil || (value != expected.Native && value != expected.Factory && value != expected.Factory+" "+expected.ActionOutcome) {
				return true
			}
		case "correction":
			if json.Unmarshal(raw, &value) != nil || (value != "" && value != expected.Correction) {
				return true
			}
		}
	}
	return false
}
