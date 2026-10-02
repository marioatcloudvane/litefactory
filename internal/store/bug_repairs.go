package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/bugrepair"
)

// ErrBugRepairExhausted indicates that all three lifetime reservations are spent.
var ErrBugRepairExhausted = errors.New("store: three lifetime Bug repair reservations exhausted")

// ErrBugRepairUnauthorized indicates missing or inapplicable analysis authorization.
var ErrBugRepairUnauthorized = errors.New("store: Bug repair lacks current exact approval")

// ErrBugWorkActive prevents concurrent substantive work in the same task.
var ErrBugWorkActive = errors.New("store: Bug task work remains active")

// BugRepairRequest is immutable factory input, including the observed source.
// Cause evidence is a retained test failure or authenticated review reference.
// GrantReference is empty for the first three lifetime attempts; above that,
// it must name the unused exhaustion:<n> grant this reservation spends.
type BugRepairRequest struct {
	SessionID                      int64
	Request                        string
	AnalysisRevision               int64
	AnalysisDigest, ApprovalReview string
	Cause, Evidence                string
	SourceCommit, SourceTree       string
	GraphAttemptID                 int64
	GrantReference                 string
}

// BugRepair is a lifetime reservation, never a passing-test or delivery claim.
type BugRepair struct {
	BugRepairRequest
	InputDigest   string
	Ordinal       int
	RunID         int64
	State, Reason string
}

func bugRepairTx(ctx context.Context, tx *sql.Tx, session int64, request string) (BugRepair, error) {
	var r BugRepair
	var runID sql.NullInt64
	var grantReference sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,run_id,state,reason,grant_reference FROM bug_repairs WHERE session_id=? AND request=?`, session, request).Scan(&r.SessionID, &r.Request, &r.InputDigest, &r.Ordinal, &r.AnalysisRevision, &r.AnalysisDigest, &r.ApprovalReview, &r.Cause, &r.Evidence, &r.SourceCommit, &r.SourceTree, &r.GraphAttemptID, &runID, &r.State, &r.Reason, &grantReference)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.RunID = runID.Int64
	r.GrantReference = grantReference.String
	return r, nil
}

func bugRepairApprovalTx(ctx context.Context, tx *sql.Tx, in BugRepairRequest) error {
	var approved int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_tasks t JOIN bug_analyses a ON a.session_id=t.session_id AND a.revision=t.current_analysis WHERE t.session_id=? AND a.revision=? AND a.digest=? AND a.review_ready=1 AND a.review=? AND a.approval_owner_id=t.owner_id AND a.review_active=1 AND a.approval_reply_id IS NOT NULL`, in.SessionID, in.AnalysisRevision, in.AnalysisDigest, in.ApprovalReview).Scan(&approved)
	if err != nil {
		return err
	}
	if approved != 1 {
		return ErrBugRepairUnauthorized
	}
	return nil
}

func validateBugRepairRequest(in BugRepairRequest) error {
	if in.SessionID <= 0 || in.Request == "" || len(in.Request) > 256 || in.AnalysisRevision <= 0 || !validSubmissionDigest(in.AnalysisDigest) || in.ApprovalReview == "" || in.GraphAttemptID <= 0 || strings.TrimSpace(in.Evidence) == "" || len(in.Evidence) > 8192 {
		return errors.New("store: malformed Bug repair request")
	}
	if len(in.GrantReference) > 64 {
		return errors.New("store: malformed Bug grant reference")
	}
	for _, sha := range []string{in.SourceCommit, in.SourceTree} {
		if len(sha) != 40 && len(sha) != 64 {
			return errors.New("store: invalid Bug source identity")
		}
		for _, c := range sha {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return errors.New("store: invalid Bug source identity")
			}
		}
	}
	switch in.Cause {
	case "initial", "test_failure", "pr_feedback", "reshape":
	default:
		return errors.New("store: unknown Bug repair cause")
	}
	return nil
}

// ReserveBugRepair atomically validates approval and spends the next lifetime
// slot. Exact replay retrieves its original state before checking exhaustion.
// The lifetime cap of three is enforced here by counting this bug's repair
// rows, never by a database CHECK on the ordinal (AC-004).
func (d *DB) ReserveBugRepair(ctx context.Context, in BugRepairRequest, expected int64) (BugRepair, error) {
	var out BugRepair
	if err := validateBugRepairRequest(in); err != nil {
		return out, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	digest := submissionDigest(raw)
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		old, err := bugRepairTx(ctx, tx, in.SessionID, in.Request)
		if err == nil {
			if old.InputDigest != digest {
				return errors.New("store: Bug repair replay conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		state, err := bugExecutionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		if err = bugRepairApprovalTx(ctx, tx, in); err != nil {
			return err
		}
		var count, active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(state IN ('reserved','dispatched')),0) FROM bug_repairs WHERE session_id=?`, in.SessionID).Scan(&count, &active); err != nil {
			return err
		}
		grants, err := bugGrantDecisionsTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if count >= 3+len(grants) {
			return ErrBugRepairExhausted
		}
		if count >= 3 {
			var matched bool
			for _, g := range grants {
				if g.Reference == in.GrantReference {
					matched = true
					break
				}
			}
			if in.GrantReference == "" || !matched {
				return errors.New("store: Bug repair above the base budget requires an unused grant")
			}
			var used int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_repairs WHERE session_id=? AND grant_reference=?`, in.SessionID, in.GrantReference).Scan(&used); err != nil {
				return err
			}
			if used != 0 {
				return errors.New("store: Bug grant reference already spent")
			}
		} else if in.GrantReference != "" {
			return errors.New("store: Bug grant reference is not allowed within the base budget")
		}
		if active != 0 {
			return ErrBugWorkActive
		}
		if count > 0 {
			var previousState, previousRequest, previousDigest string
			var previousRevision int64
			if err = tx.QueryRowContext(ctx, `SELECT state,analysis_revision,request,analysis_digest FROM bug_repairs WHERE session_id=? ORDER BY ordinal DESC LIMIT 1`, in.SessionID).Scan(&previousState, &previousRevision, &previousRequest, &previousDigest); err != nil {
				return err
			}
			if in.Cause == "reshape" && (previousState != "reshape" || in.AnalysisRevision <= previousRevision || in.AnalysisDigest == previousDigest || in.Evidence != previousRequest) {
				return ErrBugRepairUnauthorized
			}
			if previousState == "reshape" && in.AnalysisRevision <= previousRevision {
				return ErrBugRepairUnauthorized
			}
		}
		if (count == 0) != (in.Cause == "initial") {
			return errors.New("store: initial Bug fix must be exactly the first reservation")
		}
		var guarded int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_tasks WHERE session_id=? AND guard_owner IS NOT NULL`, in.SessionID).Scan(&guarded); err != nil {
			return err
		}
		if guarded != 0 {
			return ErrBugWorkActive
		}
		var session int64
		var workflow, status string
		if err = tx.QueryRowContext(ctx, `SELECT i.session_id,d.workflow,a.status FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.id=?`, in.GraphAttemptID).Scan(&session, &workflow, &status); err != nil {
			return err
		}
		if session != in.SessionID || workflow != "bug" || status != "running" {
			return errors.New("store: repair reservation graph binding mismatch")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_repairs(session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,state,grant_reference) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'reserved',?)`, in.SessionID, in.Request, digest, count+1, in.AnalysisRevision, in.AnalysisDigest, in.ApprovalReview, in.Cause, in.Evidence, in.SourceCommit, in.SourceTree, in.GraphAttemptID, nullString(in.GrantReference)); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, in.SessionID, "repair_reserved", in.Request, 0, "", []byte(bugJSONString(in.Cause))); err != nil {
			return err
		}
		out = BugRepair{BugRepairRequest: in, InputDigest: digest, Ordinal: count + 1, State: "reserved"}
		return nil
	})
	return out, err
}

// GetBugRepair resolves an immutable logical reservation without authorizing work.
func (d *DB) GetBugRepair(ctx context.Context, session int64, request string) (BugRepair, error) {
	var out BugRepair
	err := d.withTx(ctx, func(tx *sql.Tx) error { var err error; out, err = bugRepairTx(ctx, tx, session, request); return err })
	return out, err
}

// LatestBugRepair returns a retained identity for runtime resolution. Callers
// still use the domain's current approval and completion guards before effects.
func (d *DB) LatestBugRepair(ctx context.Context, session int64) (BugRepair, error) {
	var out BugRepair
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var request string
		if err := tx.QueryRowContext(ctx, `SELECT request FROM bug_repairs WHERE session_id=? ORDER BY ordinal DESC LIMIT 1`, session).Scan(&request); err != nil {
			return err
		}
		var err error
		out, err = bugRepairTx(ctx, tx, session, request)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

// BugRepairReshapes lists every repair the fixer handed back for reshaping, in
// order. Lifetime reservations bound the list to three.
func (d *DB) BugRepairReshapes(ctx context.Context, session int64) ([]BugRepair, error) {
	var out []BugRepair
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT request FROM bug_repairs WHERE session_id=? AND state='reshape' ORDER BY ordinal`, session)
		if err != nil {
			return err
		}
		var requests []string
		for rows.Next() {
			var request string
			if err = rows.Scan(&request); err != nil {
				_ = rows.Close()
				return err
			}
			requests = append(requests, request)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, request := range requests {
			r, err := bugRepairTx(ctx, tx, session, request)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

// ClaimBugRepairDispatch rechecks exact approval and source and atomically
// excludes checks/save work. A started reservation can never launch again.
func (d *DB) ClaimBugRepairDispatch(ctx context.Context, session int64, request, commit, tree string, attempt, runID int64) (bool, error) {
	var claimed bool
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, session, request)
		if err != nil {
			return err
		}
		if r.State != "reserved" {
			return nil
		}
		if r.SourceCommit != commit || r.SourceTree != tree {
			return errors.New("store: Bug repair source drift")
		}
		if err = bugRepairApprovalTx(ctx, tx, r.BugRepairRequest); err != nil {
			return err
		}
		run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, runID))
		if err != nil {
			return err
		}
		if run.SessionID != session || run.Workflow != "bug" || run.Mode != "" || (run.Status != RunRunning && run.Status != RunPrelaunch) {
			return errors.New("store: Bug fixer run mismatch")
		}
		var boundSession, boundRun int64
		var status, workflow string
		if err = tx.QueryRowContext(ctx, `SELECT i.session_id,COALESCE(a.run_id,0),a.status,d.workflow FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.id=?`, attempt).Scan(&boundSession, &boundRun, &status, &workflow); err != nil {
			return err
		}
		if boundSession != session || boundRun != runID || status != "running" || workflow != "bug" {
			return errors.New("store: Bug fixer graph mismatch")
		}
		ok, err := claimBugWorkTx(ctx, tx, session, "repair:"+request, "repair")
		if err != nil {
			return err
		}
		if !ok {
			return ErrBugWorkActive
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_repairs SET state='dispatched',graph_attempt_id=?,run_id=? WHERE session_id=? AND request=?`, attempt, runID, session, request); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, session); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "repair_dispatched", request, 0, "", nil); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

// StopBugRepair records a non-successful attempt without discarding any edits.
// Uncertain cleanup deliberately retains the task guard, excluding replacement work.
func (d *DB) StopBugRepair(ctx context.Context, session int64, request, state, reason string, quiescent bool) error {
	switch state {
	case "reshape", "blocked", "failed", "uncertain":
	default:
		return errors.New("store: invalid Bug repair stop")
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 65536 {
		return errors.New("store: Bug repair stop needs bounded reason")
	}
	if state == "uncertain" && quiescent {
		return errors.New("store: uncertain repair cannot assert quiescence")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, session, request)
		if err != nil {
			return err
		}
		if r.State != "reserved" && r.State != "dispatched" {
			if r.State == state && r.Reason == reason {
				return nil
			}
			return fmt.Errorf("store: terminal Bug repair %s cannot change", r.State)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_repairs SET state=?,reason=? WHERE session_id=? AND request=?`, state, reason, session, request); err != nil {
			return err
		}
		if quiescent {
			if err = releaseBugWorkOwnerTx(ctx, tx, session, "repair:"+request); err != nil {
				return err
			}
		}
		if err = bugBumpRevisionTx(ctx, tx, session); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "repair_"+state, request, 0, "", []byte(bugJSONString(reason)))
	})
}

// BugRepairCompletion is a factory-confirmed immutable source handoff. Scope
// observations come from the factory completion boundary, never the result payload.
type BugRepairCompletion struct {
	SessionID                               int64
	Request                                 string
	ResultID, ResultRevision                int64
	SourceCommit, SourceTree, SaveOperation string
}

// CompleteBugRepair binds retained done evidence, successful process/graph
// evidence and a confirmed factory save. The caller observes semantic scope and
// actual paths before invoking this method; agent prose is not that observation.
func (d *DB) CompleteBugRepair(ctx context.Context, in BugRepairCompletion, expected int64) error {
	if in.SessionID <= 0 || in.Request == "" || in.ResultID <= 0 || in.ResultRevision <= 0 || in.SourceCommit == "" || in.SourceTree == "" || in.SaveOperation == "" {
		return errors.New("store: incomplete Bug repair handoff")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, in.SessionID, in.Request)
		if err != nil {
			return err
		}
		if r.State == "completed" {
			prior, err := bugRepairCompletionTx(ctx, tx, in.SessionID, in.Request)
			if err != nil {
				return err
			}
			if prior != in {
				return errors.New("store: repair completion replay conflict")
			}
			return nil
		}
		state, err := bugExecutionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		if r.State != "dispatched" {
			return errors.New("store: repair completion requires dispatched invocation")
		}
		if err = bugRepairApprovalTx(ctx, tx, r.BugRepairRequest); err != nil {
			return err
		}
		run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, r.RunID))
		if err != nil {
			return err
		}
		if run.CommitSHA != in.SourceCommit || !run.Published {
			return errors.New("store: repair source save is not confirmed")
		}
		if !run.Succeeded() {
			return errors.New("store: repair process has not succeeded")
		}
		var raw []byte
		var runID int64
		var accepted string
		err = tx.QueryRowContext(ctx, `SELECT v.snapshot,v.run_id,q.input_state FROM application_result_revisions v JOIN application_result_requests q ON q.id=v.request_row JOIN application_results a ON a.id=v.result_id JOIN sessions s ON s.task_ref=a.task WHERE v.result_id=? AND v.revision=? AND s.id=?`, in.ResultID, in.ResultRevision, in.SessionID).Scan(&raw, &runID, &accepted)
		if err != nil {
			return err
		}
		if runID != r.RunID || accepted != "accepted" {
			return errors.New("store: repair retained result binding mismatch")
		}
		var retained struct {
			Result struct {
				Outcome string `json:"outcome"`
				Payload struct {
					Bug struct {
						Response, Invocation string
						Repair               *bugrepair.Evidence `json:"repair"`
					} `json:"bug"`
				} `json:"payload"`
			} `json:"result"`
		}
		if err = json.Unmarshal(raw, &retained); err != nil {
			return err
		}
		p := retained.Result.Payload.Bug
		if retained.Result.Outcome != "completed" || p.Response != "done" || p.Invocation != r.Request || p.Repair == nil {
			return errors.New("store: repair has no retained done evidence")
		}
		e := p.Repair
		if e.Reservation != r.Request || e.AnalysisDigest != r.AnalysisDigest || e.SourceCommit != r.SourceCommit || e.SourceTree != r.SourceTree {
			return errors.New("store: repair result source mismatch")
		}
		analysis, err := bugRevisionTx(ctx, tx, in.SessionID, r.AnalysisRevision)
		if err != nil {
			return err
		}
		var criteria []string
		for _, c := range analysis.Analysis.Criteria {
			criteria = append(criteria, c.ID)
		}
		notices, err := e.Validate(criteria, true)
		if err != nil {
			return err
		}
		for _, n := range notices {
			if err = recordBugFactTx(ctx, tx, in.SessionID, r.GraphAttemptID, BugFactStageRepair, n.Reference, n.Text); err != nil {
				return err
			}
		}
		var status, outcome string
		if err = tx.QueryRowContext(ctx, `SELECT status,COALESCE(outcome,'') FROM graph_node_attempts WHERE id=? AND run_id=?`, r.GraphAttemptID, r.RunID).Scan(&status, &outcome); err != nil {
			return err
		}
		if status != "succeeded" || outcome != "success" {
			return errors.New("store: repair graph did not confirm success")
		}
		var output []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload FROM graph_outputs WHERE node_attempt_id=? AND kind='json' ORDER BY id DESC LIMIT 1`, r.GraphAttemptID).Scan(&output); err != nil {
			return err
		}
		var binding struct {
			ResultID       int64 `json:"result_id"`
			ResultRevision int64 `json:"result_revision"`
		}
		if err = json.Unmarshal(output, &binding); err != nil {
			return err
		}
		if binding.ResultID != in.ResultID || binding.ResultRevision != in.ResultRevision {
			return errors.New("store: repair graph result mismatch")
		}
		var scopeCommit, scopeTree string
		// The factory must have observed the actual changes; whether they stay
		// within the analysis's expected files is evidence for the delivery
		// review, not a gate.
		err = tx.QueryRowContext(ctx, `SELECT scope_source_commit,scope_source_tree FROM bug_repairs WHERE session_id=? AND request=?`, in.SessionID, in.Request).Scan(&scopeCommit, &scopeTree)
		if err != nil {
			return err
		}
		if scopeCommit != in.SourceCommit || scopeTree != in.SourceTree {
			return errors.New("store: repair scope is not established for immutable source")
		}
		evidence, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_repairs SET state='completed',result_id=?,result_revision=?,completion_evidence=?,completed_commit=?,completed_tree=?,save_operation=? WHERE session_id=? AND request=?`, in.ResultID, in.ResultRevision, evidence, in.SourceCommit, in.SourceTree, in.SaveOperation, in.SessionID, in.Request); err != nil {
			return err
		}
		if err = releaseBugWorkOwnerTx(ctx, tx, in.SessionID, "repair:"+in.Request); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, in.SessionID, "repair_completed", in.Request, 0, "", []byte(bugJSONString(in.SaveOperation)))
	})
}

func bugRepairCompletionTx(ctx context.Context, tx *sql.Tx, session int64, request string) (BugRepairCompletion, error) {
	var out BugRepairCompletion
	var resultID, resultRevision sql.NullInt64
	var commit, tree, op sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT result_id,result_revision,completed_commit,completed_tree,save_operation FROM bug_repairs WHERE session_id=? AND request=? AND state='completed'`, session, request).Scan(&resultID, &resultRevision, &commit, &tree, &op)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out = BugRepairCompletion{SessionID: session, Request: request, ResultID: resultID.Int64, ResultRevision: resultRevision.Int64, SourceCommit: commit.String, SourceTree: tree.String, SaveOperation: op.String}
	return out, nil
}

// CompletedBugRepair revalidates current authorization and the observed immutable
// source. Later source drift withholds readiness without rewriting history.
func (d *DB) CompletedBugRepair(ctx context.Context, session int64, request, commit, tree string) (BugRepairCompletion, error) {
	var out BugRepairCompletion
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, session, request)
		if err != nil {
			return err
		}
		if r.State != "completed" {
			return errors.New("store: Bug repair is not completed")
		}
		if err = bugRepairApprovalTx(ctx, tx, r.BugRepairRequest); err != nil {
			return err
		}
		out, err = bugRepairCompletionTx(ctx, tx, session, request)
		if err != nil {
			return err
		}
		if out.SourceCommit != commit || out.SourceTree != tree {
			return errors.New("store: completed Bug repair source changed")
		}
		return nil
	})
	return out, err
}

// ApplyBugRepairStop applies a retained blocked/reshape result after successful
// invocation and graph settlement. It preserves edits, spends no extra slot and
// grants no further edits; reshape additionally requires new analysis approval.
func (d *DB) ApplyBugRepairStop(ctx context.Context, in BugRepairCompletion, expected int64) error {
	if in.SessionID <= 0 || in.Request == "" || in.ResultID <= 0 || in.ResultRevision <= 0 {
		return errors.New("store: malformed repair stop application")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, in.SessionID, in.Request)
		if err != nil {
			return err
		}
		var raw []byte
		var runID int64
		var accepted string
		err = tx.QueryRowContext(ctx, `SELECT v.snapshot,v.run_id,q.input_state FROM application_result_revisions v JOIN application_result_requests q ON q.id=v.request_row JOIN application_results a ON a.id=v.result_id JOIN sessions s ON s.task_ref=a.task WHERE v.result_id=? AND v.revision=? AND s.id=?`, in.ResultID, in.ResultRevision, in.SessionID).Scan(&raw, &runID, &accepted)
		if err != nil {
			return err
		}
		if accepted != "accepted" || runID != r.RunID {
			return errors.New("store: repair stop result binding mismatch")
		}
		var retained struct {
			Result struct {
				Outcome, Reason string
				Payload         struct {
					Bug struct {
						Response, Invocation string
						Repair               *bugrepair.Evidence `json:"repair"`
					} `json:"bug"`
				} `json:"payload"`
			} `json:"result"`
		}
		if err = json.Unmarshal(raw, &retained); err != nil {
			return err
		}
		p := retained.Result.Payload.Bug
		if (p.Response != "reshape" && p.Response != "blocked") || p.Invocation != r.Request || p.Repair == nil || strings.TrimSpace(retained.Result.Reason) == "" {
			return errors.New("store: repair stop needs retained reason and evidence")
		}
		if (p.Response == "reshape" && retained.Result.Outcome != "incomplete") || (p.Response == "blocked" && retained.Result.Outcome != "blocked") {
			return errors.New("store: repair stop response/outcome mismatch")
		}
		e := p.Repair
		if e.Reservation != r.Request || e.AnalysisDigest != r.AnalysisDigest || e.SourceCommit != r.SourceCommit || e.SourceTree != r.SourceTree {
			return errors.New("store: repair stop evidence binding mismatch")
		}
		analysis, err := bugRevisionTx(ctx, tx, in.SessionID, r.AnalysisRevision)
		if err != nil {
			return err
		}
		var criteria []string
		for _, c := range analysis.Analysis.Criteria {
			criteria = append(criteria, c.ID)
		}
		notices, err := e.Validate(criteria, false)
		if err != nil {
			return err
		}
		for _, n := range notices {
			if err = recordBugFactTx(ctx, tx, in.SessionID, r.GraphAttemptID, BugFactStageRepair, n.Reference, n.Text); err != nil {
				return err
			}
		}
		var status, outcome string
		var output []byte
		if err = tx.QueryRowContext(ctx, `SELECT a.status,COALESCE(a.outcome,''),o.payload FROM graph_node_attempts a JOIN graph_outputs o ON o.node_attempt_id=a.id AND o.kind='json' WHERE a.id=? AND a.run_id=? ORDER BY o.id DESC LIMIT 1`, r.GraphAttemptID, r.RunID).Scan(&status, &outcome, &output); err != nil {
			return err
		}
		var binding struct {
			ResultID       int64 `json:"result_id"`
			ResultRevision int64 `json:"result_revision"`
		}
		if err = json.Unmarshal(output, &binding); err != nil {
			return err
		}
		if status != "succeeded" || outcome != "success" || binding.ResultID != in.ResultID || binding.ResultRevision != in.ResultRevision {
			return errors.New("store: repair stop lacks graph consistency")
		}
		run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, r.RunID))
		if err != nil {
			return err
		}
		if !run.Succeeded() {
			return errors.New("store: repair stop process is not confirmed successful")
		}
		if r.State == p.Response && r.Reason == retained.Result.Reason {
			return nil
		}
		if r.State != "dispatched" {
			return errors.New("store: terminal repair cannot change")
		}
		state, err := bugExecutionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_repairs SET state=?,reason=? WHERE session_id=? AND request=?`, p.Response, retained.Result.Reason, in.SessionID, in.Request); err != nil {
			return err
		}
		if err = releaseBugWorkOwnerTx(ctx, tx, in.SessionID, "repair:"+in.Request); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, in.SessionID, "repair_"+p.Response, in.Request, 0, "", []byte(bugJSONString(retained.Result.Reason)))
	})
}

// BugRepairScopeObservation is factory evidence over actual immutable changes,
// never an agent result. It records uncertainty as well as positive findings.
type BugRepairScopeObservation struct {
	SessionID                                                            int64
	Request, SourceCommit, SourceTree, DiffDigest, Disposition, Evidence string
	Paths                                                                []string
}

// RetainBugRepairScope stores the immutable diff and approved-boundary assessment for a repair.
// Only the latest observation for the repair's immutable source is kept as
// current-state columns; an observation for a different commit than the one
// currently on the row is refused as a scope replay conflict, matching the
// original schema's per-(request,source_commit) immutability.
func (d *DB) RetainBugRepairScope(ctx context.Context, in BugRepairScopeObservation) error {
	if in.SessionID <= 0 || in.Request == "" || !validSubmissionDigest(in.DiffDigest) || in.SourceCommit == "" || in.SourceTree == "" || strings.TrimSpace(in.Evidence) == "" || len(in.Evidence) > 65536 || len(in.Paths) > 256 {
		return errors.New("store: malformed Bug repair scope observation")
	}
	switch in.Disposition {
	case "within", "outside", "indeterminate":
	default:
		return errors.New("store: unknown repair scope disposition")
	}
	raw, err := json.Marshal(in.Paths)
	if err != nil {
		return err
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, in.SessionID, in.Request)
		if err != nil {
			return err
		}
		if err = bugRepairApprovalTx(ctx, tx, r.BugRepairRequest); err != nil {
			return err
		}
		var tree, digest, disposition, evidence sql.NullString
		var paths []byte
		var sourceCommit sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT scope_source_commit,scope_source_tree,scope_diff_digest,scope_disposition,scope_evidence,scope_paths FROM bug_repairs WHERE session_id=? AND request=?`, in.SessionID, in.Request).Scan(&sourceCommit, &tree, &digest, &disposition, &evidence, &paths)
		if err != nil {
			return err
		}
		if sourceCommit.Valid {
			switch {
			case sourceCommit.String != in.SourceCommit:
				// A distinct historical observation for an earlier immutable source is
				// preserved (kept in bug_events) rather than lost, matching that this
				// table used to key on (request,source_commit).
				if err = bugEventJSONTx(ctx, tx, in.SessionID, "repair_scope", fmt.Sprintf("%s:%s", in.Request, sourceCommit.String), in); err != nil {
					return err
				}
			case tree.String != in.SourceTree || digest.String != in.DiffDigest || disposition.String != in.Disposition || evidence.String != in.Evidence || string(paths) != string(raw):
				return errors.New("store: immutable repair scope observation conflict")
			default:
				return nil
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_repairs SET scope_source_commit=?,scope_source_tree=?,scope_diff_digest=?,scope_disposition=?,scope_evidence=?,scope_paths=? WHERE session_id=? AND request=?`, in.SourceCommit, in.SourceTree, in.DiffDigest, in.Disposition, in.Evidence, raw, in.SessionID, in.Request); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		// The analysis's file list is guidance, not a gate (#384, #409;
		// REFUSAL_REVIEW.md's file-list-gate pain point): an "outside" or
		// "indeterminate" disposition never blocks the repair, but it is
		// recorded as a fact so the owner sees it at delivery review.
		if in.Disposition != "within" {
			text := fmt.Sprintf("repair changes were observed %s the analysis's expected files", in.Disposition)
			if err = recordBugFactTx(ctx, tx, in.SessionID, r.GraphAttemptID, BugFactStageRepair, "store.repair_scope:"+in.Request, text); err != nil {
				return err
			}
		}
		return bugEventTx(ctx, tx, in.SessionID, "repair_scope_current", in.Request, 0, "", []byte(bugJSONString(in.Disposition)))
	})
}

// BugTestRequest is factory-bound; no graph path or command is supplied by agents.
// Resolution names a retained resolved infrastructure cause for a new generation.
type BugTestRequest struct {
	SessionID        int64           `json:"session_id"`
	Operation        string          `json:"operation"`
	ParentInstance   int64           `json:"parent_instance"`
	ParentAttempt    int64           `json:"parent_attempt"`
	ChildDefinition  int64           `json:"child_definition"`
	DefinitionDigest string          `json:"definition_digest"`
	Binding          BugCheckBinding `json:"binding"`
	PreviousCall     int64           `json:"previous_call,omitempty"`
	Resolution       string          `json:"resolution,omitempty"`
}

// BugTestCall binds one durable child execution to its parent and tested
// inputs. It is the current test generation folded onto its owning repair's
// row; ID and ChildInstance are the same durable graph instance identity.
type BugTestCall struct {
	ID, ChildInstance            int64
	Request                      BugTestRequest
	Generation                   int
	Outcome, Reason              string
	Cancelled, Consumed, Current bool
}

// scanBugTestRow reads the current test-generation columns from one bug_repairs
// row. ErrNotFound means that repair has never had a test call.
func scanBugTestRow(row scanner) (BugTestCall, error) {
	var c BugTestCall
	var raw []byte
	var childInstance sql.NullInt64
	err := row.Scan(&childInstance, &raw, &c.Generation, &c.Outcome, &c.Reason, &c.Cancelled, &c.Consumed, &c.Current)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if !childInstance.Valid {
		return c, ErrNotFound
	}
	c.ID, c.ChildInstance = childInstance.Int64, childInstance.Int64
	err = json.Unmarshal(raw, &c.Request)
	return c, err
}

const bugTestColumns = `test_child_instance,test_binding,test_generation,test_outcome,test_reason,test_cancelled,test_consumed,test_current`

// GetBugTestCall loads a child call by durable identity (its child instance id).
func (d *DB) GetBugTestCall(ctx context.Context, id int64) (BugTestCall, error) {
	return scanBugTestRow(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=?`, id))
}

// BugTestParentCall finds the child call owned by a parent execution envelope.
func (d *DB) BugTestParentCall(ctx context.Context, instance int64) (BugTestCall, error) {
	return scanBugTestRow(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_parent_instance=? AND test_consumed=0 LIMIT 1`, instance))
}

// BugTestChildCall finds the call owning a child execution envelope.
func (d *DB) BugTestChildCall(ctx context.Context, instance int64) (BugTestCall, error) {
	return d.GetBugTestCall(ctx, instance)
}

// LatestBugTestCall loads the most recently allocated child call for the task.
func (d *DB) LatestBugTestCall(ctx context.Context, session int64) (BugTestCall, error) {
	return scanBugTestRow(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE session_id=? AND test_child_instance IS NOT NULL ORDER BY test_child_instance DESC LIMIT 1`, session))
}

// PhaseBinding derives the exact durable binding for one phase of this child call.
func (c BugTestCall) PhaseBinding(phase string) BugCheckBinding {
	b := c.Request.Binding
	b.Phase = phase
	b.Operation = fmt.Sprintf("bug-test:%d:%s", c.ID, phase)
	b.Generation = fmt.Sprintf("bug-test:%d", c.ID)
	return b
}

func bugTestRepairTx(ctx context.Context, tx *sql.Tx, r BugTestRequest) error {
	repair, err := bugRepairTx(ctx, tx, r.SessionID, r.Binding.Repair)
	if err != nil {
		return err
	}
	if repair.State != "completed" {
		return errors.New("bug test requires completed repair")
	}
	if err = bugRepairApprovalTx(ctx, tx, repair.BugRepairRequest); err != nil {
		return err
	}
	completion, err := bugRepairCompletionTx(ctx, tx, r.SessionID, r.Binding.Repair)
	if err != nil {
		return err
	}
	if completion.SourceCommit != r.Binding.SourceCommit || completion.SourceTree != r.Binding.SourceTree {
		return errors.New("bug test source differs from completed repair")
	}
	return nil
}

// CreateBugTestCall atomically allocates the child, its dispatch and parent wait.
// The parent attempt stays running until consumption; it is never orphan work.
func (d *DB) CreateBugTestCall(ctx context.Context, r BugTestRequest, at time.Time) (BugTestCall, error) {
	if r.SessionID <= 0 || r.Operation == "" || len(r.Operation) > 128 || r.ParentInstance <= 0 || r.ParentAttempt <= 0 || r.ChildDefinition <= 0 || !validSubmissionDigest(r.DefinitionDigest) || at.IsZero() || r.Binding.SessionID != r.SessionID {
		return BugTestCall{}, errors.New("invalid Bug test call")
	}
	b := r.Binding
	b.Phase = "regression"
	b.Operation = r.Operation
	b.Generation = r.Operation
	if err := validateBugCheckBinding(b); err != nil {
		return BugTestCall{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return BugTestCall{}, err
	}
	digest := submissionDigest(raw)
	var id int64
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRowContext(ctx, `SELECT test_input_digest FROM bug_repairs WHERE session_id=? AND test_operation=?`, r.SessionID, r.Operation).Scan(&old)
		if e == nil {
			if old != digest {
				return errors.New("bug test call replay conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e = bugTestRepairTx(ctx, tx, r); e != nil {
			return e
		}
		var guarded int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_tasks WHERE session_id=? AND guard_owner IS NOT NULL`, r.SessionID).Scan(&guarded); e != nil {
			return e
		}
		if guarded != 0 {
			return errors.New("bug test worktree is occupied")
		}
		var parentSession, definition int64
		var status, node, workflow string
		if e = tx.QueryRowContext(ctx, `SELECT i.session_id,i.definition_id,a.status,a.node,COALESCE(g.workflow,'') FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions g ON g.id=i.definition_id WHERE a.id=? AND i.id=? AND i.status='running' AND i.current_node=a.node`, r.ParentAttempt, r.ParentInstance).Scan(&parentSession, &definition, &status, &node, &workflow); e != nil {
			return e
		}
		if parentSession != r.SessionID || status != GraphRunning || workflow != "bug" {
			return errors.New("bug test parent binding mismatch")
		}
		var nested int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_repairs WHERE test_child_instance=?`, r.ParentInstance).Scan(&nested); e != nil {
			return e
		}
		if nested != 0 {
			return errors.New("nested Bug children forbidden")
		}
		// The package itself is pinned per task by its launch record; shared
		// definition rows cannot prove it, so only the child's own identity is
		// checked here.
		var childDigest, childWorkflow, path string
		if e = tx.QueryRowContext(ctx, `SELECT digest,workflow,path FROM graph_definitions WHERE id=?`, r.ChildDefinition).Scan(&childDigest, &childWorkflow, &path); e != nil {
			return e
		}
		if childDigest != r.DefinitionDigest || childWorkflow != "bug" || path != "bug_tests.dot" {
			return errors.New("bug child definition is not pinned to parent package")
		}
		var existingGeneration sql.NullInt64
		var existingChild sql.NullInt64
		var existingOutcome, existingReason sql.NullString
		var existingConsumed, existingCurrent sql.NullBool
		if e = tx.QueryRowContext(ctx, `SELECT test_generation,test_child_instance,test_outcome,test_reason,test_consumed,test_current FROM bug_repairs WHERE session_id=? AND request=?`, r.SessionID, r.Binding.Repair).Scan(&existingGeneration, &existingChild, &existingOutcome, &existingReason, &existingConsumed, &existingCurrent); e != nil {
			return e
		}
		generation := existingGeneration.Int64 + 1
		if generation > 1 {
			if existingChild.Int64 != r.PreviousCall || (existingOutcome.String != "blocked" && (existingOutcome.String != "passed" || existingCurrent.Bool)) || !existingConsumed.Bool || r.Resolution == "" || len(r.Resolution) > 8192 {
				return errors.New("new Bug generation needs resolved quiescent infrastructure block")
			}
			var unreliable int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_checks WHERE session_id=? AND source='factory' AND unreliable=1 AND json_extract(binding,'$.source_commit')=? AND json_extract(binding,'$.target_commit')=? AND json_extract(binding,'$.recipe_digest')=?`, r.SessionID, r.Binding.SourceCommit, r.Binding.TargetCommit, r.Binding.RecipeDigest).Scan(&unreliable); e != nil {
				return e
			}
			if unreliable > 0 {
				return errors.New("known flakiness cannot start a fresh generation")
			}
			// The generation about to be replaced is retired into the shared log so
			// its history remains resolvable (for example by BugTestResolution).
			retired, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE session_id=? AND request=?`, r.SessionID, r.Binding.Repair))
			if e != nil {
				return e
			}
			if e = bugEventJSONTx(ctx, tx, r.SessionID, "test_generation", fmt.Sprintf("%s:%d", r.Binding.Repair, retired.Generation), retired); e != nil {
				return e
			}
		} else if r.PreviousCall != 0 || r.Resolution != "" {
			return errors.New("initial generation cannot claim recovery")
		}
		child, e := scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running','regression',?) RETURNING `+graphInstanceColumns, r.SessionID, r.ChildDefinition, FormatTime(at)))
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_repairs SET test_operation=?,test_input_digest=?,test_binding=?,test_generation=?,test_parent_instance=?,test_parent_attempt=?,test_child_instance=?,test_definition_digest=?,test_outcome='pending',test_reason='',test_cancelled=0,test_consumed=0,test_current=1,test_created_at=? WHERE session_id=? AND request=?`, r.Operation, digest, raw, generation, r.ParentInstance, r.ParentAttempt, child.ID, r.DefinitionDigest, FormatTime(at), r.SessionID, r.Binding.Repair); e != nil {
			return e
		}
		id = child.ID
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, r.ParentInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(at), r.ParentInstance); e != nil {
			return e
		}
		if e = bugTestDispatchTx(ctx, tx, child.ID, r.SessionID, "regression", at); e != nil {
			return e
		}
		return bugBumpRevisionTx(ctx, tx, r.SessionID)
	})
	if err != nil {
		return BugTestCall{}, err
	}
	return d.GetBugTestCall(ctx, id)
}

func bugTestDispatchTx(ctx context.Context, tx *sql.Tx, instance, session int64, node string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at) SELECT ?,?,?,?,'pending',? WHERE NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, instance, session, node, FormatTime(at), FormatTime(at), instance)
	return err
}

// CancelBugTestCall serializes cancellation against phase command launches.
// It never asserts that a process has stopped.
func (d *DB) CancelBugTestCall(ctx context.Context, id int64) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=?`, id))
		if e != nil {
			return e
		}
		if c.Consumed {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_repairs SET test_cancelled=1 WHERE test_child_instance=?`, id); e != nil {
			return e
		}
		for _, phase := range []string{"regression", "focused", "full"} {
			if _, e = tx.ExecContext(ctx, `UPDATE bug_checks SET cancelled=1 WHERE session_id=? AND operation=?`, c.Request.SessionID, c.PhaseBinding(phase).Operation); e != nil {
				return e
			}
		}
		return bugEventTx(ctx, tx, c.Request.SessionID, "test_event", fmt.Sprintf("%d:cancel_requested", id), 0, "", []byte(`"durable cancellation; cleanup unconfirmed"`))
	})
}

func bugTestEvidenceTx(ctx context.Context, tx *sql.Tx, c BugTestCall) (string, string, error) {
	var unresolved int
	// This test-call outcome gate must never see a fixer self-check row: it
	// filters on source='factory' explicitly rather than relying only on the
	// generation identity never colliding (AC-006).
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_checks WHERE session_id=? AND source='factory' AND json_extract(binding,'$.generation')=? AND cleanup=0`, c.Request.SessionID, c.PhaseBinding("regression").Generation).Scan(&unresolved); err != nil {
		return "", "", err
	}
	if unresolved != 0 {
		return "pending", "check process cleanup is unconfirmed", nil
	}
	for _, phase := range []string{"regression", "focused", "full"} {
		expected := c.PhaseBinding(phase)
		record, err := getBugCheckPhaseTx(ctx, tx, c.Request.SessionID, expected.Operation)
		if errors.Is(err, ErrNotFound) {
			if c.Cancelled {
				return "cancelled", "test child cancelled before next phase", nil
			}
			return "pending", "check phase has no retained outcome", nil
		}
		if err != nil {
			return "", "", err
		}
		if record.Binding != expected {
			return "", "", errors.New("bug child phase binding mismatch")
		}
		if record.Source != "factory" {
			return "", "", errors.New("bug child phase evidence is not factory-sourced")
		}
		if !record.Cleanup {
			return "pending", "check process cleanup is unconfirmed", nil
		}
		if c.Cancelled {
			return "cancelled", "test child cancelled", nil
		}
		if record.Outcome == "pending" {
			return "pending", "check execution unresolved", nil
		}
		if !record.Current || record.Unreliable {
			return "blocked", "check evidence is stale or unreliable", nil
		}
		switch record.Outcome {
		case "failed", "blocked", "cancelled":
			return record.Outcome, record.Reason, nil
		case "passed":
		default:
			return "", "", errors.New("invalid child phase outcome")
		}
		// Recheck the evidence rather than trusting the mutable phase summary alone.
		complete := 0
		for _, cmd := range record.Commands {
			if cmd.Passed() {
				complete++
			}
		}
		if complete != record.Count || record.Count < 1 {
			return "", "", errors.New("incomplete passing child evidence")
		}
	}
	return "passed", "", nil
}

// SettleBugTestCall derives the domain outcome and atomically wakes its parent.
// An unknown process keeps ownership and the suspended call unchanged.
func (d *DB) SettleBugTestCall(ctx context.Context, id int64, at time.Time) (BugTestCall, error) {
	if at.IsZero() {
		return BugTestCall{}, errors.New("bug child settlement needs timestamp")
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=?`, id))
		if e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		outcome, reason, e := bugTestEvidenceTx(ctx, tx, c)
		if e != nil {
			return e
		}
		if outcome == "pending" {
			return nil
		}
		if outcome == "passed" {
			if e = bugTestRepairTx(ctx, tx, c.Request); e != nil {
				outcome, reason = "blocked", e.Error()
			}
		}
		var guarded int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_tasks WHERE session_id=? AND guard_owner IS NOT NULL`, c.Request.SessionID).Scan(&guarded); e != nil {
			return e
		}
		if guarded != 0 {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_repairs SET test_outcome=?,test_reason=? WHERE test_child_instance=?`, outcome, reason, id); e != nil {
			return e
		}
		if e = bugEventTx(ctx, tx, c.Request.SessionID, "test_event", fmt.Sprintf("%d:settled", id), 0, "", []byte(bugJSONString(outcome+":"+reason))); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		// Graph success is execution bookkeeping only; the evidence above owns tests.
		// A retained completed phase may have outlived its graph bookkeeping.
		// Finish that original attempt from its evidence instead of cancelling
		// successful work or launching another command.
		for _, phase := range []string{"regression", "focused", "full"} {
			var phaseOutcome string
			lookup := tx.QueryRowContext(ctx, `SELECT outcome FROM bug_checks WHERE session_id=? AND operation=? AND cleanup=1 AND outcome!='pending'`, c.Request.SessionID, c.PhaseBinding(phase).Operation).Scan(&phaseOutcome)
			if errors.Is(lookup, sql.ErrNoRows) {
				continue
			}
			if lookup != nil {
				return lookup
			}
			var attemptID int64
			lookup = tx.QueryRowContext(ctx, `SELECT id FROM graph_node_attempts WHERE instance_id=? AND node=? AND status='running'`, c.ChildInstance, phase).Scan(&attemptID)
			if errors.Is(lookup, sql.ErrNoRows) {
				continue
			}
			if lookup != nil {
				return lookup
			}
			next := phaseOutcome
			if phaseOutcome == "passed" {
				next = "focused"
				if phase == "focused" {
					next = "full"
				}
				if phase == "full" {
					next = "passed"
				}
			}
			if _, e = finishGraphAttemptTx(ctx, tx, attemptID, GraphSucceeded, phaseOutcome, at); e != nil {
				return e
			}
			if _, e = recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: c.ChildInstance, FromNode: phase, ToNode: next, OnOutcome: phaseOutcome, At: at}); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='cancelled',finished_at=? WHERE instance_id=? AND status='running'`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET current_node=?,status='succeeded',finished_at=?,next_due_at=NULL WHERE id=? AND status='running'`, outcome, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		var node string
		if e = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, c.Request.ParentAttempt).Scan(&node); e != nil {
			return e
		}
		if e = bugTestDispatchTx(ctx, tx, c.Request.ParentInstance, c.Request.SessionID, node, at); e != nil {
			return e
		}
		return bugBumpRevisionTx(ctx, tx, c.Request.SessionID)
	})
	if err != nil {
		return BugTestCall{}, err
	}
	return d.GetBugTestCall(ctx, id)
}

// InvalidateBugTestCall retains the original binding and passing history.
// Drift is observed after the parent consumed the passing call (main moved
// before review or merge), so a consumed call must stop being current too;
// otherwise the retest that drift requires can never start.
func (d *DB) InvalidateBugTestCall(ctx context.Context, id int64, reason string) error {
	if reason == "" {
		return errors.New("bug child invalidation requires cause")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var session int64
		if e := tx.QueryRowContext(ctx, `SELECT session_id FROM bug_repairs WHERE test_child_instance=?`, id).Scan(&session); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return ErrNotFound
			}
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE bug_repairs SET test_current=0,test_reason=? WHERE test_child_instance=?`, reason, id); e != nil {
			return e
		}
		return bugEventTx(ctx, tx, session, "test_event", fmt.Sprintf("%d:invalidated", id), 0, "", []byte(bugJSONString(reason)))
	})
}

// bugTestConsumeTx joins parent transition and call consumption atomically.
// It is called from the generic graph completion path (graph_attempts.go).
func bugTestConsumeTx(ctx context.Context, tx *sql.Tx, attempt int64, in GraphCompletion) error {
	c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_parent_attempt=?`, attempt))
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if in.Status != GraphSucceeded {
		return nil
	}
	if c.Consumed || c.Outcome == "pending" {
		return errors.New("bug child cannot be consumed")
	}
	outcome := c.Outcome
	if c.Cancelled {
		outcome = "cancelled"
	} else if !c.Current {
		outcome = "blocked"
	}
	if outcome == "passed" {
		verified, _, e := bugTestEvidenceTx(ctx, tx, c)
		if e != nil {
			return e
		}
		if verified != "passed" {
			return errors.New("bug child passing evidence changed before consumption")
		}
		if e = bugTestRepairTx(ctx, tx, c.Request); e != nil {
			return e
		}
	}
	if in.Outcome != outcome || in.Transition == nil {
		return errors.New("bug parent transition differs from child outcome")
	}
	_, e = tx.ExecContext(ctx, `UPDATE bug_repairs SET test_consumed=1 WHERE test_child_instance=?`, c.ID)
	return e
}

// ListUnresolvedBugTests is bounded by active task count, not dispatch polling.
func (d *DB) ListUnresolvedBugTests(ctx context.Context) ([]BugTestCall, error) {
	rows, e := d.sql.QueryContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_outcome='pending' AND session_id NOT IN (SELECT session_id FROM doctor_held_session_owners) ORDER BY test_child_instance`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []BugTestCall
	for rows.Next() {
		c, e := scanBugTestRow(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// BugTestDispatchCall resolves the recorded child instance, never the latest
// graph in a session (which may instead be its suspended parent).
func (d *DB) BugTestDispatchCall(ctx context.Context, dispatch int64) (BugTestCall, error) {
	var instance int64
	if e := d.sql.QueryRowContext(ctx, `SELECT instance_id FROM graph_dispatches WHERE id=?`, dispatch).Scan(&instance); e != nil {
		return BugTestCall{}, fmt.Errorf("bug child dispatch: %w", e)
	}
	return d.BugTestChildCall(ctx, instance)
}

// FailBugTestCall retains activity errors without confusing them with ordinary
// test failure. Possible running checks leave the call and ownership unresolved.
func (d *DB) FailBugTestCall(ctx context.Context, id int64, reason string, at time.Time) error {
	if reason == "" || at.IsZero() {
		return errors.New("bug child failure requires cause and time")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=?`, id))
		if e != nil {
			return e
		}
		if e = bugEventTx(ctx, tx, c.Request.SessionID, "test_event", fmt.Sprintf("%d:execution_error", id), 0, "", []byte(bugJSONString(reason))); e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		var active int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_tasks WHERE session_id=? AND guard_owner IS NOT NULL`, c.Request.SessionID).Scan(&active); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_repairs SET test_reason=? WHERE test_child_instance=?`, reason, id); e != nil {
			return e
		}
		if active > 0 {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) SELECT id,'diagnostic',?,? FROM graph_node_attempts WHERE instance_id=? AND status='running'`, []byte(reason), FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='failed',finished_at=? WHERE instance_id=? AND status='running'`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_repairs SET test_outcome='blocked' WHERE test_child_instance=?`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',finished_at=?,next_due_at=NULL WHERE id=? AND status='running'`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		var node string
		if e = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, c.Request.ParentAttempt).Scan(&node); e != nil {
			return e
		}
		return bugTestDispatchTx(ctx, tx, c.Request.ParentInstance, c.Request.SessionID, node, at)
	})
}

// RecoverBugTestDispatch requeues bookkeeping after restart only. Command replay
// decisions remain with the phase reconciler; no process is launched here.
func (d *DB) RecoverBugTestDispatch(ctx context.Context, id int64, at time.Time) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=?`, id))
		if e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		var node, status string
		if e = tx.QueryRowContext(ctx, `SELECT current_node,status FROM graph_instances WHERE id=?`, c.ChildInstance).Scan(&node, &status); e != nil {
			return e
		}
		if status != GraphRunning {
			return errors.New("bug child terminal graph requires error settlement")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('reserved','running')`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		return bugTestDispatchTx(ctx, tx, c.ChildInstance, c.Request.SessionID, node, at)
	})
}

// ClaimBugTestDispatch consumes the daemon reservation once without allocating
// a fictitious agent run. Child identity is its persisted graph plus dispatch.
func (d *DB) ClaimBugTestDispatch(ctx context.Context, in GraphDispatch) (bool, error) {
	var claimed bool
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=? AND session_id=?`, in.InstanceID, in.SessionID))
		if e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		result, e := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='running' WHERE id=? AND instance_id=? AND session_id=? AND node=? AND status='reserved' AND allocated_run_id IS NULL`, in.ID, in.InstanceID, in.SessionID, in.Node)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		claimed = n == 1
		return e
	})
	return claimed, err
}

// PassedBugTestCall is the read-only downstream handoff. It revalidates the
// exact consumed child and all phase records; a graph success flag is insufficient.
func (d *DB) PassedBugTestCall(ctx context.Context, id int64) (BugTestCall, error) {
	var c BugTestCall
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		c, e = scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=?`, id))
		if e != nil {
			return e
		}
		if c.Outcome != "passed" || !c.Consumed || c.Cancelled || !c.Current {
			return errors.New("bug child has no current consumed passing evidence")
		}
		outcome, _, e := bugTestEvidenceTx(ctx, tx, c)
		if e != nil {
			return e
		}
		if outcome != "passed" {
			return errors.New("bug child phase evidence no longer passes")
		}
		return bugTestRepairTx(ctx, tx, c.Request)
	})
	return c, err
}

// bugTestResolutionContent is the retained owner reply resolving an
// infrastructure block, folded into bug_events (kind "test_resolution")
// instead of its own dedicated table.
type bugTestResolutionContent struct {
	CommentID, AuthorID int64
	Body, Resolution    string
}

func bugTestResolutionGuardTx(ctx context.Context, tx *sql.Tx, session, call int64) error {
	c, e := scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=? AND session_id=?`, call, session))
	if e != nil {
		return e
	}
	if c.Request.SessionID != session || c.Outcome != "blocked" || !c.Consumed || c.Cancelled {
		return errors.New("bug resolved testing requires a consumed infrastructure block")
	}
	outcome, _, e := bugTestEvidenceTx(ctx, tx, c)
	if e != nil {
		return e
	}
	if outcome != "blocked" {
		return errors.New("bug child state is not a confirmed block")
	}
	var unsafe int
	for _, phase := range []string{"regression", "focused", "full"} {
		b := c.PhaseBinding(phase)
		var n int
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_checks WHERE session_id=? AND operation=? AND source='factory' AND (cleanup=0 OR unreliable=1)`, session, b.Operation).Scan(&n)
		if e != nil {
			return e
		}
		unsafe += n
	}
	if unsafe != 0 {
		return errors.New("bug child cleanup or known flakiness prevents fresh testing")
	}
	return nil
}

// RecordBugTestResolution records the owner's resolved infrastructure cause. It
// cannot waive known flakiness, uncertain cleanup, checks or either approval.
func (d *DB) RecordBugTestResolution(ctx context.Context, session, call int64, in GraphOwnerReply) error {
	prefix := fmt.Sprintf("/retry-bug-tests %d ", call)
	if in.Mode != "" || in.OwnerID <= 0 || in.AuthorID != in.OwnerID || in.CommentID <= 0 || in.SourceRef != in.TaskRef || in.At.IsZero() || !strings.HasPrefix(in.Body, prefix) || strings.TrimSpace(strings.TrimPrefix(in.Body, prefix)) == "" || len(in.Body) > 65536 {
		return errors.New("invalid Bug test resolution")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		identity, e := bugIdentityTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if identity.OwnerID != in.OwnerID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
			return errors.New("bug test resolution task/owner mismatch")
		}
		if e = bugTestResolutionGuardTx(ctx, tx, session, call); e != nil {
			return e
		}
		return bugEventJSONTx(ctx, tx, session, "test_resolution", fmt.Sprint(call), bugTestResolutionContent{CommentID: in.CommentID, AuthorID: in.AuthorID, Body: in.Body, Resolution: strings.TrimSpace(strings.TrimPrefix(in.Body, prefix))})
	})
}

// BugTestResolution reads the retained source for a fresh child generation.
func (d *DB) BugTestResolution(ctx context.Context, session, call int64) (string, error) {
	var content []byte
	err := d.sql.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='test_resolution' AND reference=?`, session, fmt.Sprint(call)).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	var r bugTestResolutionContent
	if err = json.Unmarshal(content, &r); err != nil {
		return "", err
	}
	return r.Resolution, nil
}
