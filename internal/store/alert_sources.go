package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// AlertTaskSource contains authoritative progress and waits, including waits
// not published to the owner overview. No text or provider output is selected.
// HasWork distinguishes actual execution or retained work from a discovery-only
// needs_poll flag. Waiting remains separately authoritative.
type AlertTaskSource struct {
	Task                                string
	SessionID                           int64
	Eligible, Waiting, HasWork, Running bool
	ProgressAt                          time.Time
	Version                             string
}

// AlertTaskSources is bounded; incomplete scans cannot establish recovery.
func (db *DB) AlertTaskSources(ctx context.Context, at time.Time) (out []AlertTaskSource, complete bool, err error) {
	complete = true
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT s.task_ref,s.id,
 EXISTS(SELECT 1 FROM doctor_held_session_owners h WHERE h.session_id=s.id) OR
 EXISTS(SELECT 1 FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id WHERE g.session_id=s.id AND g.status='running' AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND w.state IN ('waiting','claimed')) OR
 EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.status='running' AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND g.next_due_at>?),
 EXISTS(SELECT 1 FROM graph_dispatches d JOIN graph_instances g ON g.id=d.instance_id WHERE d.session_id=s.id AND d.status='pending' AND d.due_at<=? AND g.status='running' AND g.current_node=d.node AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id)),
 EXISTS(SELECT 1 FROM schedules p WHERE p.session_id=s.id AND p.status='pending' AND p.due_at<=?),
 EXISTS(SELECT 1 FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=s.id AND o.assigned_attempt_id IS NULL AND c.classification='instruction' AND (o.target_instance_id=0 OR EXISTS(SELECT 1 FROM graph_instances g WHERE g.id=o.target_instance_id AND g.session_id=s.id AND g.status='running' AND g.current_node=o.target_node AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND (o.target_launch_id=0 OR EXISTS(SELECT 1 FROM graph_dispatches d WHERE d.instance_id=g.id AND d.node=g.current_node AND d.launch_id=o.target_launch_id AND d.status='pending'))))),
 s.state='running' OR EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status IN ('running','prelaunch')),
 EXISTS(SELECT 1 FROM schedules p WHERE p.session_id=s.id AND p.status='pending'),
 EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.status='running' AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id)),
 EXISTS(SELECT 1 FROM automatic_continuations c JOIN segments z ON z.id=c.segment_id WHERE c.session_id=s.id AND c.status='pending' AND c.due_at>? AND c.segment_id=(SELECT max(x.id) FROM segments x WHERE x.session_id=s.id) AND json_valid(c.context_json) AND json_extract(c.context_json,'$.Task')=s.task_ref AND json_extract(c.context_json,'$.Worktree')=COALESCE(s.worktree_path,'') AND json_extract(c.context_json,'$.ResumeIdentity')<>'' AND json_extract(c.context_json,'$.ResumeIdentity')=CASE WHEN z.agent='claude' THEN z.session_ref ELSE COALESCE(z.resume_token,'') END),
 EXISTS(SELECT 1 FROM automatic_continuations c JOIN segments z ON z.id=c.segment_id WHERE c.session_id=s.id AND c.status='pending' AND c.due_at<=? AND c.segment_id=(SELECT max(x.id) FROM segments x WHERE x.session_id=s.id) AND json_valid(c.context_json) AND json_extract(c.context_json,'$.Task')=s.task_ref AND json_extract(c.context_json,'$.Worktree')=COALESCE(s.worktree_path,'') AND json_extract(c.context_json,'$.ResumeIdentity')<>'' AND json_extract(c.context_json,'$.ResumeIdentity')=CASE WHEN z.agent='claude' THEN z.session_ref ELSE COALESCE(z.resume_token,'') END),
 EXISTS(SELECT 1 FROM automatic_continuations c WHERE c.session_id=s.id AND c.status='pending'),
 max(s.created_at,COALESCE((SELECT max(started_at) FROM runs r WHERE r.session_id=s.id),s.created_at),COALESCE((SELECT max(finished_at) FROM runs r WHERE r.session_id=s.id),s.created_at),COALESCE((SELECT max(t.at) FROM graph_transitions t JOIN graph_instances g ON g.id=t.instance_id WHERE g.session_id=s.id),s.created_at),COALESCE((SELECT max(o.created_at) FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id JOIN graph_instances g ON g.id=a.instance_id WHERE g.session_id=s.id),s.created_at)),
 COALESCE((SELECT g.current_node FROM graph_instances g WHERE g.session_id=s.id ORDER BY g.id DESC LIMIT 1),'')||':result:'||COALESCE((SELECT sum(a.revision) FROM application_results a WHERE a.task=s.task_ref AND a.workflow NOT IN ('doctor','judge')),0)
 FROM sessions s WHERE NOT (`+alertTaskTerminalSQL+`) AND (s.state='running' OR s.needs_poll=1 OR EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.status='running') OR EXISTS(SELECT 1 FROM schedules p WHERE p.session_id=s.id AND p.status='pending') OR EXISTS(SELECT 1 FROM owner_instruction_inputs o WHERE o.session_id=s.id AND o.assigned_attempt_id IS NULL) OR EXISTS(SELECT 1 FROM automatic_continuations c WHERE c.session_id=s.id AND c.status='pending') OR EXISTS(SELECT 1 FROM doctor_held_session_owners h WHERE h.session_id=s.id)) ORDER BY s.id LIMIT 1001`, FormatTime(at), FormatTime(at), FormatTime(at), FormatTime(at), FormatTime(at))
		if err != nil {
			return err
		}
		for rows.Next() {
			var v AlertTaskSource
			var stamp string
			var dispatch, schedule, input, running, scheduled, graph, futureContinuation, dueContinuation, continuation bool
			if err = rows.Scan(&v.Task, &v.SessionID, &v.Waiting, &dispatch, &schedule, &input, &running, &scheduled, &graph, &futureContinuation, &dueContinuation, &continuation, &stamp, &v.Version); err != nil {
				return errors.Join(err, rows.Close())
			}
			v.ProgressAt, err = ParseTime(stamp)
			if err != nil {
				return errors.Join(err, rows.Close())
			}
			actionable := dispatch || schedule || input || dueContinuation
			if (scheduled || futureContinuation) && !actionable && !running {
				v.Waiting = true
			}
			v.Running = running
			v.HasWork = running || graph || scheduled || actionable || continuation
			v.Eligible = actionable && !running && !v.Waiting
			if len(out) == 1000 {
				complete = false
				break
			}
			out = append(out, v)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		planComplete, err := alertPlanWaits(ctx, tx, &out)
		complete = complete && planComplete
		return err
	})
	return out, complete, err
}

// alertPlanWaits reads the policy ledger itself. Publication is deliberately
// irrelevant: an unpublished owner wait is still an authoritative work wait.
func alertPlanWaits(ctx context.Context, tx *sql.Tx, out *[]AlertTaskSource) (bool, error) {
	complete := true
	rows, err := tx.QueryContext(ctx, `SELECT p.parent,s.repo_id,CASE WHEN length(CAST(p.state_json AS BLOB))<=1048576 THEN p.state_json ELSE '' END,length(CAST(p.state_json AS BLOB)) FROM plan_workflow_states p JOIN sessions s ON s.task_ref=p.parent WHERE NOT (`+alertTaskTerminalSQL+`) AND p.parent NOT IN (SELECT parent_task FROM managed_parent_closures) AND COALESCE((SELECT g.status FROM graph_instances g JOIN graph_definitions d ON d.id=g.definition_id WHERE g.session_id=s.id AND (d.workflow='plan' OR d.mode='plan') ORDER BY g.id DESC LIMIT 1),'running')='running' ORDER BY s.id LIMIT 129`)
	if err != nil {
		return false, err
	}
	type candidate struct {
		parent, raw string
		repo        int64
		size        int
	}
	var candidates []candidate
	total := 0
	for rows.Next() {
		var p candidate
		if err = rows.Scan(&p.parent, &p.repo, &p.raw, &p.size); err != nil {
			return false, errors.Join(err, rows.Close())
		}
		total += p.size
		if len(candidates) >= 128 || p.size > 1048576 || total > 8*1048576 {
			complete = false
			continue
		}
		candidates = append(candidates, p)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return false, err
	}
	mark := func(task string) error {
		for i := range *out {
			if (*out)[i].Task == task {
				(*out)[i].Waiting = true
				(*out)[i].Eligible = false
				return nil
			}
		}
		if len(*out) >= 1000 {
			complete = false
			return nil
		}
		var v AlertTaskSource
		var stamp string
		var terminal bool
		err := tx.QueryRowContext(ctx, `SELECT s.id,s.created_at,`+alertTaskTerminalSQL+` FROM sessions s WHERE s.task_ref=?`, task).Scan(&v.SessionID, &stamp, &terminal)
		if errors.Is(err, sql.ErrNoRows) {
			complete = false
			return nil
		}
		if err != nil {
			return err
		}
		if terminal {
			return nil
		}
		v.ProgressAt, err = ParseTime(stamp)
		if err != nil {
			return err
		}
		v.Task = task
		v.Waiting = true
		*out = append(*out, v)
		return nil
	}
	for _, p := range candidates {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		var state planbrief.PlanState
		if json.Unmarshal([]byte(p.raw), &state) != nil || len(state.Stories) > 256 {
			complete = false
			continue
		}
		parentWaiting := state.AssembledWait != "" && state.AssembledWait == state.PlanSetRevision()
		if set := state.StorySet; set != nil && !set.Promoted && set.Revision != "" {
			parentWaiting = true
		}
		for _, story := range state.Stories {
			if !planStoryWaitCurrent(&state, story) {
				continue
			}
			parentWaiting = true
			var child string
			err = tx.QueryRowContext(ctx, `SELECT s.task_ref FROM plan_story_drafts d JOIN sessions s ON s.repo_id=? AND s.issue_number=d.issue_number WHERE d.parent=? AND d.story_key=? AND d.issue_id=? AND d.retired=0 LIMIT 1`, p.repo, p.parent, story.Key, story.Wait.IssueID).Scan(&child)
			if errors.Is(err, sql.ErrNoRows) {
				complete = false
				continue
			}
			if err != nil {
				return false, err
			}
			if err = mark(child); err != nil {
				return false, err
			}
		}
		if parentWaiting {
			if err = mark(p.parent); err != nil {
				return false, err
			}
		}
	}
	return complete, nil
}

// AlertReportSources selects original delivery uncertainty without replaying it.
func (db *DB) AlertReportSources(ctx context.Context) ([]ReportDelivery, bool, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE possibly_sent=1 AND completed=0 AND comment_id IS NULL ORDER BY rowid LIMIT 1001`)
	if err != nil {
		return nil, false, err
	}
	var out []ReportDelivery
	for rows.Next() {
		d, e := scanReportDelivery(rows)
		if e != nil {
			return nil, false, errors.Join(e, rows.Close())
		}
		out = append(out, d)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, false, err
	}
	complete := len(out) <= 1000
	if !complete {
		return out[:1000], false, nil
	}
	rows, err = db.sql.QueryContext(ctx, `SELECT r.recovery_key,r.session_id,r.trigger_id,r.task,COALESCE(r.run_id,0) FROM report_recoveries r LEFT JOIN report_deliveries d ON d.delivery_key=r.delivery_key WHERE r.state='uncertain' AND d.delivery_key IS NULL ORDER BY r.recovery_key LIMIT 1001`)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		var d ReportDelivery
		var key string
		if err = rows.Scan(&key, &d.SessionID, &d.TriggerID, &d.Task, &d.RunID); err != nil {
			return nil, false, errors.Join(err, rows.Close())
		}
		d.Key = "recovery-source:" + key
		d.PossiblySent = true
		if len(out) >= 1000 {
			complete = false
			break
		}
		out = append(out, d)
	}
	return out, complete, errors.Join(rows.Err(), rows.Close())
}

// AlertReportSettled checks the original immutable source, not a replacement
// alert or the mere absence of the uncertain row in a bounded scan.
func (db *DB) AlertReportSettled(ctx context.Context, key string) (bool, error) {
	if strings.HasPrefix(key, "recovery-source:") {
		return db.alertRecoverySettled(ctx, key)
	}
	var settled bool
	err := db.sql.QueryRowContext(ctx, `SELECT completed=1 OR comment_id IS NOT NULL FROM report_deliveries WHERE delivery_key=?`, key).Scan(&settled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return settled, err
}

// AlertReportUncertainSince retains the original boundary, using an explicit
// approximate first observation only for reports from before this migration.
func (db *DB) AlertReportUncertainSince(ctx context.Context, key string, at time.Time) (time.Time, error) {
	if strings.HasPrefix(key, "recovery-source:") {
		return db.alertRecoveryUncertainSince(ctx, key, at)
	}
	var stamp string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_report_uncertainty(delivery_key,first_at,approximate) VALUES(?,?,1) ON CONFLICT DO NOTHING`, key, FormatTime(at)); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT first_at FROM alert_report_uncertainty WHERE delivery_key=?`, key).Scan(&stamp)
	})
	if err != nil {
		return time.Time{}, err
	}
	return ParseTime(stamp)
}

// AlertTaskSettled requires consumption or terminal dispatch settlement.
func (db *DB) AlertTaskSettled(ctx context.Context, kind, key string) (bool, error) {
	var settled bool
	if kind == "queue" {
		var session, comment int64
		if _, err := fmt.Sscanf(key, "%d:%d", &session, &comment); err != nil {
			return false, err
		}
		if comment < 0 || IsPrivateTrigger(comment) {
			return false, nil
		}
		var err error
		if comment == 0 {
			err = db.sql.QueryRowContext(ctx, `SELECT ticket_body_pending=0 FROM watermarks WHERE session_id=?`, session).Scan(&settled)
		} else {
			err = db.sql.QueryRowContext(ctx, `SELECT last_comment_id>=? FROM watermarks WHERE session_id=?`, comment, session).Scan(&settled)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return settled, err
	}
	if kind == "schedule" {
		err := db.sql.QueryRowContext(ctx, `SELECT status IN ('succeeded','failed') FROM schedules WHERE id=?`, key).Scan(&settled)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return settled, err
	}
	if kind == "dispatch" {
		err := db.sql.QueryRowContext(ctx, `SELECT status IN ('finished','cancelled') FROM graph_dispatches WHERE id=?`, key).Scan(&settled)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return settled, err
	}
	return false, nil
}

// alertTaskTerminalSQL is shared by both source paths and recovery. Backlog
// retirement or the latest delivered/finished workflow is affirmative evidence;
// retained historical input alone cannot override it. New admitted input and
// live execution, schedules or continuations still own work.
const alertTaskTerminalSQL = `s.state<>'running'
 AND NOT EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status IN ('running','prelaunch'))
 AND NOT EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND g.status='running')
 AND NOT EXISTS(SELECT 1 FROM schedules p WHERE p.session_id=s.id AND p.status='pending')
 AND NOT EXISTS(SELECT 1 FROM automatic_continuations c WHERE c.session_id=s.id AND c.status='pending')
 AND (s.needs_poll=0 OR EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND g.status='succeeded' AND g.current_node IN ('delivered','finished')
 AND NOT EXISTS(SELECT 1 FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=s.id AND o.assigned_attempt_id IS NULL AND c.classification='instruction' AND max(c.created_at,c.recorded_at)>g.finished_at)))`

// AlertTaskTerminal requires affirmative completion and no current work owner.
func (db *DB) AlertTaskTerminal(ctx context.Context, task string) (bool, error) {
	var terminal bool
	err := db.sql.QueryRowContext(ctx, `SELECT `+alertTaskTerminalSQL+` FROM sessions s WHERE s.task_ref=?`, task).Scan(&terminal)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return terminal, err
}

// Recovery-only debt keeps a first-observed approximate timer and immutable
// original source identity in the alert journal, outside the delivery-key FK.
func (db *DB) alertRecoveryUncertainSince(ctx context.Context, key string, at time.Time) (time.Time, error) {
	recovery, err := db.GetReportRecovery(ctx, strings.TrimPrefix(key, "recovery-source:"))
	if err != nil {
		return time.Time{}, err
	}
	var first time.Time
	err = db.UpdateAlerts(ctx, func(l *AlertLedger) error {
		evidence, ok := l.Evidence[key]
		if !ok {
			evidence = AlertEvidence{ID: key, Source: recovery.Key, Class: "report_recovery_uncertain", At: at, Approximate: true, SourceSessionID: recovery.SessionID, SourceTriggerID: recovery.TriggerID, SourceRunID: recovery.RunID, SourceDeliveryKey: recovery.DeliveryKey}
			l.Evidence[key] = evidence
		}
		first = evidence.At
		return nil
	})
	return first, err
}
func (db *DB) alertRecoverySettled(ctx context.Context, key string) (bool, error) {
	recovery, err := db.GetReportRecovery(ctx, strings.TrimPrefix(key, "recovery-source:"))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if err == nil && recovery.State == "resolved" {
		return true, nil
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil {
		return false, err
	}
	evidence, ok := ledger.Evidence[key]
	if !ok {
		return false, nil
	}
	if evidence.SourceDeliveryKey != "" {
		var confirmed bool
		err = db.sql.QueryRowContext(ctx, `SELECT completed=1 OR comment_id IS NOT NULL FROM report_deliveries WHERE delivery_key=? AND session_id=? AND trigger_id=?`, evidence.SourceDeliveryKey, evidence.SourceSessionID, evidence.SourceTriggerID).Scan(&confirmed)
		if err == nil && confirmed {
			return true, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	if evidence.SourceRunID > 0 {
		var confirmed bool
		err = db.sql.QueryRowContext(ctx, `SELECT status='reported' AND result_comment_id IS NOT NULL AND result_comment_id>0 FROM runs WHERE id=? AND session_id=?`, evidence.SourceRunID, evidence.SourceSessionID).Scan(&confirmed)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return confirmed, err
	}
	return false, nil
}
