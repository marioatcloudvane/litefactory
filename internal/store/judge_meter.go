package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// StartJudgeMeterAttempt attributes cost to the advisory launch, never the source run.
func (db *DB) StartJudgeMeterAttempt(ctx context.Context, judgeID, provider string) (MeterAttempt, error) {
	if judgeID == "" || meterIdentity(provider) != nil {
		return MeterAttempt{}, errors.New("invalid judge meter identity")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), JudgeAttemptID: judgeID, CodingAgent: provider}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM judge_attempts WHERE id=? AND provider=? AND state='running'`, judgeID, provider).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrResultAuthority
		}
		return tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,judge_attempt_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM meter_attempts WHERE judge_attempt_id=?),?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING ordinal,price_revision_id`, a.ID, judgeID, judgeID, provider, FormatTime(nowUTC())).Scan(&a.Ordinal, &a.PriceRevisionID)
	})
	return a, err
}

// JudgeAcceptedResult recovers accepted bytes after a process ended before adoption.
func (db *DB) JudgeAcceptedResult(ctx context.Context, requestID int64) ([]byte, error) {
	var raw []byte
	err := db.sql.QueryRowContext(ctx, `SELECT r.snapshot FROM application_result_revisions r JOIN judge_attempts a ON a.id=r.judge_attempt_id WHERE a.request_id=? ORDER BY r.request_row DESC LIMIT 1`, requestID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}
