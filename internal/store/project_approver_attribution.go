package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// legacyRecordActor resolves original authored evidence rather than treating
// current configuration as the author of every old record. Zero means unknown.
func legacyRecordActor(ctx context.Context, tx *sql.Tx, kind, key string) (int64, error) {
	var actor int64
	var err error
	switch kind {
	case "doctor_instruction":
		err = tx.QueryRowContext(ctx, `SELECT actor_id FROM doctor_inbox WHERE id=?`, key).Scan(&actor)
	case "doctor_effect":
		err = tx.QueryRowContext(ctx, `SELECT i.actor_id FROM report_deliveries d JOIN doctor_inbox i ON i.id=d.doctor_command_id WHERE d.delivery_key=?`, key).Scan(&actor)
	case "owner_exception":
		err = tx.QueryRowContext(ctx, `SELECT actor_id FROM owner_exceptions WHERE id=?`, key).Scan(&actor)
	case "followup_approval":
		err = tx.QueryRowContext(ctx, `SELECT actor_id FROM followup_proposals WHERE proposal_key=? AND approval_comment>0`, key).Scan(&actor)
	case "story_refinement":
		err = tx.QueryRowContext(ctx, `SELECT actor_id FROM story_refinements WHERE comment_id=?`, key).Scan(&actor)
	case "creation_intent":
		err = tx.QueryRowContext(ctx, `SELECT owner_id FROM mcp_creation_intents WHERE id=?`, key).Scan(&actor)
	case "run":
		id, e := strconv.ParseInt(key, 10, 64)
		if e != nil {
			return 0, e
		}
		return legacyRunActor(ctx, tx, id, 0)
	case "session":
		id, e := strconv.ParseInt(key, 10, 64)
		if e != nil {
			return 0, e
		}
		return legacySessionActor(ctx, tx, id)
	case "plan_state":
		var session int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM sessions WHERE task_ref=?`, key).Scan(&session)
		if err == nil {
			return legacySessionActor(ctx, tx, session)
		}
	case "plan_authority":
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT authority_json FROM plan_story_authorities WHERE parent||':'||request_id=?`, key).Scan(&raw)
		if err == nil {
			var v any
			if e := json.Unmarshal([]byte(raw), &v); e != nil {
				return 0, e
			}
			var ids []int64
			collectOwnerIDs(v, map[int64]bool{}, &ids)
			if len(ids) == 1 {
				return ids[0], nil
			}
			return 0, nil
		}
	case "schedule":
		var session, source int64
		err = tx.QueryRowContext(ctx, `SELECT session_id,source_comment_id FROM schedules WHERE id=?`, key).Scan(&session, &source)
		if err == nil {
			return legacySourceActor(ctx, tx, session, source)
		}
	case "continuation":
		var run int64
		err = tx.QueryRowContext(ctx, `SELECT root_run_id FROM automatic_continuations WHERE id=?`, key).Scan(&run)
		if err == nil {
			return legacyRunActor(ctx, tx, run, 0)
		}
	case "report_effect":
		var run, session, trigger int64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(run_id,0),COALESCE(session_id,0),trigger_id FROM report_deliveries WHERE delivery_key=?`, key).Scan(&run, &session, &trigger)
		if err == nil {
			if run > 0 {
				return legacyRunActor(ctx, tx, run, 0)
			}
			if trigger > 0 && trigger < chatTriggerBase {
				return legacySourceActor(ctx, tx, session, trigger)
			}
			return 0, nil
		}
	case "owner_wait":
		var run, instance int64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(w.reply_author_id,0),COALESCE(a.run_id,0),g.id FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id WHERE w.delivery_key=?`, key).Scan(&actor, &run, &instance)
		if err == nil {
			if actor > 0 {
				return actor, nil
			}
			if run > 0 {
				return legacyRunActor(ctx, tx, run, 0)
			}
			return legacyRecordActor(ctx, tx, "graph_instance", strconv.FormatInt(instance, 10))
		}
	case "graph_instance":
		var session, source int64
		err = tx.QueryRowContext(ctx, `SELECT g.session_id,COALESCE((SELECT trigger_id FROM research_launch_definitions l WHERE l.instance_id=g.id ORDER BY id LIMIT 1),(SELECT r.trigger_comment_id FROM graph_node_attempts a JOIN runs r ON r.id=a.run_id WHERE a.instance_id=g.id ORDER BY a.id LIMIT 1),0) FROM graph_instances g WHERE g.id=?`, key).Scan(&session, &source)
		if err == nil {
			return legacySourceActor(ctx, tx, session, source)
		}
	case "graph_effect":
		var run int64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(a.run_id,0) FROM graph_effects e LEFT JOIN graph_node_attempts a ON a.id=e.source_attempt_id WHERE e.operation=?`, key).Scan(&run)
		if err == nil && run > 0 {
			return legacyRunActor(ctx, tx, run, 0)
		}
	case "story_progress_effect":
		var run int64
		err = tx.QueryRowContext(ctx, `SELECT run_id FROM story_progress_effects WHERE id=?`, key).Scan(&run)
		if err == nil {
			return legacyRunActor(ctx, tx, run, 0)
		}
	case "acceptance_decision":
		var run int64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(e.actor_id,0),COALESCE(d.run_id,0) FROM acceptance_decisions d LEFT JOIN owner_exceptions e ON e.id=d.exception_id WHERE d.id=?`, key).Scan(&actor, &run)
		if err == nil {
			if actor > 0 {
				return actor, nil
			}
			if run > 0 {
				return legacyRunActor(ctx, tx, run, 0)
			}
		}
	case "bug_analysis_approval":
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(approval_owner_id,0) FROM bug_analyses WHERE invocation=?`, key).Scan(&actor)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return actor, err
}
func legacySourceActor(ctx context.Context, tx *sql.Tx, session, source int64) (int64, error) {
	if session <= 0 || source <= 0 || source >= chatTriggerBase {
		return 0, nil
	}
	var actor int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(author_user_id,0) FROM comments WHERE session_id=? AND forge_comment_id=? AND author_is_bot=0`, session, source).Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return actor, err
}
func legacySessionActor(ctx context.Context, tx *sql.Tx, session int64) (int64, error) {
	var actor int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(author_user_id),0) FROM comments WHERE session_id=? AND classification='instruction' AND author_is_bot=0 AND forge_comment_id>0 AND forge_comment_id<? HAVING COUNT(DISTINCT author_user_id)=1`, session, chatTriggerBase).Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return actor, err
}
func legacyRunActor(ctx context.Context, tx *sql.Tx, run int64, depth int) (int64, error) {
	if run <= 0 || depth > 8 {
		return 0, nil
	}
	var session, trigger int64
	err := tx.QueryRowContext(ctx, `SELECT session_id,trigger_comment_id FROM runs WHERE id=?`, run).Scan(&session, &trigger)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if trigger < chatTriggerBase {
		return legacySourceActor(ctx, tx, session, trigger)
	}
	if IsChatTrigger(trigger) {
		return 0, nil
	}
	if IsScheduleTrigger(trigger) {
		var source int64
		err = tx.QueryRowContext(ctx, `SELECT source_comment_id FROM schedules WHERE id=? AND session_id=?`, trigger-scheduleTriggerBase, session).Scan(&source)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		return legacySourceActor(ctx, tx, session, source)
	}
	if IsAutomaticTrigger(trigger) {
		var root int64
		err = tx.QueryRowContext(ctx, `SELECT root_run_id FROM automatic_continuations WHERE id=? AND session_id=?`, trigger-automaticTriggerBase, session).Scan(&root)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		return legacyRunActor(ctx, tx, root, depth+1)
	}
	if IsGraphTrigger(trigger) {
		var source int64
		err = tx.QueryRowContext(ctx, `SELECT l.trigger_id FROM graph_dispatches d JOIN research_launch_definitions l ON l.id=d.launch_id WHERE d.id=? AND d.session_id=?`, trigger-graphTriggerBase, session).Scan(&source)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		return legacySourceActor(ctx, tx, session, source)
	}
	return 0, nil
}

// checkLegacyOutstandingAttribution fails before seeding authority when an
// unfinished remote action cannot identify its original instruction author.
func checkLegacyOutstandingAttribution(ctx context.Context, tx *sql.Tx, repoID int64, expected string) error {
	queries := []struct{ kind, sql string }{
		{"story_progress_effect", `SELECT CAST(e.id AS TEXT) FROM story_progress_effects e JOIN sessions s ON s.task_ref=e.parent_task WHERE s.repo_id=? AND e.delivered=0 AND e.superseded=0`},
		{"acceptance_decision", `SELECT CAST(e.id AS TEXT) FROM acceptance_decisions e JOIN sessions s ON s.task_ref=e.parent_task WHERE s.repo_id=? AND (e.delivered=0 OR e.projected=0)`},
		{"continuation", `SELECT CAST(c.id AS TEXT) FROM automatic_continuations c JOIN sessions s ON s.id=c.session_id WHERE s.repo_id=? AND c.status IN ('pending','reserved','running')`},
		{"graph_instance", `SELECT CAST(g.id AS TEXT) FROM graph_instances g JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND g.status='running'`},
		{"run", `SELECT CAST(r.id AS TEXT) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.repo_id=? AND r.status<>'reported' AND NOT (r.trigger_comment_id>? AND r.trigger_comment_id<?)`},
		{"report_effect", `SELECT d.delivery_key FROM report_deliveries d JOIN sessions s ON s.id=d.session_id WHERE s.repo_id=? AND d.completed=0 AND d.trigger_namespace<>'chat'`},
		{"owner_wait", `SELECT w.delivery_key FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND w.state IN ('waiting','claimed')`},
		{"graph_effect", `SELECT e.operation FROM graph_effects e JOIN graph_instances g ON g.id=e.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND e.state IN ('intent','dispatched','uncertain')`},
		{"plan_authority", `SELECT a.parent||':'||a.request_id FROM plan_story_authorities a JOIN sessions s ON s.task_ref=a.parent WHERE s.repo_id=? AND NOT EXISTS(SELECT 1 FROM plan_story_completions c WHERE c.parent=a.parent AND c.request_id=a.request_id)`},
	}
	for _, q := range queries {
		args := []any{repoID}
		if q.kind == "run" {
			args = append(args, chatTriggerBase, graphTriggerBase)
		}
		rows, e := tx.QueryContext(ctx, q.sql, args...)
		if e != nil {
			return e
		}
		var keys []string
		for rows.Next() {
			var key string
			if e = rows.Scan(&key); e != nil {
				_ = rows.Close()
				return e
			}
			keys = append(keys, key)
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		for _, key := range keys {
			actor, e := legacyRecordActor(ctx, tx, q.kind, key)
			if e != nil {
				return e
			}
			if actor <= 0 || strconv.FormatInt(actor, 10) != expected {
				return fmt.Errorf("%w: original actor missing for unfinished %s %s", ErrApproverUnauthorized, q.kind, key)
			}
		}
	}
	// Open saved Plan waits must retain an authenticated authored source too.
	rows, err := tx.QueryContext(ctx, `SELECT s.id,w.parent,w.state_json FROM plan_workflow_states w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=?`, repoID)
	if err != nil {
		return err
	}
	type pending struct {
		session     int64
		parent, raw string
	}
	var states []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.session, &p.parent, &p.raw); err != nil {
			_ = rows.Close()
			return err
		}
		states = append(states, p)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, p := range states {
		var state planbrief.PlanState
		if err = json.Unmarshal([]byte(p.raw), &state); err != nil {
			return err
		}
		waiting := state.AssembledWait != ""
		for _, story := range state.Stories {
			waiting = waiting || story.Wait != nil
		}
		if waiting {
			actor, e := legacySessionActor(ctx, tx, p.session)
			if e != nil {
				return e
			}
			if actor <= 0 || strconv.FormatInt(actor, 10) != expected {
				return fmt.Errorf("%w: original authored source missing for Plan wait %s", ErrApproverUnauthorized, p.parent)
			}
		}
	}
	return nil
}
