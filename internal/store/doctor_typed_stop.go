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

// BeginDoctorStopRun records the approved backup and owner-stop intent before
// the daemon cancels its own worker. It deliberately precedes exclusion.
func (db *DB) BeginDoctorStopRun(ctx context.Context, id string, number int) (out Run, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		if s.Kind != "stop_run" || !DoctorTypedRowRepairTarget(s.Kind, s.Target, s.Before, s.After) {
			return errors.New("stop tool does not match approved step")
		}
		key, e := strconv.ParseInt(strings.TrimPrefix(s.Target, "run:"), 10, 64)
		if e != nil {
			return e
		}
		out, e = scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE id=?", key))
		if e != nil {
			return e
		}
		if s.State == "verified" {
			if string(out.Status) != s.After {
				return errors.New("settled stop drift")
			}
			return nil
		}
		if s.State != "pending" {
			return errors.New("stop intent already dispatched")
		}
		current, _, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, s.Kind, s.Target)
		if e != nil {
			return e
		}
		if current != s.Before {
			return errors.New("stop before-state drift")
		}
		var n int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND number<? AND state<>'verified'", id, number).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("preceding stop step incomplete")
		}
		var related []byte
		e = tx.QueryRowContext(ctx, `SELECT json_object('session',json((SELECT json_object('id',id,'state',state,'lock_owner',lock_owner,'lock_acquired_at',lock_acquired_at) FROM sessions WHERE id=?)), 'attempts',json((SELECT json_group_array(json_object('id',id,'instance_id',instance_id,'status',status,'outcome',outcome,'finished_at',finished_at)) FROM graph_node_attempts WHERE run_id=?)), 'instances',json((SELECT json_group_array(json_object('id',id,'status',status,'finished_at',finished_at,'next_due_at',next_due_at)) FROM graph_instances WHERE id IN (SELECT instance_id FROM graph_node_attempts WHERE run_id=?))), 'dispatches',json((SELECT json_group_array(json_object('id',id,'status',status,'finished_at',finished_at,'cancellation_reason',cancellation_reason)) FROM graph_dispatches WHERE allocated_run_id=?)))`, out.SessionID, key, key, key).Scan(&related)
		if e != nil {
			return e
		}
		data, e := json.Marshal(struct {
			Run     Run             `json:"run"`
			Related json.RawMessage `json:"related"`
		}{out, related})
		if e != nil {
			return e
		}
		if len(data) > 131072 {
			return errors.New("stop backup too large")
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)", id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='backed_up' WHERE repair_id=? AND number=?", data, doctorDigest(data), fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, number), id, number); e != nil {
			return e
		}
		stop, e := requestOwnerStopTx(ctx, tx, r.TaskRef, 0)
		if e != nil {
			return e
		}
		if stop.RunID != key {
			return errors.New("stop target is not current invocation")
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM doctor_repair_write_permits WHERE repair_id=?", id); e != nil {
			return e
		}
		return nil
	})
	return
}

// CompleteDoctorStopRun is called only after the factory has joined its worker.
// Related attempts, dispatches and the session are backed up and settled with
// the run so the stopped invocation cannot retain active graph work.
func (db *DB) CompleteDoctorStopRun(ctx context.Context, id string, number int) (err error) {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		s, e := doctorRepairStepTx(ctx, tx, id, number)
		if e != nil {
			return e
		}
		if s.Kind != "stop_run" || s.State != "backed_up" || s.BackupReference == "" {
			return errors.New("stop intent missing")
		}
		key, e := strconv.ParseInt(strings.TrimPrefix(s.Target, "run:"), 10, 64)
		if e != nil {
			return e
		}
		run, e := scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE id=?", key))
		if e != nil {
			return e
		}
		if _, ok := run.OwnerStop(); !ok {
			return errors.New("owner stop intent missing")
		}
		if run.Status != RunRunning {
			return errors.New("stop terminal state drift")
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)", id); e != nil {
			return e
		}
		if _, e = finishRunTx(ctx, tx, key, RunOutcome{Status: RunFailed, Error: ReasonOwnerStop, CostUSD: run.CostUSD, CommitSHA: run.CommitSHA, SemanticOutcome: run.SemanticOutcome}, nowUTC()); e != nil {
			return e
		}
		stamp := FormatTime(nowUTC())
		rows, e := tx.QueryContext(ctx, `SELECT id FROM graph_node_attempts WHERE run_id=? AND status IN ('pending','running')`, key)
		if e != nil {
			return e
		}
		var attempts []int64
		for rows.Next() {
			var attempt int64
			if e = rows.Scan(&attempt); e != nil {
				return errors.Join(e, rows.Close())
			}
			attempts = append(attempts, attempt)
		}
		e = errors.Join(rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		for _, attempt := range attempts {
			if _, e = finishGraphAttemptTx(ctx, tx, attempt, GraphFailed, "", nowUTC()); e != nil {
				return e
			}
			if _, e = recordGraphOutputTx(ctx, tx, GraphOutput{NodeAttemptID: attempt, Kind: GraphOutputDiagnostic, Payload: []byte(ReasonOwnerStop), CreatedAt: nowUTC()}); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled',finished_at=?,cancellation_reason=? WHERE allocated_run_id=? AND status IN ('pending','reserved','running')`, stamp, ReasonOwnerStop, key); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='cancelled',finished_at=?,next_due_at=NULL WHERE id IN (SELECT instance_id FROM graph_node_attempts WHERE run_id=?) AND status='running'`, stamp, key); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE sessions SET state='failed',lock_owner=NULL,lock_acquired_at=NULL WHERE id=? AND NOT EXISTS(SELECT 1 FROM runs WHERE session_id=? AND status='running')`, run.SessionID, run.SessionID); e != nil {
			return e
		}
		after, _, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, s.Kind, s.Target)
		if e != nil {
			return e
		}
		if after != s.After {
			return errors.New("stop recheck failed")
		}
		if _, e = tx.ExecContext(ctx, "UPDATE doctor_repair_steps SET state='verified',observed_after=? WHERE repair_id=? AND number=?", after, id, number); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM doctor_repair_write_permits WHERE repair_id=?", id)
		return e
	})
}

// CheckDoctorTargetStopped refuses a drain while any held writer still has a
// running target invocation. Parent writers are joined by the drain without
// cancelling unrelated work. The hold prevents a new invocation racing this check.
func (db *DB) CheckDoctorTargetStopped(ctx context.Context, ref string) error {
	var legacy int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps s JOIN doctor_repair_holds h ON h.repair_id=s.repair_id WHERE h.task_ref=? AND h.active=1 AND s.kind NOT IN ('release_instance','resume_story','park_story','stop_run','requeue_schedule','cancel_schedule','consume_comment','sync_branch')`, ref).Scan(&legacy)
	if err != nil {
		return err
	}
	if legacy > 0 {
		return nil
	}
	var n int
	err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM runs r JOIN doctor_held_session_owners v ON v.session_id=r.session_id JOIN doctor_repair_holds h ON h.repair_id=v.repair_id WHERE h.task_ref=? AND h.active=1 AND r.status='running' AND EXISTS(SELECT 1 FROM sessions target WHERE target.id=r.session_id AND target.task_ref=h.task_ref)`, ref).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return errors.New("live invocation requires an approved stop_run step")
	}
	return nil
}
