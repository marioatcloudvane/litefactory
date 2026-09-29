package store

import (
	"context"
	"database/sql"
	"errors"
)

// DoctorRestart retains one handoff; an uncertain external response never grants another dispatch.
type DoctorRestart struct{ RepairID, Token, State, OldInstance, NewInstance, Report string }

// CommitDoctorRestart closes verified repair authority and retains one recovery intent.
// The caller must reread every file under the repair effect lock immediately before this call.
func (db *DB) CommitDoctorRestart(ctx context.Context, id, token, report string) error {
	if token == "" || report == "" || len(report) > 65536 {
		return errors.New("invalid doctor restart handoff")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.ExclusionID == "" {
			return ErrDoctorFenced
		}
		if err = verifyDoctorRepairRows(ctx, tx, id, r.TaskRef); err != nil {
			return err
		}
		now := FormatTime(nowUTC())
		if _, err = tx.ExecContext(ctx, `INSERT INTO doctor_restarts(repair_id,token,state,old_instance,report,created_at) VALUES(?,?,'pending',?,?,?)`, id, token, r.ExclusionID, report, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='restart_pending',fenced=1,revision=revision+1,updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_conversations SET state='stopped',revision=revision+1,updated_at=? WHERE id=? AND generation=?`, now, r.ConversationID, r.Generation); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE doctor_attempt_id IN(SELECT id FROM doctor_attempts WHERE conversation_id=?)`, r.ConversationID)
		return err
	})
}
func verifyDoctorRepairRows(ctx context.Context, tx *sql.Tx, id, ref string) error {
	rows, err := tx.QueryContext(ctx, `SELECT number,kind,target,after_value,state FROM doctor_repair_steps WHERE repair_id=? ORDER BY number`, id)
	if err != nil {
		return err
	}
	var steps []DoctorRepairStep
	for rows.Next() {
		var s DoctorRepairStep
		if err = rows.Scan(&s.Number, &s.Kind, &s.Target, &s.After, &s.State); err != nil {
			_ = rows.Close()
			return err
		}
		steps = append(steps, s)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if len(steps) == 0 {
		return errors.New("doctor repair has no steps")
	}
	for _, s := range steps {
		if s.State != "verified" {
			return errors.New("doctor repair has incomplete or uncertain steps")
		}
		if s.Kind != "session_file" && s.Kind != "worktree_file" {
			state, _, e := doctorRowSnapshot(ctx, tx, ref, s.Kind, s.Target)
			if e != nil {
				return e
			}
			if state != s.After {
				return errors.New("doctor final row verification failed")
			}
		}

	}
	return nil
}

// ClaimDoctorRestart records dispatch before the external effect. It never reclaims a dispatched token.
func (db *DB) ClaimDoctorRestart(ctx context.Context, id string) (DoctorRestart, error) {
	var out DoctorRestart
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT repair_id,token,state,old_instance,new_instance,report FROM doctor_restarts WHERE repair_id=?`, id).Scan(&out.RepairID, &out.Token, &out.State, &out.OldInstance, &out.NewInstance, &out.Report); err != nil {
			return err
		}
		if out.State != "pending" {
			return errors.New("doctor restart already dispatched; observation required")
		}
		_, err := tx.ExecContext(ctx, `UPDATE doctor_restarts SET state='dispatched' WHERE repair_id=? AND state='pending'`, id)
		out.State = "dispatched"
		return err
	})
	return out, err
}

// AcknowledgeDoctorRestart releases a hold only after a distinct singleton owner has verified files and rows.
func (db *DB) AcknowledgeDoctorRestart(ctx context.Context, id, token, newInstance string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var old, state string
		if err := tx.QueryRowContext(ctx, `SELECT old_instance,state FROM doctor_restarts WHERE repair_id=? AND token=?`, id, token).Scan(&old, &state); err != nil {
			return err
		}
		if newInstance == "" || newInstance == old || (state != "dispatched" && state != "uncertain") {
			return errors.New("doctor restart acknowledgment identity refused")
		}
		r, err := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, id))
		if err != nil {
			return err
		}
		if err = verifyDoctorRepairRows(ctx, tx, id, r.TaskRef); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_restarts SET state='acknowledged',new_instance=? WHERE repair_id=?`, newInstance, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='recovered',revision=revision+1,updated_at=? WHERE id=?`, FormatTime(nowUTC()), id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repair_holds SET active=0 WHERE repair_id=?`, id)
		return err
	})
}

// ReadDoctorRestart supports status and publication retries without repeating effects.
func (db *DB) ReadDoctorRestart(ctx context.Context, id string) (DoctorRestart, error) {
	var r DoctorRestart
	err := db.sql.QueryRowContext(ctx, `SELECT repair_id,token,state,old_instance,new_instance,report FROM doctor_restarts WHERE repair_id=?`, id).Scan(&r.RepairID, &r.Token, &r.State, &r.OldInstance, &r.NewInstance, &r.Report)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

// MarkDoctorRestartUncertain preserves dispatch debt after a lost external reply.
func (db *DB) MarkDoctorRestartUncertain(ctx context.Context, id string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE doctor_restarts SET state='uncertain' WHERE repair_id=? AND state='dispatched'`, id)
	if err != nil {
		return err
	}
	return requireAffected(result, "doctor restart uncertainty")
}
