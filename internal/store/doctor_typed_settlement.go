package store

import (
	"context"
	"database/sql"
	"errors"
)

// SettleDoctorTypedRepair fences a finished typed execution and releases its
// target hold only for proven aborts or verified commits. It never replays a write.
func (db *DB) SettleDoctorTypedRepair(ctx context.Context, id string, success bool, reason string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, id))
		if err != nil {
			return err
		}
		state := "failed"
		if success {
			state = "completed"
		} else if reason == "" {
			return errors.New("failed repair requires a reason")
		}
		if r.State == state {
			return nil
		}
		if r.State == "completed" || r.State == "failed" || r.State == "restart_pending" || r.State == "recovered" {
			return ErrDoctorFenced
		}
		rows, err := tx.QueryContext(ctx, `SELECT kind,state,backup_reference,observed_after FROM doctor_repair_steps WHERE repair_id=?`, id)
		if err != nil {
			return err
		}
		count := 0
		graphCount, graphVerified, graphNoWrite := 0, 0, true
		valid := true
		typed := true
		for rows.Next() {
			var kind, stepState, backup, observed string
			if err = rows.Scan(&kind, &stepState, &backup, &observed); err != nil {
				_ = rows.Close()
				return err
			}
			count++
			if DoctorGraphRepairKind(kind) {
				graphCount++
				if stepState == "verified" {
					graphVerified++
				}
				graphNoWrite = graphNoWrite && (stepState == "pending" || stepState == "backed_up")
			}
			typed = typed && DoctorRepairToolKind(kind)
			valid = valid && stepState == "verified" && backup != "" && observed != ""
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		if count == 0 || !typed {
			return errors.New("repair settlement requires a typed manifest")
		}
		if graphCount > 0 {
			if graphCount != count {
				return errors.New("mixed graph repair settlement refused")
			}
			if graphVerified == count {
				steps, e := graphStepsTx(ctx, tx, id)
				if e != nil {
					return e
				}
				if e = verifyGraphStepsTx(ctx, tx, r, steps); e != nil {
					return e
				}
			} else if !graphNoWrite || success {
				_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='needs_review',fenced=1,failure=?,revision=revision+1,updated_at=? WHERE id=?`, "Graph repair outcome requires inspection: "+reason, FormatTime(nowUTC()), id)
				return err
			}
		}
		if success && (!valid || r.Fenced || r.ExclusionID == "") {
			return errors.New("repair is not fully rechecked")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state=?,fenced=1,failure=?,revision=revision+1,updated_at=? WHERE id=?`, state, reason, FormatTime(nowUTC()), id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repair_holds SET active=0 WHERE repair_id=?`, id)
		return err
	})
}
