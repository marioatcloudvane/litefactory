package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrDoctorWatermarkDrift reports that a consume step's comment watermark no
// longer matches the approved before-state.
var ErrDoctorWatermarkDrift = errors.New("doctor comment watermark drift")

// DoctorConsumeDroppedNote is appended once to a stopped repair's failure when
// its unconfirmed consume reply is dropped without being posted.
const DoctorConsumeDroppedNote = "; doctor reply dropped unposted"

// BeginDoctorBranchSync records the approved head before any git mutation.
func (db *DB) BeginDoctorBranchSync(ctx context.Context, id string, number int, current string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.ExclusionID == "" {
			return ErrDoctorFenced
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		if s.Kind != "sync_branch" || s.State != "pending" || current != s.Before {
			return errors.New("branch synchronization precondition changed")
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND number<? AND state<>'verified'`, id, number).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("doctor preceding step incomplete")
		}
		_, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='write_intent' WHERE repair_id=? AND number=?`, []byte(current), doctorDigest([]byte(current)), fmt.Sprintf("git:head:%s", current), id, number)
		return e
	})
}

// FinishDoctorBranchSync records factory-observed containment after a successful push.
func (db *DB) FinishDoctorBranchSync(ctx context.Context, id string, number int, head string) (out DoctorRepairStep, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := doctorRepairAuthority(ctx, tx, id); e != nil {
			return e
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		if s.Kind != "sync_branch" || s.State != "write_intent" || head == "" {
			return ErrDoctorFenced
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET state='verified',observed_after=? WHERE repair_id=? AND number=?`, head, id, number); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='applying',revision=revision+1,updated_at=? WHERE id=?`, FormatTime(nowUTC()), id); e != nil {
			return e
		}
		out, e = doctorRepairStepTx(ctx, tx, id, number)
		return e
	})
	return
}

// PrepareDoctorConsumeComment records the watermark backup before posting a reply.
func (db *DB) PrepareDoctorConsumeComment(ctx context.Context, id string, number int) (session int64, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.ExclusionID == "" {
			return ErrDoctorFenced
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		if s.Kind != "consume_comment" || s.State != "pending" || !DoctorTypedRowRepairTarget(s.Kind, s.Target, s.Before, s.After) {
			return ErrDoctorFenced
		}
		current, data, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, s.Kind, s.Target)
		if e != nil {
			return e
		}
		if current != s.Before {
			return ErrDoctorWatermarkDrift
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND number<? AND state<>'verified'`, id, number).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("doctor preceding step incomplete")
		}
		if e = tx.QueryRowContext(ctx, `SELECT c.session_id FROM comments c JOIN sessions s ON s.id=c.session_id WHERE c.forge_comment_id=? AND s.task_ref=?`, s.After, r.TaskRef).Scan(&session); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='backed_up' WHERE repair_id=? AND number=?`, data, doctorDigest(data), fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, number), id, number)
		return e
	})
	return
}

// ClaimDoctorConsumeDelivery retains the reply and its only POST opportunity
// under the approved repair permit. It never opens a workflow write window.
func (db *DB) ClaimDoctorConsumeDelivery(ctx context.Context, id string, number int, want ReportDelivery) (out ReportDelivery, claimed bool, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		if r.ExclusionID == "" || s.Kind != "consume_comment" || s.State != "backed_up" || want.Task != r.TaskRef || want.Destination != r.TaskRef || want.TriggerNamespace != "comment" || want.Purpose != "reply" || want.RunID != 0 || want.BotUserID <= 0 || want.Body == "" || want.Marker == "" || want.Key == "" || fmt.Sprint(want.TriggerID) != s.After {
			return ErrDoctorFenced
		}
		var session int64
		if e = tx.QueryRowContext(ctx, `SELECT c.session_id FROM comments c JOIN sessions x ON x.id=c.session_id WHERE c.forge_comment_id=? AND x.task_ref=?`, s.After, r.TaskRef).Scan(&session); e != nil {
			return e
		}
		if session != want.SessionID {
			return ErrDoctorFenced
		}
		existing, e := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, want.Key))
		if e == nil {
			out = existing
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); e != nil {
			return e
		}
		out, e = scanReportDelivery(tx.QueryRowContext(ctx, `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,legacy) VALUES(?,1,?,?,NULL,?,?,'comment','reply',?,?,?,1,0) RETURNING `+reportDeliveryColumns, want.Key, session, want.TriggerID, want.Task, want.Destination, want.BotUserID, want.Body, want.Marker))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id)
		claimed = e == nil
		return e
	})
	return
}

// ConfirmDoctorConsumeDelivery stores the trusted forge receipt without consuming.
func (db *DB) ConfirmDoctorConsumeDelivery(ctx context.Context, id string, number int, key string, comment int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		d, e := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if e != nil {
			return e
		}
		if s.Kind != "consume_comment" || s.State != "backed_up" || d.Task != r.TaskRef || fmt.Sprint(d.TriggerID) != s.After || !d.PossiblySent || comment <= 0 || d.Completed || d.CommentID != 0 {
			return ErrDoctorFenced
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE report_deliveries SET comment_id=? WHERE delivery_key=?`, comment, key); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id)
		return e
	})
}

// UnmarkDoctorConsumeDelivery records that the consume reply's only POST was
// refused by the forge before any write, under the same checks as its receipt.
// The row stays retained but is no longer possibly sent.
func (db *DB) UnmarkDoctorConsumeDelivery(ctx context.Context, id string, number int, key string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		d, e := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if e != nil {
			return e
		}
		if s.Kind != "consume_comment" || s.State != "backed_up" || d.Task != r.TaskRef || fmt.Sprint(d.TriggerID) != s.After || !d.PossiblySent || d.Completed || d.CommentID != 0 {
			return ErrDoctorFenced
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE report_deliveries SET possibly_sent=0 WHERE delivery_key=? AND comment_id IS NULL AND completed=0`, key); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id)
		return e
	})
}

// DoctorConsumeSettlement names the repair step whose reply was dropped.
type DoctorConsumeSettlement struct {
	RepairID string
	Step     int
}

// SettleFencedDoctorConsumeDelivery drops the unconfirmed consume reply of a
// repair that is no longer live, so it never holds its session and is never
// posted. It applies only while the step is unverified and the row has no
// receipt, and only when the row is not possibly sent or a complete lookup
// found no trace (lookupEmpty). The repair's state is never changed; its
// failure gains DoctorConsumeDroppedNote once. A false result changes nothing.
func (db *DB) SettleFencedDoctorConsumeDelivery(ctx context.Context, key string, lookupEmpty bool) (out DoctorConsumeSettlement, settled bool, err error) {
	rest, ok := strings.CutPrefix(key, "doctor-consume:")
	cut := strings.LastIndexByte(rest, ':')
	if !ok || cut <= 0 {
		return out, false, nil
	}
	digits := rest[cut+1:]
	if strings.Trim(digits, "0123456789") != "" || len(digits) > 9 {
		return out, false, nil
	}
	number, _ := strconv.Atoi(digits)
	if number <= 0 {
		return out, false, nil
	}
	id := rest[:cut]
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		d, e := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		r, e := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, id))
		if errors.Is(e, sql.ErrNoRows) || errors.Is(e, ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		live := !r.Fenced && r.State != "stopped" && r.State != "failed" && r.State != "needs_review"
		if live {
			return nil
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if errors.Is(e, sql.ErrNoRows) || errors.Is(e, ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if s.Kind != "consume_comment" || s.State == "verified" || d.Task != r.TaskRef || fmt.Sprint(d.TriggerID) != s.After || d.CommentID != 0 || d.Completed || (d.PossiblySent && !lookupEmpty) {
			return nil
		}
		// A row of a held session is written only under that hold's permit.
		holds := `SELECT h.repair_id FROM doctor_repair_holds h JOIN doctor_held_session_owners dv ON dv.repair_id=h.repair_id WHERE h.active=1 AND dv.session_id=?`
		if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO doctor_repair_write_permits(repair_id) `+holds, d.SessionID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM report_deliveries WHERE delivery_key=? AND comment_id IS NULL AND completed=0`, key); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id IN (`+holds+`)`, d.SessionID); e != nil {
			return e
		}
		if !strings.Contains(r.Failure, DoctorConsumeDroppedNote) {
			if _, e = tx.ExecContext(ctx, `UPDATE doctor_repairs SET failure=failure||? WHERE id=?`, DoctorConsumeDroppedNote, id); e != nil {
				return e
			}
		}
		out, settled = DoctorConsumeSettlement{RepairID: id, Step: number}, true
		return nil
	})
	if err != nil {
		return DoctorConsumeSettlement{}, false, fmt.Errorf("store: settle doctor consume reply: %w", err)
	}
	return out, settled, nil
}
