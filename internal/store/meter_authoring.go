package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// StartAuthoringMeterAttempt attributes a response-only call to local authoring,
// without inventing a repository task, workflow run or conversation.
func (db *DB) StartAuthoringMeterAttempt(ctx context.Context, session, provider, model, inputDigest, attemptID, token string) (MeterAttempt, error) {
	if meterIdentity(session, provider, model, inputDigest, attemptID, token) != nil {
		return MeterAttempt{}, errors.New("invalid authoring meter identity")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), AuthoringSessionID: session, CodingAgent: provider}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM standards_authoring_sessions WHERE id=?`, session).Scan(&found); err != nil {
			return err
		}
		if found != 1 {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,authoring_session_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM meter_attempts WHERE authoring_session_id=?),?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING ordinal,price_revision_id`, a.ID, session, session, provider, FormatTime(nowUTC())).Scan(&a.Ordinal, &a.PriceRevisionID); err != nil {
			return err
		}
		var active sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT active_attempt FROM standards_authoring_sessions WHERE id=?`, session).Scan(&active); err != nil {
			return err
		}
		if !active.Valid || active.String != attemptID {
			return ErrStandardsConflict
		}
		attempt, err := readAuthoringAttempt(ctx, tx, active.String)
		if err != nil {
			return err
		}
		if attempt.Token != token || attempt.Outcome != "running" || !attempt.LeaseUntil.After(nowUTC()) {
			return ErrStandardsConflict
		}
		attempt.Provider, attempt.Model, attempt.UsageReference, attempt.InputDigest = provider, model, a.ID, inputDigest
		return writeAuthoringAttempt(ctx, tx, attempt)
	})
	return a, err
}
