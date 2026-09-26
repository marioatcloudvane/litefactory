package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrBugRepairExhausted indicates that all three lifetime reservations are spent.
var ErrBugRepairExhausted = errors.New("store: three lifetime Bug repair reservations exhausted")

// ErrBugRepairUnauthorized indicates missing or inapplicable analysis authorization.
var ErrBugRepairUnauthorized = errors.New("store: Bug repair lacks current exact approval")

// ErrBugWorkActive prevents concurrent substantive work in the same task.
var ErrBugWorkActive = errors.New("store: Bug task work remains active")

// BugRepairRequest is immutable factory input, including the observed source.
// Cause evidence is a retained test failure or authenticated review reference.
type BugRepairRequest struct {
	SessionID                      int64
	Request                        string
	AnalysisRevision               int64
	AnalysisDigest, ApprovalReview string
	Cause, Evidence                string
	SourceCommit, SourceTree       string
	GraphAttemptID                 int64
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
	err := tx.QueryRowContext(ctx, `SELECT session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,COALESCE(run_id,0),state,reason FROM bug_repairs WHERE session_id=? AND request=?`, session, request).Scan(&r.SessionID, &r.Request, &r.InputDigest, &r.Ordinal, &r.AnalysisRevision, &r.AnalysisDigest, &r.ApprovalReview, &r.Cause, &r.Evidence, &r.SourceCommit, &r.SourceTree, &r.GraphAttemptID, &r.RunID, &r.State, &r.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}
func bugRepairApprovalTx(ctx context.Context, tx *sql.Tx, in BugRepairRequest) error {
	var approved int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_execution e JOIN bug_analysis_revisions a ON a.session_id=e.session_id AND a.revision=e.current_analysis JOIN bug_analysis_approvals p ON p.session_id=a.session_id AND p.analysis_revision=a.revision AND p.analysis_digest=a.digest JOIN bug_analysis_reviews r ON r.session_id=p.session_id AND r.review=p.review JOIN bug_records b ON b.session_id=e.session_id WHERE e.session_id=? AND a.revision=? AND a.digest=? AND a.review_ready=1 AND p.review=? AND p.owner_id=b.owner_id AND r.active=1`, in.SessionID, in.AnalysisRevision, in.AnalysisDigest, in.ApprovalReview).Scan(&approved)
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
		if count >= 3 {
			return ErrBugRepairExhausted
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
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_work_guards WHERE session_id=?`, in.SessionID).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
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
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_repairs(session_id,request,input_digest,ordinal,analysis_revision,analysis_digest,approval_review,cause,evidence,source_commit,source_tree,graph_attempt_id,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'reserved')`, in.SessionID, in.Request, digest, count+1, in.AnalysisRevision, in.AnalysisDigest, in.ApprovalReview, in.Cause, in.Evidence, in.SourceCommit, in.SourceTree, in.GraphAttemptID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, in.SessionID, "repair_reserved", in.Request, in.Cause); err != nil {
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
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "repair_dispatched", request, ""); err != nil {
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
			if _, err = tx.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=?`, session, "repair:"+request); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "repair_"+state, request, reason)
	})
}
