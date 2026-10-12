package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ApproverBlocker identifies the existing owner of unfinished work.
type ApproverBlocker struct{ Kind, Key string }

// ApproverBlockers inspects project work without cancelling or rewriting it.
func (db *DB) ApproverBlockers(ctx context.Context, repoID int64) ([]ApproverBlocker, error) {
	return approverBlockers(ctx, db.sql, repoID)
}
func approverBlockers(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, repoID int64) ([]ApproverBlocker, error) {
	// Each query names the existing durable lifecycle owner, including work that
	// has not allocated a run yet and possibly dispatched remote publications.
	queries := []struct{ kind, sql string }{

		{"conflict_confirmation", `SELECT CAST(w.id AS TEXT) FROM conflict_proposals w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.status='pending'`},
		{"followup_decision", `SELECT w.proposal_key FROM followup_proposals w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.reported=0`},
		{"owner_exception_report", `SELECT CAST(w.id AS TEXT) FROM owner_exceptions w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.report_comment_id=0`},
		{"acceptance_projection", `SELECT CAST(w.id AS TEXT) FROM acceptance_decisions w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND (w.projected=0 OR w.delivered=0)`},
		{"story_progress_effect", `SELECT CAST(w.id AS TEXT) FROM story_progress_effects w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.delivered=0 AND w.superseded=0`},
		{"story_scope_notice", `SELECT w.parent_task||':'||w.story_key||':'||w.kind FROM story_scope_notices w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.delivered=0`},
		{"plan_notice", `SELECT CAST(w.id AS TEXT) FROM plan_story_notices w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=? AND w.delivered=0`},
		{"plan_explanation", `SELECT w.delivery_key FROM plan_reply_explanations w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=? AND NOT EXISTS(SELECT 1 FROM report_deliveries d WHERE d.delivery_key=w.delivery_key AND d.completed=1)`},
		{"graph_comment", `SELECT w.delivery_key FROM graph_comment_intents w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND g.status<>'cancelled' AND NOT EXISTS(SELECT 1 FROM report_deliveries d WHERE d.delivery_key=w.delivery_key AND d.completed=1)`},
		{"graph_dispatch", `SELECT CAST(w.id AS TEXT) FROM graph_dispatches w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.status IN ('pending','reserved','running')`},
		{"story_refinement", `SELECT CAST(w.comment_id AS TEXT) FROM story_refinements w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.consumed=0`},
		{"story_acknowledgement", `SELECT CAST(w.comment_id AS TEXT) FROM story_refinement_acks w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.acknowledgement_id=0`},
		{"creation_presentation", `SELECT CAST(w.run_id AS TEXT) FROM mcp_creation_presentations w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.presented=0`},
		{"creation_artifact", `SELECT CAST(w.operation_id AS TEXT)||':'||CAST(w.ordinal AS TEXT) FROM creation_publication_artifacts w JOIN mcp_creation_operations o ON o.id=w.operation_id JOIN mcp_creation_proposals p ON p.hash=o.proposal_hash JOIN sessions s ON s.task_ref=p.source_task WHERE s.repo_id=? AND o.state NOT IN ('released','failed') AND w.relationship IN ('pending','uncertain')`},
		{"creation_owner_artifact", `SELECT CAST(w.intent_id AS TEXT)||':'||CAST(w.ordinal AS TEXT) FROM creation_owner_artifacts w JOIN mcp_creation_intents i ON i.id=w.intent_id JOIN creation_owner_publications p ON p.intent_id=i.id JOIN sessions s ON s.task_ref=i.source_task WHERE s.repo_id=? AND p.state<>'refused' AND w.relationship IN ('pending','uncertain')`},
		{"doctor_command", `SELECT CAST(id AS TEXT) FROM doctor_inbox WHERE repo_id=? AND state IN ('queued','claimed')`},
		{"doctor_response", `SELECT CAST(w.id AS TEXT) FROM doctor_inbox w WHERE w.repo_id=? AND w.state='completed' AND w.result_body<>'' AND NOT EXISTS(SELECT 1 FROM doctor_presentations p WHERE p.command_id=w.id)`},
		{"doctor_publication", `SELECT d.delivery_key FROM report_deliveries d JOIN doctor_inbox w ON w.id=d.doctor_command_id WHERE w.repo_id=? AND d.completed=0`},
		{"doctor_attempt", `SELECT a.id FROM doctor_attempts a JOIN doctor_conversations c ON c.id=a.conversation_id WHERE a.state='running' AND (EXISTS(SELECT 1 FROM doctor_inbox i WHERE i.task_ref=c.task_ref AND i.repo_id=?) OR EXISTS(SELECT 1 FROM sessions s WHERE s.task_ref=c.task_ref AND s.repo_id=?))`},
		{"doctor_owner_decision", `SELECT CAST(c.id AS TEXT) FROM doctor_conversations c WHERE c.state='active' AND c.current_proposal_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM doctor_repairs r WHERE r.proposal_id=c.current_proposal_id AND r.state IN ('recovered','stopped','completed','failed')) AND (EXISTS(SELECT 1 FROM doctor_inbox i WHERE i.task_ref=c.task_ref AND i.repo_id=?) OR EXISTS(SELECT 1 FROM sessions s WHERE s.task_ref=c.task_ref AND s.repo_id=?))`},
		{"doctor_repair", `SELECT w.id FROM doctor_repairs w JOIN sessions s ON s.task_ref=w.task_ref WHERE s.repo_id=? AND w.state NOT IN ('recovered','stopped','completed','failed')`},
		{"chat_notice", `SELECT w.transport||':'||w.message_id FROM chat_notices w JOIN sessions s ON s.task_ref=w.task_ref WHERE s.repo_id=? AND w.state IN ('pending','sent')`},
		{"continuation_cancellation_notice", `SELECT CAST(w.id AS TEXT) FROM automatic_continuations w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.status='cancelled' AND w.cancellation_comment_id IS NULL`},
		{"creation_rejection", `SELECT CAST(w.comment_id AS TEXT) FROM creation_rejections w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND NOT EXISTS(SELECT 1 FROM report_deliveries d WHERE d.delivery_key=json_extract(CAST(w.delivery AS TEXT),'$.Key') AND d.completed=1)`},
		{"authority_claim", `SELECT CAST(id AS TEXT) FROM project_authority_claims WHERE repo_id=? AND completed=0`},
		{"session", `SELECT task_ref FROM sessions WHERE repo_id=? AND (state='running' OR COALESCE(lock_owner,'')<>'' OR needs_poll=1)`},
		{"run", `SELECT CAST(r.id AS TEXT) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.repo_id=? AND r.status IN ('prelaunch','running','succeeded','failed')`},
		{"schedule", `SELECT CAST(w.id AS TEXT) FROM schedules w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.status NOT IN ('succeeded','failed','cancelled')`},
		{"schedule_decision", `SELECT CAST(w.id AS TEXT) FROM schedule_proposals w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.status='pending'`},
		{"continuation", `SELECT CAST(w.id AS TEXT) FROM automatic_continuations w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.status NOT IN ('succeeded','failed','cancelled','interrupted')`},
		{"graph", `SELECT CAST(g.id AS TEXT) FROM graph_instances g JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND g.status='running'`},
		{"graph_effect", `SELECT e.operation FROM graph_effects e JOIN graph_instances g ON g.id=e.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND e.state IN ('intent','dispatched','uncertain')`},
		{"bug_analysis", `SELECT w.invocation FROM bug_analyses w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND (w.state IN ('reserved','dispatched','uncertain') OR (w.review_active=1 AND w.approval_reply_id IS NULL))`},
		{"bug_repair", `SELECT w.request FROM bug_repairs w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.state IN ('reserved','dispatched','uncertain')`},
		{"bug_delivery", `SELECT CAST(w.session_id AS TEXT) FROM bug_delivery w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND ((w.review_active=1 AND w.approval_comment_id IS NULL) OR (w.pr_dispatched=1 AND w.pr IS NULL AND w.pr_cancelled=0) OR (w.merge_dispatched=1 AND w.merge_outcome IN ('pending','uncertain')) OR (w.close_dispatched=1 AND w.close_confirmed=0))`},
		{"validate_effect", `SELECT CAST(w.id AS TEXT) FROM validate_effects w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND w.state IN ('intent','uncertain')`},
		{"bug_guard", `SELECT CAST(w.session_id AS TEXT) FROM bug_tasks w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.guard_owner IS NOT NULL`},
		{"bug_lifecycle_event", `SELECT w.delivery_key FROM bug_events w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.reported=0 AND w.delivery_key IS NOT NULL`},
		{"owner_wait", `SELECT w.delivery_key FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND w.state IN ('waiting','claimed')`},
		{"report_publication", `SELECT w.delivery_key FROM report_deliveries w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.completed=0`},
		{"creation_operation", `SELECT CAST(w.id AS TEXT) FROM mcp_creation_operations w JOIN mcp_creation_proposals p ON p.hash=w.proposal_hash JOIN sessions s ON s.task_ref=p.source_task WHERE s.repo_id=? AND w.state IN ('reserved','dispatched','uncertain')`},
		{"creation_publication", `SELECT CAST(w.intent_id AS TEXT) FROM creation_owner_publications w JOIN mcp_creation_intents i ON i.id=w.intent_id JOIN sessions s ON s.task_ref=i.source_task WHERE s.repo_id=? AND w.state IN ('dispatched','uncertain')`},
		{"creation_decision", `SELECT c.source_task FROM mcp_creation_current c JOIN sessions s ON s.task_ref=c.source_task WHERE s.repo_id=? AND NOT EXISTS(SELECT 1 FROM mcp_creation_intents i JOIN creation_owner_publications p ON p.intent_id=i.id WHERE i.proposal_hash=c.proposal_hash AND p.state IN ('confirmed','refused'))`},
		{"delivery_decision", `SELECT d.proposal_key FROM delivery_proposals d JOIN sessions s ON s.task_ref=d.parent_task WHERE s.repo_id=? AND d.superseded=0 AND d.reported=0`},
		{"story_publication", `SELECT w.operation_key FROM story_publications w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.delivered=0`},
		{"story_revision", `SELECT w.operation_key FROM story_revision_operations w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.delivered=0`},
		{"plan_effect", `SELECT CAST(w.id AS TEXT) FROM plan_story_effects w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=? AND w.outcome IN ('pending','uncertain') AND NOT EXISTS(SELECT 1 FROM plan_story_completions c WHERE c.parent=w.parent AND c.request_id=w.request_id)`},
		{"plan_recovery", `SELECT w.parent FROM plan_story_authorities w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=? AND NOT EXISTS(SELECT 1 FROM plan_story_completions c WHERE c.parent=w.parent AND c.request_id=w.request_id)`},
		{"contract_upgrade", `SELECT w.parent_task FROM story_contract_upgrades w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.state='pending'`},
		{"mode_transition", `SELECT CAST(w.trigger_comment_id AS TEXT) FROM mode_selections w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND (w.cleanup_pending=1 OR (w.label<>'' AND w.label_applied=0))`},
		{"retirement", `SELECT CAST(w.id AS TEXT) FROM retirement_intents w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.outcome IN ('unknown','uncertain')`},
	}
	var out []ApproverBlocker
	for _, query := range queries {
		args := []any{repoID}
		if query.kind == "doctor_attempt" || query.kind == "doctor_owner_decision" {
			args = append(args, repoID)
		}
		rows, err := q.QueryContext(ctx, query.sql, args...)
		if err != nil {
			return nil, fmt.Errorf("store: inspect approver blocker %s: %w", query.kind, err)
		}
		for rows.Next() {
			var key string
			if err = rows.Scan(&key); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, ApproverBlocker{Kind: query.kind, Key: key})
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT w.parent,w.state_json FROM plan_workflow_states w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=?`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var parent, raw string
		if err = rows.Scan(&parent, &raw); err != nil {
			return nil, err
		}
		var state planbrief.PlanState
		if err = json.Unmarshal([]byte(raw), &state); err != nil {
			return nil, fmt.Errorf("store: inspect Plan authority state: %w", err)
		}
		if state.AssembledWait != "" {
			out = append(out, ApproverBlocker{Kind: "plan_assembled_wait", Key: parent})
		}
		if state.StorySet != nil && !state.StorySet.Promoted {
			out = append(out, ApproverBlocker{Kind: "plan_story_set_wait", Key: parent})
		}
		for _, story := range state.Stories {
			if story.Wait != nil || story.Hold != "" || story.LifecycleHold != "" || story.EffectHold != "" || story.EffectRequest != "" || len(story.EffectHolds) > 0 {
				out = append(out, ApproverBlocker{Kind: "plan_story_wait", Key: parent + ":" + story.Key})
			}
		}
	}
	return out, rows.Err()
}
