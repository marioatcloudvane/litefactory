package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// SaveChatOutcome fixes the redacted completed result under the delivery fence.
// Saving final content is a separate step; command effects never need repeating.
func (db *DB) SaveChatOutcome(ctx context.Context, r ChatReply, outcome string) error {
	if outcome == "" || len(outcome) > 262144 {
		return errors.New("invalid presentation outcome size")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET presentation_outcome=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=? AND presentation_state='pending' AND presentation_outcome='' AND reply_text<>''`, outcome, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// BeginChatPresentation spends the durable one-call budget before model I/O.
func (db *DB) BeginChatPresentation(ctx context.Context, r ChatReply, checking bool) error {
	query := `UPDATE chat_inbox SET presentation_generation=1 WHERE transport=? AND message_id=? AND state='sent' AND attempts=? AND presentation_state='pending' AND presentation_outcome<>'' AND presentation_generation=0`
	if checking {
		query = `UPDATE chat_inbox SET presentation_check=1 WHERE transport=? AND message_id=? AND state='sent' AND attempts=? AND presentation_state='pending' AND presentation_outcome<>'' AND presentation_generation=1 AND presentation_check=0`
	}
	result, err := db.sql.ExecContext(ctx, query, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// SaveChatPresentation fixes content and attachment together before delivery.
func (db *DB) SaveChatPresentation(ctx context.Context, r ChatReply, state, content, attachment string) error {
	if (state != "narrated" && state != "raw_fallback") || content == "" || len(content) > 8000 || len(attachment) > 65536 {
		return errors.New("invalid final presentation")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET presentation_state=?,reply_text=?,reply_attachment=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=? AND presentation_state='pending' AND presentation_outcome<>''`, state, content, attachment, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// InlineChatPresentation selects a fixed raw payload only after a definite
// attachment rejection. Uncertain sends must retry the unchanged attachment.
func (db *DB) InlineChatPresentation(ctx context.Context, r ChatReply, text string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET presentation_state='inline',reply_text=?,reply_attachment='' WHERE transport=? AND message_id=? AND state='sent' AND attempts=? AND presentation_state IN ('narrated','raw_fallback')`, text, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// StartPresentationMeterAttempt records a genuine factory or repository owner,
// independently for generation and checking, retaining no reply content.
func (db *DB) StartPresentationMeterAttempt(ctx context.Context, repoID int64, identity, phase, provider string) (MeterAttempt, error) {
	if repoID < 0 || meterIdentity(identity) != nil || meterIdentity(provider) != nil || (phase != "generation" && phase != "checking") {
		return MeterAttempt{}, errors.New("invalid presentation meter identity")
	}
	var repository any
	if repoID > 0 {
		repository = repoID
	}
	owner := identity + "-" + phase
	_, err := db.sql.ExecContext(ctx, `INSERT INTO chat_presentation_owners(id,repo_id,phase) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, owner, repository, phase)
	if err != nil {
		return MeterAttempt{}, err
	}
	var storedRepo int64
	var storedPhase string
	if err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(repo_id,0),phase FROM chat_presentation_owners WHERE id=?`, owner).Scan(&storedRepo, &storedPhase); err != nil {
		return MeterAttempt{}, err
	}
	if storedRepo != repoID || storedPhase != phase {
		return MeterAttempt{}, errors.New("presentation meter owner changed")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), CodingAgent: provider, Ordinal: 1}
	err = db.sql.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,presentation_owner,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,1,?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING price_revision_id`, a.ID, owner, provider, FormatTime(nowUTC())).Scan(&a.PriceRevisionID)
	return a, err
}
