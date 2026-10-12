package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func doctorRepairAuthority(ctx context.Context, tx *sql.Tx, id string) (DoctorRepair, error) {
	r, err := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, id))
	if err != nil {
		return r, err
	}
	var n int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_conversations c JOIN doctor_repair_holds h ON h.task_ref=c.task_ref WHERE c.id=? AND c.generation=? AND c.state='active' AND c.current_proposal_id=? AND h.repair_id=? AND h.active=1`, r.ConversationID, r.Generation, r.ProposalID, id).Scan(&n)
	if err != nil {
		return r, err
	}
	if n != 1 || r.Fenced || r.State == "needs_review" || r.State == "verified" || r.State == "restart_pending" || r.State == "recovered" || r.State == "stopped" || r.State == "completed" || r.State == "failed" {
		return r, ErrDoctorFenced
	}
	return r, nil
}

// AcknowledgeDoctorExclusion records factory-owned process/writer drain evidence.
func (db *DB) AcknowledgeDoctorExclusion(ctx context.Context, id, instance string) error {
	if instance == "" {
		return errors.New("doctor exclusion requires instance identity")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.State != "reserved" && r.State != "quiescing" {
			return ErrDoctorFenced
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='quiescing',exclusion_id=?,revision=revision+1,updated_at=? WHERE id=?`, instance, FormatTime(nowUTC()), id)
		return err
	})
}
func doctorRepairStepTx(ctx context.Context, tx *sql.Tx, id string, number int) (DoctorRepairStep, error) {
	var s DoctorRepairStep
	err := tx.QueryRowContext(ctx, `SELECT number,kind,target,before_value,after_value,state,backup_reference,backup_digest,observed_after,source_identity FROM doctor_repair_steps WHERE repair_id=? AND number=?`, id, number).Scan(&s.Number, &s.Kind, &s.Target, &s.Before, &s.After, &s.State, &s.BackupReference, &s.BackupDigest, &s.ObservedAfter, &s.SourceIdentity)
	return s, err
}

// BackupDoctorPoll retains exact affected row values before any repair writes.
func (db *DB) BackupDoctorPoll(ctx context.Context, id string, number int) error {
	return db.BackupDoctorRows(ctx, id, number)
}

// RecordDoctorFileBackup retains only the durable private copy reference, never its raw content in agent output.
func (db *DB) RecordDoctorFileBackup(ctx context.Context, id string, number int, reference, digest string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.ExclusionID == "" {
			return errors.New("doctor target drain is not acknowledged")
		}
		s, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if (s.Kind != "session_file" && s.Kind != "worktree_file") || reference == "" || digest != s.Before {
			return errors.New("doctor file backup mismatch")
		}
		if s.State == "backed_up" && s.BackupReference == reference && s.BackupDigest == digest {
			return nil
		}
		if s.State != "pending" {
			return ErrDoctorFenced
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup_reference=?,backup_digest=?,state='backed_up' WHERE repair_id=? AND number=?`, reference, digest, id, number)
		return err
	})
}
func doctorRepairReady(ctx context.Context, tx *sql.Tx, id string, number int) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND (state='pending' OR (number<? AND state<>'verified'))`, id, number).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return errors.New("doctor backups or preceding steps are incomplete")
	}
	return nil
}

// ApplyDoctorPoll atomically changes the approved column and its journal.
func (db *DB) ApplyDoctorPoll(ctx context.Context, id string, number int) error {
	return db.ApplyDoctorRows(ctx, id, number)
}

// BeginDoctorFileWrite records durable intent before touching a file. A caller must hold the shared repair-effect lock.
func (db *DB) BeginDoctorFileWrite(ctx context.Context, id string, number int) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.ExclusionID == "" {
			return ErrDoctorFenced
		}
		s, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if (s.Kind != "session_file" && s.Kind != "worktree_file") || s.State != "backed_up" {
			return errors.New("doctor file step cannot be replayed")
		}
		if err = doctorRepairReady(ctx, tx, id, number); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET state='write_intent' WHERE repair_id=? AND number=?`, id, number)
		return err
	})
}

// FinishDoctorFileWrite records a reread digest. A third state remains uncertain and held.
func (db *DB) FinishDoctorFileWrite(ctx context.Context, id string, number int, digest string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := doctorRepairAuthority(ctx, tx, id); err != nil {
			return err
		}
		s, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if s.State != "write_intent" || (s.Kind != "session_file" && s.Kind != "worktree_file") {
			return ErrDoctorFenced
		}
		state := "verified"
		if digest != doctorDigest([]byte(s.After)) {
			state = "uncertain"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET state=?,observed_after=? WHERE repair_id=? AND number=?`, state, digest, id, number); err != nil {
			return err
		}
		if state == "uncertain" {
			_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='needs_review',fenced=1,failure='File write outcome uncertain',revision=revision+1 WHERE id=?`, id)
		}
		return err
	})
}

// StopDoctorRepair fences writes and releases only a proven no-write hold.
func (db *DB) StopDoctorRepair(ctx context.Context, ref, reason string) error {
	return db.stopDoctorRepair(ctx, ref, reason, false)
}

// FenceStoppedDoctorRepair cannot fence a newer active generation on a replayed stop.
func (db *DB) FenceStoppedDoctorRepair(ctx context.Context, ref string) error {
	return db.stopDoctorRepair(ctx, ref, "Owner stopped repair", true)
}
func (db *DB) stopDoctorRepair(ctx context.Context, ref, reason string, onlyStopped bool) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE task_ref=? ORDER BY rowid DESC LIMIT 1`, ref))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if onlyStopped {
			var state string
			if err = tx.QueryRowContext(ctx, `SELECT state FROM doctor_conversations WHERE id=? AND generation=?`, r.ConversationID, r.Generation).Scan(&state); err != nil {
				return err
			}
			if state != "stopped" {
				return nil
			}
		}
		// A settled repair is terminal; a later stop must not reopen it.
		if r.State == "verified" || r.State == "restart_pending" || r.State == "recovered" || r.State == "completed" || r.State == "failed" || r.State == "stopped" {
			return nil
		}
		var count, legacy int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND state IN('write_intent','verified','uncertain')`, r.ID).Scan(&count); err != nil {
			return err
		}
		kinds := DoctorRepairToolKinds()
		args := []any{r.ID}
		for _, kind := range kinds {
			args = append(args, kind)
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND kind NOT IN(?`+strings.Repeat(",?", len(kinds)-1)+`)`, args...).Scan(&legacy); err != nil {
			return err
		}
		state := "stopped"
		if count > 0 && legacy == 0 {
			// Typed effects are settled as failed, matching typed recovery.
			state = "failed"
		} else if count > 0 {
			state = "needs_review"
		}
		if state != "needs_review" {
			if _, err = tx.ExecContext(ctx, `UPDATE doctor_repair_holds SET active=0 WHERE repair_id=?`, r.ID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state=?,fenced=1,failure=?,revision=revision+1,updated_at=? WHERE id=?`, state, reason, FormatTime(nowUTC()), r.ID)
		return err
	})
}
