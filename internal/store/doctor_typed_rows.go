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

// DoctorTypedRowRepairTarget checks a typed tool's immutable approved transition.
func DoctorTypedRowRepairTarget(kind, target, before, after string) bool {
	prefix, ok := doctorTypedPrefixes[kind]
	if !ok || !strings.HasPrefix(target, prefix+":") {
		return false
	}
	key := strings.TrimPrefix(target, prefix+":")
	n, err := strconv.ParseInt(key, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != key {
		return false
	}
	switch kind {
	case "release_instance", "stop_run":
		return before == "running" && after == "failed"
	case "resume_story":
		return before == "parked" && after == "waiting"
	case "park_story":
		return (before == "running" || before == "done") && after == "parked"
	case "requeue_schedule":
		return before == "running" && after == "pending"
	case "cancel_schedule":
		return (before == "pending" || before == "running") && after == "failed"
	case "consume_comment":
		old, e := strconv.ParseInt(before, 10, 64)
		return e == nil && old >= 0 && strconv.FormatInt(old, 10) == before && n > old && after == key
	}
	return false
}

var doctorTypedPrefixes = map[string]string{"release_instance": "graph", "resume_story": "story", "park_story": "story", "stop_run": "run", "requeue_schedule": "schedule", "cancel_schedule": "schedule", "consume_comment": "comment"}

func doctorTypedRowSnapshot(ctx context.Context, tx *sql.Tx, ref, kind, target string) (string, []byte, error) {
	prefix, ok := doctorTypedPrefixes[kind]
	if !ok || !strings.HasPrefix(target, prefix+":") {
		return "", nil, errors.New("unsupported typed doctor target")
	}
	key := strings.TrimPrefix(target, prefix+":")
	var query string
	switch kind {
	case "release_instance":
		query = `SELECT g.status,json_object('table','graph_instances','id',g.id,'status',g.status,'finished_at',g.finished_at,'next_due_at',g.next_due_at,'implement_stories',json((SELECT json_group_array(json_object('parent',i.parent,'generation',i.generation,'story_key',i.story_key,'last_failure',i.last_failure,'revision',i.revision,'updated_at',i.updated_at)) FROM implement_stories i WHERE i.worker_session=g.session_id AND i.state='running'))) FROM graph_instances g JOIN sessions s ON s.id=g.session_id WHERE g.id=? AND s.task_ref=?`
	case "resume_story", "park_story":
		query = `SELECT i.state,json_object('table','implement_stories','rowid',i.rowid,'parent',i.parent,'generation',i.generation,'story_key',i.story_key,'state',i.state,'reason',i.reason,'merge_commit',i.merge_commit,'revision',i.revision,'updated_at',i.updated_at,'state_at',i.state_at,'park_origin',i.park_origin,'attempts',i.attempts,'last_failure',i.last_failure,'worker_session',i.worker_session,'guidance',i.guidance) FROM implement_stories i WHERE i.rowid=? AND (i.parent=? OR EXISTS(SELECT 1 FROM managed_stories m WHERE m.parent_task=i.parent AND m.generation=i.generation AND m.story_key=i.story_key AND substr(m.parent_task,1,instr(m.parent_task,'#'))||m.issue_number=?))`
	case "stop_run":
		return doctorRowSnapshot(ctx, tx, ref, "run_interrupt", target)
	case "requeue_schedule", "cancel_schedule":
		query = `SELECT q.status,json_object('table','schedules','id',q.id,'status',q.status,'started_at',q.started_at,'finished_at',q.finished_at,'updated_at',q.updated_at,'run_id',q.run_id,'descendants',json((WITH RECURSIVE descendants(id) AS (SELECT schedule_id FROM schedule_prerequisites WHERE prerequisite_id=q.id UNION SELECT p.schedule_id FROM schedule_prerequisites p JOIN descendants d ON p.prerequisite_id=d.id) SELECT json_group_array(json_object('id',x.id,'status',x.status,'finished_at',x.finished_at,'updated_at',x.updated_at)) FROM schedules x WHERE x.id IN descendants AND x.status='pending'))) FROM schedules q JOIN sessions s ON s.id=q.session_id WHERE q.id=? AND s.task_ref=?`
	case "consume_comment":
		query = `SELECT CAST(COALESCE(w.last_comment_id,0) AS TEXT),json_object('table','watermarks','session_id',c.session_id,'last_comment_id',COALESCE(w.last_comment_id,0),'updated_at',w.updated_at) FROM comments c JOIN sessions s ON s.id=c.session_id LEFT JOIN watermarks w ON w.session_id=c.session_id WHERE c.forge_comment_id=? AND s.task_ref=?`
	default:
		return "", nil, errors.New("unsupported typed doctor row operation")
	}
	var state string
	var backup []byte
	args := []any{key, ref}
	if kind == "resume_story" || kind == "park_story" {
		args = append(args, ref)
	}
	err := tx.QueryRowContext(ctx, query, args...).Scan(&state, &backup)
	return state, backup, err
}

// ApplyDoctorTypedRows backs up, applies and rechecks one approved row tool atomically.
// The caller supplies only its closed tool name and the reserved step number.
func (db *DB) ApplyDoctorTypedRows(ctx context.Context, id string, number int, kind string) (out DoctorRepairStep, err error) {
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
		if s.Kind != kind || !DoctorTypedRowRepairTarget(kind, s.Target, s.Before, s.After) {
			return errors.New("doctor typed tool does not match approved step")
		}
		current, data, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, kind, s.Target)
		if e != nil {
			return e
		}
		if s.State == "verified" {
			if current != s.After {
				return errors.New("settled doctor typed row drift")
			}
			out = s
			return nil
		}
		if s.State != "pending" || current != s.Before {
			return errors.New("doctor typed row precondition changed")
		}
		if kind == "consume_comment" || kind == "stop_run" {
			return errors.New("doctor tool requires factory effect adapter")
		}
		var preceding int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND number<? AND state<>'verified'`, id, number).Scan(&preceding); e != nil {
			return e
		}
		if preceding != 0 {
			return errors.New("doctor preceding step incomplete")
		}
		if kind == "cancel_schedule" {
			var snapshot map[string]json.RawMessage
			if e = json.Unmarshal(data, &snapshot); e != nil {
				return e
			}
			snapshot["repair_reason"], e = json.Marshal("Owner cancelled schedule through approved doctor repair " + id)
			if e != nil {
				return e
			}
			data, e = json.Marshal(snapshot)
			if e != nil {
				return e
			}
		}
		if len(data) > 131072 || !json.Valid(data) {
			return errors.New("doctor typed row backup malformed or too large")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='backed_up' WHERE repair_id=? AND number=?`, data, doctorDigest(data), fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, number), id, number); e != nil {
			return e
		}
		key := strings.TrimPrefix(s.Target, doctorTypedPrefixes[kind]+":")
		now := nowUTC()
		stamp := FormatTime(now)
		reason := "Approved doctor repair " + id
		var res sql.Result
		switch kind {
		case "release_instance":
			res, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',finished_at=?,next_due_at=NULL WHERE id=? AND status='running' AND NOT EXISTS(SELECT 1 FROM graph_node_attempts WHERE instance_id=graph_instances.id AND status IN('pending','running')) AND NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=graph_instances.id AND status IN('pending','reserved','running')) AND NOT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=graph_instances.id AND state IN('waiting','claimed','allocated')) AND NOT EXISTS(SELECT 1 FROM runs WHERE session_id=graph_instances.session_id AND status='running') AND NOT EXISTS(SELECT 1 FROM sessions WHERE id=graph_instances.session_id AND (state='running' OR lock_owner IS NOT NULL))`, stamp, key)
			if e == nil {
				_, e = tx.ExecContext(ctx, `UPDATE implement_stories SET last_failure=?,revision=revision+1,updated_at=? WHERE worker_session=(SELECT session_id FROM graph_instances WHERE id=?) AND state='running' AND EXISTS(SELECT 1 FROM graph_instances g JOIN graph_definitions d ON d.id=g.definition_id WHERE g.id=? AND d.mode='implement')`, reason, stamp, key, key)
			}
			if e == nil {
				_, e = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) SELECT a.id,'diagnostic',?,? FROM graph_node_attempts a WHERE a.instance_id=? ORDER BY a.id DESC LIMIT 1`, []byte(reason), stamp, key)
			}
		case "resume_story":
			res, e = tx.ExecContext(ctx, `UPDATE implement_stories SET state='waiting',reason='',merge_commit='',revision=revision+1,updated_at=?,state_at=?,park_origin='',attempts=0,last_failure='',worker_session=0,guidance='' WHERE rowid=? AND state='parked' AND NOT EXISTS(SELECT 1 FROM runs WHERE session_id=implement_stories.worker_session AND status='running')`, stamp, stamp, key)
		case "park_story":
			res, e = tx.ExecContext(ctx, `UPDATE implement_stories SET state='parked',reason=?,merge_commit='',revision=revision+1,updated_at=?,state_at=?,park_origin=CASE WHEN state='running' THEN 'worker' ELSE 'merge' END WHERE rowid=? AND state=? AND NOT EXISTS(SELECT 1 FROM runs WHERE session_id=implement_stories.worker_session AND status='running')`, reason, stamp, stamp, key, s.Before)
		case "requeue_schedule":
			res, e = tx.ExecContext(ctx, `UPDATE schedules SET status='pending',started_at=NULL,updated_at=? WHERE id=? AND status='running' AND (run_id IS NULL OR NOT EXISTS(SELECT 1 FROM runs WHERE id=schedules.run_id AND status='running'))`, stamp, key)
		case "cancel_schedule":
			res, e = tx.ExecContext(ctx, `UPDATE schedules SET status='failed',finished_at=?,updated_at=? WHERE id=? AND status=? AND (run_id IS NULL OR NOT EXISTS(SELECT 1 FROM runs WHERE id=schedules.run_id AND status='running'))`, stamp, stamp, key, s.Before)
			if e == nil {
				n, parseErr := strconv.ParseInt(key, 10, 64)
				if parseErr != nil {
					return parseErr
				}
				e = blockDescendants(ctx, tx, n, now)
			}
		}
		if e != nil {
			return e
		}
		if e = requireAffected(res, "doctor typed row transition or related precondition"); e != nil {
			return e
		}
		after, _, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, kind, s.Target)
		if e != nil {
			return e
		}
		if after != s.After {
			return errors.New("doctor typed row recheck failed")
		}
		ref := fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, number)
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='verified',observed_after=? WHERE repair_id=? AND number=?`, data, doctorDigest(data), ref, after, id, number); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='applying',revision=revision+1,updated_at=? WHERE id=?`, stamp, id); e != nil {
			return e
		}
		out, e = doctorRepairStepTx(ctx, tx, id, number)
		return e
	})
	return out, err
}

// ApplyDoctorConsumeComment completes a factory-authored, remotely confirmed
// reply and consumes only the comment named by the approved step. Confirmation
// is supplied through the existing durable delivery ledger, never by the agent.
func (db *DB) ApplyDoctorConsumeComment(ctx context.Context, id string, number int, deliveryKey string) (out DoctorRepairStep, err error) {
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
		if s.Kind != "consume_comment" || !DoctorTypedRowRepairTarget(s.Kind, s.Target, s.Before, s.After) {
			return errors.New("doctor consume tool does not match approved step")
		}
		current, data, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, s.Kind, s.Target)
		if e != nil {
			return e
		}
		d, e := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, deliveryKey))
		if e != nil {
			return e
		}
		target, e := strconv.ParseInt(s.After, 10, 64)
		if e != nil {
			return e
		}
		var session int64
		if e = tx.QueryRowContext(ctx, `SELECT c.session_id FROM comments c JOIN sessions x ON x.id=c.session_id WHERE c.forge_comment_id=? AND x.task_ref=?`, target, r.TaskRef).Scan(&session); e != nil {
			return e
		}
		if d.Version != 1 || d.Legacy || d.SessionID != session || d.Task != r.TaskRef || d.Destination != r.TaskRef || d.TriggerID != target || d.TriggerNamespace != "comment" || d.Purpose != "reply" || d.RunID != 0 || d.BotUserID <= 0 || d.CommentID <= 0 || !d.PossiblySent {
			return errors.New("doctor consume requires matching confirmed reply delivery")
		}
		if s.State == "verified" {
			if current != s.After || !d.Completed {
				return errors.New("settled doctor comment drift")
			}
			out = s
			return nil
		}
		if s.State != "pending" || current != s.Before || d.Completed {
			return errors.New("doctor consume precondition changed")
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id=? AND number<? AND state<>'verified'`, id, number).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("doctor preceding step incomplete")
		}
		if len(data) > 131072 || !json.Valid(data) {
			return errors.New("doctor consume backup malformed or too large")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); e != nil {
			return e
		}
		reference := fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, number)
		backup := struct {
			Watermark json.RawMessage `json:"watermark"`
			Delivery  ReportDelivery  `json:"delivery"`
		}{data, d}
		data, e = json.Marshal(backup)
		if e != nil {
			return e
		}
		if len(data) > 131072 {
			return errors.New("doctor consume delivery backup too large")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,state='backed_up' WHERE repair_id=? AND number=?`, data, doctorDigest(data), reference, id, number); e != nil {
			return e
		}
		if e = consumeReportSource(ctx, tx, session, target); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE report_deliveries SET completed=1 WHERE delivery_key=? AND completed=0`, deliveryKey); e != nil {
			return e
		}
		after, _, e := doctorTypedRowSnapshot(ctx, tx, r.TaskRef, s.Kind, s.Target)
		if e != nil {
			return e
		}
		if after != s.After {
			return errors.New("doctor consume recheck failed")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET state='verified',observed_after=? WHERE repair_id=? AND number=?`, after, id, number); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='applying',revision=revision+1,updated_at=? WHERE id=?`, FormatTime(nowUTC()), id); e != nil {
			return e
		}
		out, e = doctorRepairStepTx(ctx, tx, id, number)
		return e
	})
	return out, err
}
