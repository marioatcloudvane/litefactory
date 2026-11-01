package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func applyGraphStageDecisionTx(ctx context.Context, tx *sql.Tx, t StageDecisionTarget, change string, owner int64, at time.Time) error {
	var ordinal int64
	if e := tx.QueryRowContext(ctx, `SELECT rowid FROM graph_owner_waits WHERE instance_id=? AND delivery_key=? AND state='waiting'`, t.InstanceID, t.WaitKey).Scan(&ordinal); e != nil {
		return e
	}
	replyID := chatTriggerBase + ordinal
	if !IsChatTrigger(replyID) {
		return ErrStageDecisionTarget
	}
	in := GraphOwnerReply{InstanceID: t.InstanceID, OwnerID: owner, AuthorID: owner, CommentID: replyID, QuestionCommentID: t.QuestionID, TaskRef: t.TaskRef, SourceRef: t.TaskRef, Workflow: t.Workflow, DeliveryKey: t.WaitKey, Body: "Requested revision:\n" + change, At: at}
	if t.Workflow == "research" {
		in.Mode = "research"
	}
	if t.Operation == "approve_stage" {
		if t.Workflow == "bug" {
			in.Body = "/approve-bug analysis " + strconv.FormatInt(t.DiagnosisRevision, 10)
			if _, e := approveGraphDiagnosisTx(ctx, tx, in, t.DiagnosisRevision, true); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `UPDATE graph_owner_waits SET reply_source='chat' WHERE instance_id=? AND delivery_key=?`, t.InstanceID, t.WaitKey); e != nil {
				return e
			}
			return parkChatResearchReplyTx(ctx, tx, t.InstanceID, t.SourceNode, at)
		}
		// A Research approval records the same claimed review consumed by settlement.
		in.Body = "done"
	}
	_, e := claimGraphOwnerReplyTx(ctx, tx, in, true)
	return e
}

func applyPlanStageDecisionTx(ctx context.Context, tx *sql.Tx, t StageDecisionTarget, change string, owner int64, at time.Time, dispatchID *int64) error {
	var state planbrief.PlanState
	var revision int64
	var raw string
	if e := tx.QueryRowContext(ctx, `SELECT revision,state_json FROM plan_workflow_states WHERE parent=?`, t.PlanParent).Scan(&revision, &raw); e != nil {
		return e
	}
	if e := json.Unmarshal([]byte(raw), &state); e != nil {
		return e
	}
	state.ClearStaleWaits()
	// Receipt identities occupy an isolated band and never become forge comments.
	effect, ok := ctx.Value(ownerActionEffectKey{}).(ownerActionEffect)
	if !ok || effect.key == "" {
		return ErrConnectorOwner
	}
	var ordinal int64
	if e := tx.QueryRowContext(ctx, `SELECT rowid FROM owner_action_proposals WHERE execution_id=?`, effect.key).Scan(&ordinal); e != nil {
		return e
	}
	replyID := chatTriggerBase + ordinal
	if !IsChatTrigger(replyID) {
		return ErrStageDecisionTarget
	}
	switch t.Operation {
	case "approve_stage":
		switch t.Stage {
		case "set":
			if state.StorySet == nil || state.StorySet.Promoted || state.StorySet.Revision != t.Candidate {
				return ErrStageDecisionTarget
			}
			if _, e := state.PromoteStorySet(t.Candidate); e != nil {
				return e
			}
			for _, entry := range state.StorySet.Entries {
				key := state.StorySet.Keys[entry.ID]
				if _, e := tx.ExecContext(ctx, `INSERT INTO plan_story_drafts(parent,story_key,ordinal) SELECT ?,?,COALESCE(MAX(ordinal),0)+1 FROM plan_story_drafts WHERE parent=? ON CONFLICT(parent,story_key) DO NOTHING`, t.PlanParent, key, t.PlanParent); e != nil {
					return e
				}
				var stored int64
				if e := tx.QueryRowContext(ctx, `SELECT ordinal FROM plan_story_drafts WHERE parent=? AND story_key=?`, t.PlanParent, key).Scan(&stored); e != nil {
					return e
				}
				expected, e := strconv.ParseInt(key[len("story-"):], 10, 64)
				if e != nil || stored != expected {
					return ErrStageDecisionTarget
				}
			}
			state.ConsumedReplies = append(state.ConsumedReplies, replyID)
		case "assembled":
			if len(t.Scope.Roles) == 0 {
				return fmt.Errorf("%w: retained Plan content scope unavailable", ErrStageDecisionTarget)
			}
			if e := state.ApproveAssembled(owner, owner, replyID, "/approve-plan-set "+t.Candidate, t.Scope); e != nil {
				return e
			}
		default:
			found := false
			for _, item := range state.Stories {
				if item.Key != t.StoryKey {
					continue
				}
				found = true
				if item.Wait == nil {
					return ErrStageDecisionTarget
				}
				ok, e := state.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: replyID, OwnerID: owner, ActorID: owner, IssueID: item.Wait.IssueID, Key: item.Key, Stage: t.Stage, Candidate: t.Candidate, WaitID: t.WaitKey, Body: "/approve-plan " + item.Key + " " + t.Stage + " " + t.Candidate})
				if e != nil {
					return e
				}
				if !ok {
					return ErrStageDecisionTarget
				}
				break
			}
			if !found {
				return ErrStageDecisionTarget
			}
		}
	case "revise_task":
		if e := stageDecisionPlanRevisionPendingTx(ctx, tx, t.PlanParent, t.StoryKey); e != nil {
			return e
		}
		if t.StoryKey != "" {
			found := false
			for _, item := range state.Stories {
				if item.Key != t.StoryKey {
					continue
				}
				found = true
				if item.Wait == nil {
					return ErrStageDecisionTarget
				}
				_, e := state.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: replyID, OwnerID: owner, ActorID: owner, IssueID: item.Wait.IssueID, Key: item.Key, Stage: t.Stage, Candidate: t.Candidate, WaitID: t.WaitKey, Body: "Requested revision:\n" + change})
				if e != nil {
					return e
				}
				break
			}
			if !found {
				return ErrStageDecisionTarget
			}
		} else {
			state.ConsumedReplies = append(state.ConsumedReplies, replyID)
		}
	default:
		return ErrStageDecisionTarget
	}
	if t.ManagedFeature && t.Operation == "revise_task" {
		result, e := tx.ExecContext(ctx, `UPDATE managed_features SET ready=0 WHERE parent_task=? AND generation=? AND state='planning'`, t.PlanParent, t.ManagedGeneration)
		if e != nil {
			return e
		}
		changed, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if changed != 1 {
			return ErrStageDecisionTarget
		}
	}
	// Revision admission consumes only the exact current wait. Candidates and
	// approvals change only if the ordinary Plan worker produces a replacement.
	{
		bytes, e := json.Marshal(state)
		if e != nil {
			return e
		}
		if e = savePlanWorkflowStateTx(ctx, tx, t.PlanParent, revision, state, bytes); e != nil {
			return e
		}
	}
	instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, t.InstanceID))
	if e != nil {
		return e
	}
	if instance.Status == GraphSucceeded {
		id, e := schedulePlanDecisionSuccessorTx(ctx, tx, t, change, at)
		if e != nil {
			return e
		}
		*dispatchID = id
		return nil
	}
	if instance.Status != GraphRunning {
		return ErrStageDecisionTarget
	}
	node := instance.CurrentNode
	// Plan reviews may retain a graph wait in addition to the semantic Plan wait.
	// Approval/revision both answer that exact wait; no replacement is selected.
	wait, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND state='waiting' ORDER BY rowid DESC LIMIT 1`, t.InstanceID))
	if e == nil {
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='claimed',reply_id=?,reply_body=?,reply_author_id=?,reply_source='chat',claimed_at=? WHERE delivery_key=? AND state='waiting'`, replyID, change, owner, FormatTime(at), wait.DeliveryKey); e != nil {
			return e
		}
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	return parkChatResearchReplyTx(ctx, tx, t.InstanceID, node, at)
}

// StageDecisionContinuation is retained dispatch input, never caller-supplied
// authority. Its lookup requires the receipt and exact graph dispatch together.
type StageDecisionContinuation struct {
	ExecutionID string
	TaskRef     string
	Operation   string
	Change      string
	OwnerID     int64
	StoryKey    string
	Stage       string
}

// StageDecisionForDispatch reads input only from an applied receipt and its dispatch.
func (db *DB) StageDecisionForDispatch(ctx context.Context, dispatchID int64) (StageDecisionContinuation, bool, error) {
	var c StageDecisionContinuation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var target, owner string
		var allocated int64
		e := tx.QueryRowContext(ctx, `SELECT r.execution_id,r.task_ref,r.target,r.change_text,p.owner_id,COALESCE(d.allocated_run_id,0) FROM stage_decision_receipts r JOIN owner_action_proposals p ON p.execution_id=r.execution_id JOIN graph_dispatches d ON d.id=r.dispatch_id WHERE r.dispatch_id=? AND r.state='applied' AND d.status IN ('reserved','running','pending')`, dispatchID).Scan(&c.ExecutionID, &c.TaskRef, &target, &c.Change, &owner, &allocated)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		var t StageDecisionTarget
		if e = json.Unmarshal([]byte(target), &t); e != nil {
			return e
		}
		c.Operation = t.Operation
		c.StoryKey = t.StoryKey
		c.Stage = t.Stage
		c.OwnerID, e = strconv.ParseInt(owner, 10, 64)
		if e != nil {
			return e
		}
		if t.Workflow == "plan" && t.Operation == "revise_task" {
			_, binding, e := stageDecisionGuardTx(ctx, tx, t)
			if e != nil {
				return e
			}
			if binding.Present != t.ManagedFeature || binding.Generation != t.ManagedGeneration || binding.State != t.ManagedState || (binding.Present && binding.State != "planning") {
				return ErrStageDecisionTarget
			}
			if allocated == 0 {
				return stageDecisionPlanContinuationTx(ctx, tx, t)
			}
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return c, false, nil
	}
	return c, err == nil, err
}

// schedulePlanDecisionSuccessorTx uses the normal finished-invocation successor
// model: the historical graph is immutable, and only its captured Plan starts.
func schedulePlanDecisionSuccessorTx(ctx context.Context, tx *sql.Tx, t StageDecisionTarget, change string, at time.Time) (int64, error) {
	if t.StartNode == "" || t.DefinitionID < 1 {
		return 0, ErrStageDecisionTarget
	}
	var pending int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_comment_intents c JOIN graph_instances i ON i.id=c.instance_id LEFT JOIN report_deliveries d ON d.delivery_key=c.delivery_key WHERE i.session_id=? AND (d.delivery_key IS NULL OR d.completed=0 OR d.comment_id IS NULL)`, t.SessionID).Scan(&pending); e != nil {
		return 0, e
	}
	if pending > 0 {
		return 0, ErrWorkflowEffectsPending
	}
	var instance, dispatch, launch int64
	if e := tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at,next_due_at) VALUES(?,?,'running',?,?,?) RETURNING id`, t.SessionID, t.DefinitionID, t.StartNode, FormatTime(at), FormatTime(at)).Scan(&instance); e != nil {
		return 0, e
	}
	if e := tx.QueryRowContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at) VALUES(?,?,?,?,'pending',?) RETURNING id`, instance, t.SessionID, t.StartNode, FormatTime(at), FormatTime(at)).Scan(&dispatch); e != nil {
		return 0, e
	}
	trigger := graphTriggerBase + dispatch
	if e := tx.QueryRowContext(ctx, `INSERT INTO research_launch_definitions(session_id,trigger_id,instance_id,content_digest,topology_digest,files,provenance,files_digest) SELECT session_id,?,?,content_digest,topology_digest,files,provenance,files_digest FROM research_launch_definitions WHERE instance_id=? ORDER BY id DESC LIMIT 1 RETURNING id`, trigger, instance, t.InstanceID).Scan(&launch); e != nil {
		return 0, e
	}
	if _, e := tx.ExecContext(ctx, `UPDATE graph_dispatches SET launch_id=? WHERE id=?`, launch, dispatch); e != nil {
		return 0, e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO research_successors(instance_id,predecessor_id,trigger_id,reply_body) VALUES(?,?,?,?)`, instance, t.InstanceID, trigger, change); e != nil {
		return 0, e
	}
	return dispatch, nil
}
