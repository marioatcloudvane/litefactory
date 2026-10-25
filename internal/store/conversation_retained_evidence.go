package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

// ConversationEvidence is a protected, bounded view of an admitted source.
// Entry identifies captured bytes; ViewDigest identifies the derived view.
// SourceTurnID pins retention to its original turn, so a follow-up cannot extend it.
// Read methods provide no access grant: callers recheck owner/channel/repository.
type ConversationEvidence struct {
	TurnID, SourceTurnID  string
	Ordinal               int
	Repository, Issue     string
	CapturedAt, ExpiresAt time.Time
	Entry                 retainedevidence.Entry
	Content               string
	RangeStart, RangeEnd  int64
	ViewDigest            string
}

func conversationEvidenceManifest(e ConversationEvidence) retainedevidence.Manifest {
	repository := e.Repository
	if repository == "" {
		repository = "unknown"
	}
	return retainedevidence.Manifest{
		Version:  retainedevidence.Version,
		Scope:    retainedevidence.Scope{Kind: "conversation", ID: e.TurnID + ":" + strconv.Itoa(e.Ordinal), Repository: repository, Task: e.Issue},
		Revision: e.Entry.Revision,
		Coverage: retainedevidence.Partial,
		Entries:  []retainedevidence.Entry{e.Entry},
	}
}

// SaveConversationEvidence atomically stores the owning view and its shared
// manifest before provider use. Exact retries reuse identity; changed captures
// must use another ordinal. Content must have passed the factory's Protect step.
func (db *DB) SaveConversationEvidence(ctx context.Context, t ConversationTurn, e ConversationEvidence) error {
	if e.TurnID != t.ID || e.SourceTurnID == "" || e.Ordinal < 1 || e.Ordinal > 128 || len(e.Repository) > 512 || len(e.Issue) > 512 || len(e.Content) > 65536 || e.CapturedAt.IsZero() || e.ExpiresAt.IsZero() || e.RangeStart < 0 || e.RangeEnd < e.RangeStart || (e.Entry.Bytes >= 0 && e.RangeEnd > e.Entry.Bytes) || e.ViewDigest != retainedevidence.Digest([]byte(e.Content)) {
		return errors.New("invalid conversation evidence")
	}
	manifest := conversationEvidenceManifest(e)
	if err := manifest.Validate(); err != nil {
		return err
	}
	metadata := e
	metadata.Content = ""
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		var source string
		err := tx.QueryRowContext(ctx, `SELECT source FROM chat_conversation_turns WHERE id=? AND conversation_id=?`, e.SourceTurnID, t.ConversationID).Scan(&source)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if source == "" {
			return ErrNotFound
		}
		if err := putEvidenceManifest(ctx, tx, manifest); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO chat_conversation_evidence(turn_id,ordinal,source_turn_id,repository,issue,metadata,content) VALUES(?,?,?,?,?,?,?) ON CONFLICT(turn_id,ordinal) DO NOTHING`, e.TurnID, e.Ordinal, e.SourceTurnID, e.Repository, e.Issue, raw, e.Content)
		if err != nil {
			return err
		}
		var saved []byte
		var content string
		var purged bool
		if err = tx.QueryRowContext(ctx, `SELECT metadata,content,purged FROM chat_conversation_evidence WHERE turn_id=? AND ordinal=?`, e.TurnID, e.Ordinal).Scan(&saved, &content, &purged); err != nil {
			return err
		}
		if purged {
			return ErrNotFound
		}
		if !bytes.Equal(saved, raw) || content != e.Content {
			return retainedevidence.ErrConflict
		}
		return nil
	})
}

// ListConversationEvidence reads bounded captures in an active conversation.
// Expired/purged entries remain explicit metadata, never invented replacement bytes.
func (db *DB) ListConversationEvidence(ctx context.Context, conversationID string, at time.Time, limit int) ([]ConversationEvidence, error) {
	if limit < 1 || limit > 128 || at.IsZero() {
		return nil, errors.New("invalid conversation evidence range")
	}
	return db.listConversationEvidence(ctx, "conversation", conversationID, at, limit)
}

// ListConversationTurnEvidence restores exact saved views for answer details
// and restart. It does not recollect sources or read provider requests.
func (db *DB) ListConversationTurnEvidence(ctx context.Context, turnID string, at time.Time) ([]ConversationEvidence, error) {
	if at.IsZero() {
		return nil, errors.New("invalid conversation evidence time")
	}
	return db.listConversationEvidence(ctx, "turn", turnID, at, 128)
}

func (db *DB) listConversationEvidence(ctx context.Context, selection, id string, at time.Time, limit int) ([]ConversationEvidence, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT e.metadata,e.content,e.purged FROM chat_conversation_evidence e JOIN chat_conversation_turns t ON t.id=e.turn_id JOIN chat_conversations c ON c.id=t.conversation_id WHERE c.active=1 AND ((?='conversation' AND t.conversation_id=?) OR (?='turn' AND t.id=?)) ORDER BY t.sequence DESC,e.ordinal LIMIT ?`, selection, id, selection, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationEvidence
	for rows.Next() {
		var e ConversationEvidence
		var raw []byte
		var content string
		var purged bool
		if err := rows.Scan(&raw, &content, &purged); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode conversation evidence: %w", err)
		}
		if err := conversationEvidenceManifest(e).Validate(); err != nil {
			return nil, err
		}
		switch {
		case purged:
			e.Entry.Availability = retainedevidence.Unavailable
			e.Entry.Reason = "The captured source is no longer retained."
		case !at.Before(e.ExpiresAt):
			e.Entry.Availability = retainedevidence.Unavailable
			e.Entry.Reason = "The captured evidence has expired."
		case e.Entry.Availability == retainedevidence.Available:
			if retainedevidence.Digest([]byte(content)) != e.ViewDigest {
				return nil, retainedevidence.ErrIntegrity
			}
			e.Content = content
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReadConversationByID resolves the current active binding before projecting
// retained evidence. A saved identifier does not restore a retired binding.
func (db *DB) ReadConversationByID(ctx context.Context, id string) (Conversation, error) {
	return scanConversation(db.sql.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM chat_conversations WHERE id=? AND active=1`, id))
}
