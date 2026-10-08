package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

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
			return errors.New("doctor comment watermark drift")
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
