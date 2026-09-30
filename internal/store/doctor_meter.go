package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// StartDoctorMeterAttempt pins the existing price catalog without a workflow run.
func (db *DB) StartDoctorMeterAttempt(ctx context.Context, doctorID, provider string) (MeterAttempt, error) {
	if doctorID == "" || meterIdentity(provider) != nil {
		return MeterAttempt{}, errors.New("invalid doctor meter identity")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), DoctorAttemptID: doctorID, CodingAgent: provider}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_attempts a JOIN doctor_conversations c ON c.id=a.conversation_id WHERE a.id=? AND a.state='running' AND c.state='active' AND a.provider=?`, doctorID, provider).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrDoctorFenced
		}
		return tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,doctor_attempt_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM meter_attempts WHERE doctor_attempt_id=?),?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING ordinal,price_revision_id`, a.ID, doctorID, doctorID, provider, FormatTime(nowUTC())).Scan(&a.Ordinal, &a.PriceRevisionID)
	})
	return a, err
}
