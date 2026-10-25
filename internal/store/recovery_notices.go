package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
)

// RecoveryNotice links immutable delivery identities to material dispositions.
// It contains reporting metadata only, never scheduling or resume authority.
type RecoveryNotice struct{ Key, PreviousKey string }

// RetainRecoveryNotice reuses the initial source identity and retains one linked
// successor per material disposition. Wording and polling times are excluded by
// the caller's typed disposition. The delivery body is kept by report_deliveries.
func (db *DB) RetainRecoveryNotice(ctx context.Context, sessionID int64, source, disposition string) (out RecoveryNotice, err error) {
	if sessionID <= 0 || len(source) != 64 || disposition == "" || len(disposition) > 4096 {
		return out, errors.New("invalid recovery notice identity")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, `SELECT delivery_key,previous_key FROM recovery_notice_dispositions WHERE session_id=? AND source_key=? AND disposition=?`, sessionID, source, disposition).Scan(&out.Key, &out.PreviousKey)
		if e == nil {
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		// Rendering can fail after identity selection but before any immutable body
		// exists. Until that first materialization, retain the source key for the
		// latest state rather than creating an undeliverable successor.
		var materialized bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_deliveries WHERE delivery_key=?)`, source).Scan(&materialized); e != nil {
			return e
		}
		if !materialized {
			result, e := tx.ExecContext(ctx, `UPDATE recovery_notice_dispositions SET disposition=? WHERE session_id=? AND source_key=? AND delivery_key=?`, disposition, sessionID, source, source)
			if e != nil {
				return e
			}
			count, e := result.RowsAffected()
			if e != nil {
				return e
			}
			if count == 1 {
				out.Key = source
				return nil
			}
		}
		e = tx.QueryRowContext(ctx, `SELECT n.delivery_key FROM recovery_notice_dispositions n JOIN report_deliveries d ON d.delivery_key=n.delivery_key WHERE n.session_id=? AND n.source_key=? ORDER BY n.id DESC LIMIT 1`, sessionID, source).Scan(&out.PreviousKey)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		out.Key = source
		if out.PreviousKey != "" {
			sum := sha256.Sum256([]byte(source + "\n" + disposition))
			out.Key = hex.EncodeToString(sum[:])
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO recovery_notice_dispositions(session_id,source_key,disposition,delivery_key,previous_key) VALUES(?,?,?,?,?)`, sessionID, source, disposition, out.Key, out.PreviousKey)
		return e
	})
	return out, err
}

// Informational successors cannot acknowledge an execution or owner input.
func validateRecoveryNoticeTx(ctx context.Context, tx *sql.Tx, d ReportDelivery) error {
	var valid bool
	if d.RunID != 0 || d.TriggerID != 0 {
		return errors.New("recovery notice cannot settle execution or input")
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM recovery_notice_dispositions n JOIN report_deliveries source ON source.delivery_key=n.source_key WHERE n.delivery_key=? AND n.session_id=? AND n.previous_key<>'' AND source.completed=1 AND source.task=? AND source.destination=?)`, d.Key, d.SessionID, d.Task, d.Destination).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errors.New("recovery notice lacks a settled source explanation")
	}
	return nil
}
