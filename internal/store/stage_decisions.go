package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ErrStageDecisionTarget refuses unavailable, unsupported or superseded work.
var ErrStageDecisionTarget = errors.New("the current stage is unavailable, unsupported, or changed; request a fresh proposal")

// StageDecisionTarget freezes the current workflow and all decision dependencies.
type StageDecisionTarget struct {
	ManagedFeature    bool                   `json:"managed_feature"`
	ManagedGeneration int                    `json:"managed_generation"`
	ManagedState      string                 `json:"managed_state"`
	ReviewBinding     string                 `json:"review_binding,omitempty"`
	StartNode         string                 `json:"start_node"`
	DefinitionID      int64                  `json:"definition_id"`
	RepositoryID      int64                  `json:"repository_id"`
	SessionID         int64                  `json:"session_id"`
	InstanceID        int64                  `json:"instance_id"`
	RepositoryRef     string                 `json:"repository_ref"`
	TaskRef           string                 `json:"task_ref"`
	Workflow          string                 `json:"workflow"`
	Stage             string                 `json:"stage"`
	Operation         string                 `json:"operation"`
	Revision          string                 `json:"revision"`
	Summary           string                 `json:"summary"`
	Dependencies      string                 `json:"dependencies"`
	Effect            string                 `json:"effect"`
	Guard             string                 `json:"guard"`
	WaitKey           string                 `json:"wait_key"`
	PlanParent        string                 `json:"plan_parent,omitempty"`
	StoryKey          string                 `json:"story_key,omitempty"`
	Scope             planbrief.ContentScope `json:"scope"`
	Candidate         string                 `json:"candidate"`
	SourceNode        string                 `json:"source_node"`
	ApprovalNode      string                 `json:"approval_node,omitempty"`
	QuestionID        int64                  `json:"question_id"`
	DiagnosisRevision int64                  `json:"diagnosis_revision,omitempty"`
}

// StageDecisionReceipt records an atomic transition independently of summary delivery.
type StageDecisionReceipt struct {
	ExecutionID      string    `json:"execution_id"`
	State            string    `json:"state"`
	Operation        string    `json:"operation"`
	TaskRef          string    `json:"task_ref"`
	Transition       string    `json:"transition"`
	ContinuationRef  string    `json:"continuation_ref,omitempty"`
	RevisionProgress string    `json:"revision_progress,omitempty"`
	Reason           string    `json:"reason,omitempty"`
	AppliedAt        time.Time `json:"applied_at"`
}

type stageDecisionEffectKey struct{}

// ResolveStageDecisionTarget selects only an eligible, published, current target.
func (db *DB) ResolveStageDecisionTarget(ctx context.Context, task, stage, operation string) (StageDecisionTarget, error) {
	if operation != "revise_task" && operation != "approve_stage" {
		return StageDecisionTarget{}, ErrStageDecisionTarget
	}
	snapshot, err := db.readTaskAdvisorySnapshot(ctx, task, true)
	if err != nil {
		return StageDecisionTarget{}, errors.Join(ErrStageDecisionTarget, err)
	}
	var files map[string][]byte
	if err = json.Unmarshal(snapshot.Files, &files); err != nil {
		return StageDecisionTarget{}, err
	}
	profile, err := graphexec.ParseProfile(files[path.Base(snapshot.Definition.Path)])
	if err != nil {
		return StageDecisionTarget{}, err
	}
	t := StageDecisionTarget{RepositoryID: snapshot.Repository.ID, SessionID: snapshot.Session.ID, InstanceID: snapshot.Instance.ID, RepositoryRef: snapshot.Repository.Forge + ":" + snapshot.Repository.Owner + "/" + snapshot.Repository.Name, TaskRef: task, Workflow: snapshot.Definition.Workflow, Operation: operation, Guard: snapshot.Guard, PlanParent: snapshot.PlanParent, StoryKey: snapshot.StoryKey}
	t.StartNode = profile.Start
	t.DefinitionID = snapshot.Definition.ID
	if t.Workflow == "" {
		t.Workflow = snapshot.Definition.Mode
	}
	if snapshot.Wait != nil {
		t.WaitKey = snapshot.Wait.DeliveryKey
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		guard, e := taskAdvisoryGuardTx(ctx, tx, task)
		if e != nil {
			return e
		}
		if guard != t.Guard {
			return ErrStageDecisionTarget
		}
		var running int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts WHERE instance_id=? AND status IN ('running','pending')`, t.InstanceID).Scan(&running); e != nil {
			return e
		}
		if running != 0 {
			return ErrGraphAttemptRunning
		}
		var unresolved int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM owner_action_proposals p WHERE p.state IN ('executing','uncertain') AND json_valid(p.facts) AND json_extract(p.facts,'$.target_id')=? AND json_extract(p.facts,'$.invocation.command') IN ('revise_task','approve_stage') AND NOT EXISTS(SELECT 1 FROM stage_decision_receipts r WHERE r.execution_id=p.execution_id)`, task).Scan(&unresolved); e != nil {
			return e
		}
		if unresolved > 0 {
			return fmt.Errorf("%w: an earlier decision must be recovered before proposing another", ErrStageDecisionTarget)
		}
		if t.Workflow == "plan" {
			guard, binding, e := stageDecisionGuardTx(ctx, tx, t)
			if e != nil {
				return e
			}
			if binding.Present && binding.State != "planning" {
				return fmt.Errorf("%w: the Plan is locked after promotion to Implement", ErrStageDecisionTarget)
			}
			t.Guard = guard
			t.ManagedFeature = binding.Present
			t.ManagedGeneration = binding.Generation
			t.ManagedState = binding.State
			if e := stageDecisionPlanRevisionPendingTx(ctx, tx, t.PlanParent, t.StoryKey); e != nil {
				return e
			}
			return resolvePlanStageTarget(&t, snapshot, stage, operation)
		}
		if t.Workflow != "research" && t.Workflow != "bug" {
			return ErrStageDecisionTarget
		}
		if snapshot.Wait == nil || snapshot.Wait.State != "waiting" {
			return ErrStageDecisionTarget
		}
		var kind, cause string
		var candidate []byte
		var completed bool
		if e = tx.QueryRowContext(ctx, `SELECT c.kind,COALESCE(c.stop_cause,''),c.candidate,d.comment_id,d.completed FROM graph_comment_intents c JOIN report_deliveries d ON d.delivery_key=c.delivery_key WHERE c.delivery_key=?`, t.WaitKey).Scan(&kind, &cause, &candidate, &t.QuestionID, &completed); e != nil {
			return e
		}
		if !completed || t.QuestionID < 1 {
			return ErrStageDecisionTarget
		}
		t.SourceNode = snapshot.Wait.OriginNode
		if t.SourceNode == "" {
			if e = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, snapshot.Wait.SourceAttemptID).Scan(&t.SourceNode); e != nil {
				return e
			}
		}
		t.Summary = stageDecisionCandidateSummary(candidate)
		t.Revision = t.Guard
		if t.Workflow == "bug" {
			if (cause != "diagnosis_review" && cause != "diagnosis_question") || t.SourceNode == "" {
				return ErrStageDecisionTarget
			}
			var diagnosis GraphDiagnosis
			if e = graphRecordTx(ctx, tx, t.InstanceID, "diagnosis", &diagnosis); e != nil {
				return e
			}
			if operation == "approve_stage" && (!diagnosis.ReviewReady || cause != "diagnosis_review") {
				return ErrStageDecisionTarget
			}
			t.Stage = "analysis"
			t.DiagnosisRevision = diagnosis.Revision
			t.Candidate = diagnosis.Digest
			t.Summary = fmt.Sprintf("Expected: %s\nObserved: %s\nCause: %s\nRemedy: %s", diagnosis.Analysis.Expected, diagnosis.Analysis.Actual, diagnosis.Analysis.Cause, diagnosis.Analysis.Remedy)
			t.Effect = "Admit revision guidance to the current Bug analysis; replacement work needs its own review."
			if operation == "approve_stage" {
				t.Effect = "Approve this exact Bug diagnosis. This can start the existing repair workflow."
			}
		} else {
			source, ok := profile.Node(t.SourceNode)
			if !ok {
				return ErrStageDecisionTarget
			}
			seen := map[string]bool{}
			for source.Action != "lf_agent" && source.Attrs["lf_source"] != "" {
				if seen[source.Name] {
					return ErrStageDecisionTarget
				}
				seen[source.Name] = true
				source, ok = profile.Node(source.Attrs["lf_source"])
				if !ok {
					return ErrStageDecisionTarget
				}
			}
			switch source.Attrs["lf_agent_markdown"] {
			case "research":
				t.Stage = "investigation"
			case "shaping":
				t.Stage = "shaping"
			default:
				return ErrStageDecisionTarget
			}
			if kind != "question" && kind != "review" {
				return ErrStageDecisionTarget
			}
			if operation == "approve_stage" {
				if kind != "review" {
					return fmt.Errorf("%w: this Research question has no approval gate", ErrStageDecisionTarget)
				}
				for _, n := range profile.Nodes {
					if n.Action == graphexec.ResearchIfAction && n.Attrs["lf_source"] == source.Name && n.Attrs["lf_condition"] == "approved" {
						target, exists := graphexec.Decide(profile, n.Name, "true")
						final, found := profile.Node(target)
						if exists && found && final.Action == "lf_comment" && final.Attrs["lf_kind"] == "final" && final.Attrs["lf_source"] == source.Name {
							t.ApprovalNode = target
						}
					}
				}
				if t.ApprovalNode == "" {
					return ErrStageDecisionTarget
				}
				t.Effect = "Approve the displayed Research work through its existing review gate."
			} else {
				t.Effect = "Admit revision guidance to the current Research stage; replacement work is not approved."
			}
		}
		if stage != "" && stage != t.Stage {
			return ErrStageDecisionTarget
		}
		return nil
	})
	if err != nil {
		return StageDecisionTarget{}, err
	}
	if len(t.Summary) > 16000 {
		return StageDecisionTarget{}, ErrConnectorCapacity
	}
	if t.Dependencies == "" {
		t.Dependencies = "The current captured workflow and saved source work."
	}
	return t, nil
}

func stageDecisionCandidateSummary(raw []byte) string {
	var candidate struct {
		Result struct {
			Comment string `json:"comment"`
		} `json:"result"`
		Comment   string          `json:"comment"`
		Diagnosis *GraphDiagnosis `json:"diagnosis"`
	}
	if json.Unmarshal(raw, &candidate) == nil {
		if candidate.Comment != "" {
			return candidate.Comment
		}
		if candidate.Result.Comment != "" {
			return candidate.Result.Comment
		}
		if candidate.Diagnosis != nil {
			b, _ := json.Marshal(candidate.Diagnosis.Analysis)
			return string(b)
		}
	}
	return string(raw)
}

func resolvePlanStageTarget(t *StageDecisionTarget, snapshot TaskAdvisorySnapshot, stage, operation string) error {
	var state planbrief.PlanState
	if json.Unmarshal(snapshot.PlanState, &state) != nil || snapshot.PlanRevision < 1 {
		return ErrStageDecisionTarget
	}
	state.ClearStaleWaits()
	t.Revision = t.Guard
	if t.StoryKey == "" {
		if state.AssembledWait != "" {
			t.Stage = "assembled"
			if operation == "revise_task" {
				return fmt.Errorf("%w: select the existing story to revise in this assembled Plan", ErrStageDecisionTarget)
			}
			if state.AssembledWait != state.PlanSetRevision() {
				return ErrStageDecisionTarget
			}
			t.Candidate = state.AssembledWait
			t.ReviewBinding = t.Candidate
			t.Summary = "The assembled Plan and all current story approvals."
			for _, story := range state.Stories {
				if story.Active() {
					t.Summary += "\n" + story.Draft.Name + ": " + story.Draft.Problem
				}
			}
			t.Dependencies = "All current story candidates, their approvals and shared decisions."
		} else {
			if state.StorySet == nil || state.StorySet.Promoted || len(state.StorySet.Entries) == 0 {
				return ErrStageDecisionTarget
			}
			t.Stage = "set"
			t.Candidate = state.StorySet.Revision
			t.ReviewBinding = t.Candidate
			for _, entry := range state.StorySet.Entries {
				t.Summary += entry.Name + ": " + entry.Intent + "\n"
			}
			t.Dependencies = "The current unpromoted story set and shared decisions."
		}
	} else {
		found := false
		for _, item := range state.Stories {
			if item.Key != t.StoryKey {
				continue
			}
			if found {
				return ErrStageDecisionTarget
			}
			found = true
			if !item.Active() || item.Blocked() || item.Wait == nil || item.Wait.Candidate != item.Candidate || item.Wait.Stage != item.Stage || item.Wait.Kind == "retire" {
				return ErrStageDecisionTarget
			}
			revision, e := state.ReviewRevision(item.Key, item.Stage)
			if e != nil || revision != item.Wait.Revision {
				return ErrStageDecisionTarget
			}
			t.Stage = item.Stage
			if t.Stage != "story" && t.Stage != "technical" && t.Stage != "acceptance" {
				return ErrStageDecisionTarget
			}
			t.Candidate = item.Candidate
			t.ReviewBinding = item.Wait.Revision
			t.WaitKey = item.Wait.ID
			t.Summary = item.Draft.Name + "\n" + item.Draft.Problem
			if item.Stage == "technical" && item.Draft.Architecture != nil {
				for _, decision := range item.Draft.Architecture.General {
					t.Summary += "\n" + decision.Decision
				}
			}
			if item.Stage == "acceptance" {
				for _, criterion := range item.Draft.Criteria {
					t.Summary += "\n" + criterion.Text
				}
			}
			t.Dependencies = "Current shared decisions and earlier stage approvals."
			for _, key := range item.Draft.Dependencies.StoryKeys {
				for _, upstream := range state.Stories {
					if upstream.Key == key {
						t.Dependencies += " " + upstream.Draft.Name + "."
					}
				}
			}
			if operation == "approve_stage" {
				checked := state
				_, e = checked.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: chatTriggerBase + 1, OwnerID: 1, ActorID: 1, IssueID: item.Wait.IssueID, Key: item.Key, Stage: item.Stage, Candidate: item.Candidate, WaitID: item.Wait.ID, Body: "/approve-plan " + item.Key + " " + item.Stage + " " + item.Candidate})
				if e != nil {
					return errors.Join(ErrStageDecisionTarget, e)
				}
			}
		}
		if !found {
			return ErrStageDecisionTarget
		}
	}
	if stage != "" && stage != t.Stage {
		return ErrStageDecisionTarget
	}
	if operation == "revise_task" {
		t.Effect = "Admit revision guidance for this current Plan stage; replacement work is not approved."
	} else {
		t.Effect = "Approve this exact current Plan proposal through the existing owner transition and continue the existing workflow."
	}
	return nil
}

// ApplyStageDecision commits authority, exact target, state, receipt and dispatch
// together. No external effect precedes this transaction.
func (db *DB) ApplyStageDecision(ctx context.Context, target StageDecisionTarget, change, execution string, at time.Time) (StageDecisionReceipt, error) {
	var out StageDecisionReceipt
	if (target.Operation != "revise_task" && target.Operation != "approve_stage") || execution == "" || at.IsZero() || target.Guard == "" || target.TaskRef == "" || len(change) > 65536 || (target.Operation == "revise_task" && strings.TrimSpace(change) == "") {
		return out, ErrConnectorInvalid
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		prior, e := stageDecisionReceiptTx(ctx, tx, execution)
		if e == nil {
			if prior.State != "not_applied" {
				var storedTarget, storedChange string
				if e = tx.QueryRowContext(ctx, `SELECT target,change_text FROM stage_decision_receipts WHERE execution_id=?`, execution).Scan(&storedTarget, &storedChange); e != nil {
					return e
				}
				encoded, _ := json.Marshal(target)
				if storedTarget != string(encoded) || storedChange != change {
					return ErrWorkflowConflict
				}
			}
			out = prior
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		effect, ok := ctx.Value(ownerActionEffectKey{}).(ownerActionEffect)
		if !ok || effect.key != execution || effect.clock == nil {
			return ErrConnectorOwner
		}
		at = effect.clock()
		if e = ownerActionEffectContextTx(ctx, tx); e != nil {
			return e
		}
		var proposal OwnerAction
		proposal, e = scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE execution_id=?`, execution))
		if e != nil {
			return e
		}
		if !at.Before(proposal.ExpiresAt) {
			return ErrConnectorInvalid
		}
		if proposal.RepositoryID != target.RepositoryID || proposal.Scope != "repository:actions" || (target.Operation == "approve_stage" && proposal.Tier != "binding") || (target.Operation == "revise_task" && proposal.Tier != "conversational") {
			return ErrConnectorOwner
		}
		var facts struct {
			TargetID       string `json:"target_id"`
			TargetRevision string `json:"target_revision"`
			Invocation     struct {
				Command string `json:"command"`
			} `json:"invocation"`
		}
		if json.Unmarshal([]byte(proposal.Facts), &facts) != nil || facts.TargetID != target.TaskRef || facts.TargetRevision != target.Revision || facts.Invocation.Command != target.Operation {
			return ErrConnectorOwner
		}
		var reviewed struct {
			Target StageDecisionTarget `json:"target"`
		}
		if json.Unmarshal([]byte(proposal.Payload), &reviewed) != nil {
			return ErrConnectorInvalid
		}
		encodedTarget, _ := json.Marshal(target)
		encodedReviewed, _ := json.Marshal(reviewed.Target)
		if string(encodedTarget) != string(encodedReviewed) {
			return ErrStageDecisionTarget
		}
		var owner int64
		if owner, e = strconv.ParseInt(proposal.Principal.OwnerID, 10, 64); e != nil || owner < 1 {
			return ErrConnectorOwner
		}
		if e = stageDecisionUnresolvedTx(ctx, tx, target.TaskRef, execution); e != nil {
			return e
		}
		guard, _, e := stageDecisionGuardTx(ctx, tx, target)
		if e != nil {
			return e
		}
		out = StageDecisionReceipt{ExecutionID: execution, TaskRef: target.TaskRef, Operation: target.Operation, State: "refused", Reason: "The reviewed stage, candidate or dependencies changed; request a fresh proposal.", AppliedAt: at}
		dispatchID := int64(0)
		if guard == target.Guard {
			ctx = context.WithValue(ctx, stageDecisionEffectKey{}, target)
			if target.Workflow == "plan" {
				e = applyPlanStageDecisionTx(ctx, tx, target, change, owner, at, &dispatchID)
			} else {
				e = applyGraphStageDecisionTx(ctx, tx, target, change, owner, at)
			}
			if e != nil {
				return e
			}
			if dispatchID == 0 {
				if e = tx.QueryRowContext(ctx, `SELECT id FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running') ORDER BY id DESC LIMIT 1`, target.InstanceID).Scan(&dispatchID); e != nil {
					return e
				}
			}
			out.State = "applied"
			out.Reason = ""
			out.Transition = target.Effect
			out.ContinuationRef = strconv.FormatInt(dispatchID, 10)
			if target.Operation == "revise_task" {
				out.RevisionProgress = "admitted; replacement work pending"
			}
		}
		return retainStageDecisionReceiptTx(ctx, tx, target, change, out, dispatchID, at)
	})
	return out, err
}

func stageDecisionReceiptTx(ctx context.Context, tx *sql.Tx, execution string) (StageDecisionReceipt, error) {
	var raw string
	var r StageDecisionReceipt
	e := tx.QueryRowContext(ctx, `SELECT receipt FROM stage_decision_receipts WHERE execution_id=?`, execution).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	e = json.Unmarshal([]byte(raw), &r)
	return r, e
}
func retainStageDecisionReceiptTx(ctx context.Context, tx *sql.Tx, target StageDecisionTarget, change string, r StageDecisionReceipt, dispatch int64, at time.Time) error {
	t, _ := json.Marshal(target)
	raw, _ := json.Marshal(r)
	_, e := tx.ExecContext(ctx, `INSERT INTO stage_decision_receipts(execution_id,task_ref,target,change_text,state,receipt,dispatch_id,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.ExecutionID, target.TaskRef, string(t), change, r.State, string(raw), stageDecisionNullableID(dispatch), FormatTime(at))
	return e
}

// RecoverStageDecision never invokes a transition. The permanent not-applied
// tombstone fences an executor that has not entered its atomic transaction yet;
// an executor already inside must commit or roll back before this transaction.
func (db *DB) RecoverStageDecision(ctx context.Context, execution string, at time.Time) (StageDecisionReceipt, bool, error) {
	var out StageDecisionReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := stageDecisionReceiptTx(ctx, tx, execution)
		if e == nil {
			out = r
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var payload string
		if e = tx.QueryRowContext(ctx, `SELECT payload FROM owner_action_proposals WHERE execution_id=? AND state IN ('executing','uncertain')`, execution).Scan(&payload); e != nil {
			return e
		}
		var frozen struct {
			Target  StageDecisionTarget `json:"target"`
			Request struct {
				Change string `json:"change"`
			} `json:"request"`
		}
		if json.Unmarshal([]byte(payload), &frozen) != nil || frozen.Target.TaskRef == "" {
			return ErrConnectorInvalid
		}
		out = StageDecisionReceipt{ExecutionID: execution, TaskRef: frozen.Target.TaskRef, Operation: frozen.Target.Operation, State: "not_applied", Reason: "The interrupted execution committed no transition. Request a fresh proposal.", AppliedAt: at}
		return retainStageDecisionReceiptTx(ctx, tx, frozen.Target, frozen.Request.Change, out, 0, at)
	})
	return out, err == nil, err
}

// GetStageDecisionReceipt reads the durable effect independently of outer status.
func (db *DB) GetStageDecisionReceipt(ctx context.Context, execution string) (StageDecisionReceipt, error) {
	var out StageDecisionReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = stageDecisionReceiptTx(ctx, tx, execution); return e })
	return out, err
}

func stageDecisionNullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func stageDecisionUnresolvedTx(ctx context.Context, tx *sql.Tx, task, except string) error {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM owner_action_proposals p WHERE p.state IN ('executing','uncertain') AND p.execution_id<>? AND json_valid(p.facts) AND json_extract(p.facts,'$.target_id')=? AND json_extract(p.facts,'$.invocation.command') IN ('revise_task','approve_stage') AND NOT EXISTS(SELECT 1 FROM stage_decision_receipts r WHERE r.execution_id=p.execution_id)`, except, task).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: an earlier decision needs recovery", ErrStageDecisionTarget)
	}
	return nil
}
