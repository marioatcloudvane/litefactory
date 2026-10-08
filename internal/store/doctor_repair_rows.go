package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DoctorRowRepairTarget validates the closed operation catalog without executing SQL.
func DoctorRowRepairTarget(kind, target, before, after string) bool {
	prefix, ok := doctorRowPrefixes[kind]
	if !ok || !strings.HasPrefix(target, prefix+":") || len(target) > 512 || before == after {
		return false
	}
	key := strings.TrimPrefix(target, prefix+":")
	if key == "" || strings.ContainsAny(key, " \t\r\n\x00\\/\"'`") {
		return false
	}
	if kind != "report_completion" && kind != "graph_wait_cancel" {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != key {
			return false
		}
	}
	switch kind {
	case "poll_flag":
		return (before == "0" || before == "1") && (after == "0" || after == "1")
	case "run_interrupt":
		return before == "running" && after == "failed"
	case "session_unlock":
		return before != "" && after == ""
	case "report_completion":
		return before == "confirmed" && after == "completed"
	case "watermark_rewind":
		a, e := strconv.ParseInt(before, 10, 64)
		b, f := strconv.ParseInt(after, 10, 64)
		return e == nil && f == nil && b >= 0 && b < a
	case "graph_dispatch_cancel":
		return (before == "pending" || before == "reserved" || before == "running") && after == "cancelled"
	case "graph_wait_cancel":
		return (before == "waiting" || before == "claimed" || before == "allocated") && after == "cancelled"
	case "graph_attempt_cancel":
		return (before == "pending" || before == "running") && after == "cancelled"
	case "graph_fail":
		return before == "running" && after == "failed"
	}
	return false
}

var doctorRowPrefixes = map[string]string{"poll_flag": "session", "run_interrupt": "run", "session_unlock": "session_lock", "report_completion": "report", "watermark_rewind": "watermark", "graph_dispatch_cancel": "graph_dispatch", "graph_wait_cancel": "owner_wait", "graph_attempt_cancel": "graph_attempt", "graph_fail": "graph"}

func doctorRowSnapshot(ctx context.Context, tx *sql.Tx, ref, kind, target string) (state string, backup []byte, err error) {
	if _, ok := doctorTypedPrefixes[kind]; ok {
		return doctorTypedRowSnapshot(ctx, tx, ref, kind, target)
	}
	prefix, ok := doctorRowPrefixes[kind]
	if !ok || !strings.HasPrefix(target, prefix+":") {
		return "", nil, errors.New("unsupported doctor row target")
	}
	key := strings.TrimPrefix(target, prefix+":")
	var query string
	switch kind {
	case "poll_flag":
		query = `SELECT CAST(s.needs_poll AS TEXT),json_object('table','sessions','id',s.id,'needs_poll',s.needs_poll) FROM sessions s WHERE s.id=? AND s.task_ref=?`
	case "session_unlock":
		query = `SELECT COALESCE(s.lock_owner,''),json_object('table','sessions','id',s.id,'lock_owner',s.lock_owner,'lock_acquired_at',s.lock_acquired_at) FROM sessions s WHERE s.id=? AND s.task_ref=?`
	case "run_interrupt":
		query = `SELECT r.status,json_object('table','runs','id',r.id,'session_id',r.session_id,'status',r.status,'exit_code',r.exit_code,'error',r.error,'finished_at',r.finished_at,'duration_ms',r.duration_ms) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=? AND s.task_ref=?`
	case "report_completion":
		query = `SELECT CASE WHEN r.completed=1 THEN 'completed' WHEN r.comment_id IS NOT NULL AND r.possibly_sent=1 THEN 'confirmed' ELSE 'unconfirmed' END,json_object('table','report_deliveries','delivery_key',r.delivery_key,'session_id',r.session_id,'completed',r.completed,'comment_id',r.comment_id,'bot_user_id',r.bot_user_id,'destination',r.destination) FROM report_deliveries r JOIN sessions s ON s.id=r.session_id WHERE r.delivery_key=? AND s.task_ref=? AND r.task=s.task_ref AND r.destination=s.task_ref AND r.doctor_command_id IS NULL`
	case "watermark_rewind":
		query = `SELECT CAST(w.last_comment_id AS TEXT),json_object('table','watermarks','session_id',w.session_id,'last_comment_id',w.last_comment_id,'updated_at',w.updated_at) FROM watermarks w JOIN sessions s ON s.id=w.session_id WHERE w.session_id=? AND s.task_ref=?`
	case "graph_dispatch_cancel":
		query = `SELECT d.status,json_object('table','graph_dispatches','id',d.id,'session_id',d.session_id,'status',d.status,'finished_at',d.finished_at,'cancellation_reason',d.cancellation_reason,'allocated_run_id',d.allocated_run_id) FROM graph_dispatches d JOIN sessions s ON s.id=d.session_id WHERE d.id=? AND s.task_ref=?`
	case "graph_wait_cancel":
		query = `SELECT w.state,json_object('table','graph_owner_waits','delivery_key',w.delivery_key,'instance_id',w.instance_id,'state',w.state,'reply_id',w.reply_id,'reply_body',w.reply_body,'reply_author_id',w.reply_author_id,'claimed_at',w.claimed_at,'continuation_attempt_id',w.continuation_attempt_id) FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE w.delivery_key=? AND s.task_ref=?`
	case "graph_attempt_cancel":
		query = `SELECT a.status,json_object('table','graph_node_attempts','id',a.id,'instance_id',a.instance_id,'run_id',a.run_id,'status',a.status,'outcome',a.outcome,'finished_at',a.finished_at) FROM graph_node_attempts a JOIN graph_instances g ON g.id=a.instance_id JOIN sessions s ON s.id=g.session_id WHERE a.id=? AND s.task_ref=?`
	case "graph_fail":
		query = `SELECT g.status,json_object('table','graph_instances','id',g.id,'session_id',g.session_id,'status',g.status,'finished_at',g.finished_at,'next_due_at',g.next_due_at) FROM graph_instances g JOIN sessions s ON s.id=g.session_id WHERE g.id=? AND s.task_ref=?`
	default:
		return "", nil, errors.New("unsupported doctor row operation")
	}
	err = tx.QueryRowContext(ctx, query, key, ref).Scan(&state, &backup)
	return state, backup, err
}

// BackupDoctorRows saves the exact affected columns privately, bounded to the approved issue.
func (db *DB) BackupDoctorRows(ctx context.Context, id string, number int) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.ExclusionID == "" {
			return errors.New("doctor target drain is not acknowledged")
		}
		step, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		if step.State == "backed_up" || step.State == "verified" {
			return nil
		}
		if step.State != "pending" {
			return ErrDoctorFenced
		}
		state, data, err := doctorRowSnapshot(ctx, tx, r.TaskRef, step.Kind, step.Target)
		if err != nil {
			return err
		}
		if state != step.Before || len(data) > 131072 {
			return errors.New("doctor row precondition changed or backup too large")
		}
		if !json.Valid(data) {
			return errors.New("doctor row backup malformed")
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='backed_up' WHERE repair_id=? AND number=?`, data, doctorDigest(data), fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, number), id, number)
		return err
	})
}

// ApplyDoctorRows executes one catalog transition and its journal in one transaction.
func (db *DB) ApplyDoctorRows(ctx context.Context, id string, number int) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.ExclusionID == "" {
			return ErrDoctorFenced
		}
		step, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		current, _, err := doctorRowSnapshot(ctx, tx, r.TaskRef, step.Kind, step.Target)
		if err != nil {
			return err
		}
		if step.State == "verified" {
			if current != step.After {
				return errors.New("settled doctor row drift")
			}
			return nil
		}
		if step.State != "backed_up" || current != step.Before || !DoctorRowRepairTarget(step.Kind, step.Target, step.Before, step.After) {
			return errors.New("doctor row precondition or transition refused")
		}
		if err = doctorRepairReady(ctx, tx, id, number); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); err != nil {
			return err
		}
		key := strings.TrimPrefix(step.Target, doctorRowPrefixes[step.Kind]+":")
		now := FormatTime(nowUTC())
		var result sql.Result
		switch step.Kind {
		case "poll_flag":
			result, err = tx.ExecContext(ctx, `UPDATE sessions SET needs_poll=? WHERE id=? AND task_ref=? AND needs_poll=?`, step.After, key, r.TaskRef, step.Before)
		case "session_unlock":
			result, err = tx.ExecContext(ctx, `UPDATE sessions SET lock_owner=NULL,lock_acquired_at=NULL WHERE id=? AND task_ref=? AND lock_owner=? AND NOT EXISTS(SELECT 1 FROM runs WHERE session_id=sessions.id AND status='running')`, key, r.TaskRef, step.Before)
		case "run_interrupt":
			// Related live graph attempts must also appear in this exact approved manifest.
			var unapproved int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts a WHERE a.run_id=? AND a.status IN('pending','running') AND NOT EXISTS(SELECT 1 FROM doctor_repair_steps x WHERE x.repair_id=? AND x.kind='graph_attempt_cancel' AND x.target='graph_attempt:'||a.id)`, key, id).Scan(&unapproved); err != nil {
				return err
			}
			if unapproved != 0 {
				return errors.New("doctor run repair would strand an unapproved graph attempt")
			}
			result, err = tx.ExecContext(ctx, `UPDATE runs SET status='failed',exit_code=-1,error=?,finished_at=?,duration_ms=MAX(0,CAST((julianday(?)-julianday(started_at))*86400000 AS INTEGER)) WHERE id=? AND status='running'`, "Approved doctor repair "+id, now, now, key)
		case "report_completion":
			result, err = tx.ExecContext(ctx, `UPDATE report_deliveries SET completed=1 WHERE delivery_key=? AND completed=0 AND comment_id>0 AND bot_user_id>0 AND possibly_sent=1 AND destination=?`, key, r.TaskRef)
		case "watermark_rewind":
			result, err = tx.ExecContext(ctx, `UPDATE watermarks SET last_comment_id=?,updated_at=? WHERE session_id=? AND last_comment_id=? AND (CAST(? AS INTEGER)=0 OR EXISTS(SELECT 1 FROM comments WHERE session_id=watermarks.session_id AND forge_comment_id=?))`, step.After, now, key, step.Before, step.After, step.After)
		case "graph_dispatch_cancel":
			result, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled',finished_at=?,cancellation_reason=? WHERE id=? AND status=? AND (allocated_run_id IS NULL OR EXISTS(SELECT 1 FROM runs WHERE id=allocated_run_id AND status<>'running'))`, now, "Approved doctor repair "+id, key, step.Before)
		case "graph_wait_cancel":
			result, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='cancelled' WHERE delivery_key=? AND state=? AND (continuation_attempt_id IS NULL OR EXISTS(SELECT 1 FROM graph_node_attempts WHERE id=continuation_attempt_id AND status NOT IN('pending','running')))`, key, step.Before)
		case "graph_attempt_cancel":
			result, err = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='cancelled',finished_at=? WHERE id=? AND status=? AND (run_id IS NULL OR EXISTS(SELECT 1 FROM runs WHERE id=run_id AND status<>'running'))`, now, key, step.Before)
		case "graph_fail":
			result, err = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',finished_at=?,next_due_at=NULL WHERE id=? AND status='running' AND NOT EXISTS(SELECT 1 FROM graph_node_attempts WHERE instance_id=graph_instances.id AND status IN('pending','running')) AND NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=graph_instances.id AND status IN('pending','reserved','running')) AND NOT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=graph_instances.id AND state IN('waiting','claimed','allocated'))`, now, key)
		default:
			return errors.New("unsupported doctor row operation")
		}
		if err != nil {
			return err
		}
		if err = requireAffected(result, "doctor row transition or related precondition"); err != nil {
			return err
		}
		after, _, err := doctorRowSnapshot(ctx, tx, r.TaskRef, step.Kind, step.Target)
		if err != nil {
			return err
		}
		if after != step.After {
			return errors.New("doctor row postcondition failed")
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET state='verified',observed_after=? WHERE repair_id=? AND number=?`, after, id, number); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='applying',revision=revision+1,updated_at=? WHERE id=?`, now, id)
		return err
	})
}

// VerifyDoctorRows rereads a factual settled step without granting new write authority.
func (db *DB) VerifyDoctorRows(ctx context.Context, id string, number int) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, id))
		if err != nil {
			return err
		}
		s, err := doctorRepairStepTx(ctx, tx, id, number)
		if err != nil {
			return err
		}
		state, _, err := doctorRowSnapshot(ctx, tx, r.TaskRef, s.Kind, s.Target)
		if err != nil {
			return err
		}
		if s.State != "verified" || state != s.After {
			return errors.New("doctor repaired row changed")
		}
		return nil
	})
}
