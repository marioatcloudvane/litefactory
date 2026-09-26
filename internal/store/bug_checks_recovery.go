package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ReconcileBugCheckStopped records a factory-observed namespace termination.
// It does not overwrite any completed command observation, and cannot create
// passing evidence. The caller owns kernel identity/termination observation.
func (d *DB) ReconcileBugCheckStopped(ctx context.Context, session int64, operation string, ordinal int, identity string, observed time.Time) error {
	if identity == "" || observed.IsZero() {
		return errors.New("check recovery requires process identity and observation")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var state string
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT state,observation FROM bug_check_commands WHERE session_id=? AND operation=? AND ordinal=?`, session, operation, ordinal).Scan(&state, &raw); err != nil {
			return err
		}
		var old BugCheckObservation
		if err := json.Unmarshal(raw, &old); err != nil {
			return err
		}
		if old.ProcessIdentity != identity || old.ProcessID <= 0 || old.Started.IsZero() || observed.Before(old.Started) {
			return errors.New("check recovery process identity mismatch")
		}
		var cancelled, cleanup bool
		var outcome string
		if err := tx.QueryRowContext(ctx, `SELECT cancelled,cleanup,outcome FROM bug_check_phases WHERE session_id=? AND operation=?`, session, operation).Scan(&cancelled, &cleanup, &outcome); err != nil {
			return err
		}
		if cleanup {
			return nil
		}
		var latest int
		if err := tx.QueryRowContext(ctx, `SELECT MAX(ordinal) FROM bug_check_commands WHERE session_id=? AND operation=?`, session, operation).Scan(&latest); err != nil {
			return err
		}
		if latest != ordinal {
			return errors.New("check recovery does not name active command")
		}
		if state != "started" && state != "completed" {
			return errors.New("check recovery lacks retained launch observation")
		}
		proof, err := json.Marshal(struct {
			Identity string    `json:"identity"`
			Observed time.Time `json:"observed"`
			Ordinal  int       `json:"ordinal"`
		}{identity, observed, ordinal})
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_check_events(session_id,operation,kind,content) VALUES(?,?,'namespace_stopped',?)`, session, operation, proof); err != nil {
			return err
		}
		if state != "completed" {
			old.Outcome = "blocked"
			old.Reason = "check interrupted; namespace termination confirmed during recovery"
			if cancelled {
				old.Outcome = "cancelled"
				old.Reason = "check cancellation termination confirmed during recovery"
			}
			old.Ended = observed
			old.Exit = nil
			old.CaptureComplete = false
			old.CleanupConfirmed = true
			content, err := json.Marshal(old)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE bug_check_commands SET state='completed',observation=? WHERE session_id=? AND operation=? AND ordinal=?`, content, session, operation, ordinal); err != nil {
				return err
			}
		}
		// Completed observations remain immutable even if their cleanup was unknown.
		// The append-only namespace_stopped event supplies later cleanup evidence.
		_, err = tx.ExecContext(ctx, `UPDATE bug_check_phases SET cleanup=1 WHERE session_id=? AND operation=?`, session, operation)
		return err
	})
}

// ReleaseQuiescentBugCheckWork releases only this phase's check guard and is
// idempotent after recovery. Foreign or child ownership is never removed.
func (d *DB) ReleaseQuiescentBugCheckWork(ctx context.Context, session int64, operation string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var cleanup bool
		if err := tx.QueryRowContext(ctx, `SELECT cleanup FROM bug_check_phases WHERE session_id=? AND operation=?`, session, operation).Scan(&cleanup); err != nil {
			return err
		}
		if !cleanup {
			return errors.New("check cleanup remains unresolved")
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=? AND kind='checks'`, session, operation)
		return err
	})
}
