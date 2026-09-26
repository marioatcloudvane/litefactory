package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrBugAnalysisExhausted means all eight lifetime analysis passes are spent.
var ErrBugAnalysisExhausted = errors.New("store: eight Bug analysis passes exhausted")

// ErrBugAnalysisActive means a prior invocation still needs settlement.
var ErrBugAnalysisActive = errors.New("store: Bug analysis invocation remains active")

// ErrBugRevisionConflict means the execution changed since the supplied revision.
var ErrBugRevisionConflict = errors.New("store: Bug execution revision changed")

// BugAnalysisPass identifies one lifetime substantive invocation. A transport
// recovery keeps this identity; a fresh investigation must reserve a new one.
type BugAnalysisPass struct {
	SessionID               int64
	Invocation, InputDigest string
	Ordinal                 int
	GraphAttemptID, RunID   int64
	State, Reason           string
}

func bugPassTx(ctx context.Context, tx *sql.Tx, session int64, invocation string) (BugAnalysisPass, error) {
	var p BugAnalysisPass
	err := tx.QueryRowContext(ctx, `SELECT session_id,invocation,input_digest,ordinal,graph_attempt_id,COALESCE(run_id,0),state,reason FROM bug_analysis_passes WHERE session_id=? AND invocation=?`, session, invocation).Scan(&p.SessionID, &p.Invocation, &p.InputDigest, &p.Ordinal, &p.GraphAttemptID, &p.RunID, &p.State, &p.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

// ReserveBugAnalysisPass spends a slot before dispatch. Exact replay returns
// its original state even after exhaustion and never refunds failed work.
func (d *DB) ReserveBugAnalysisPass(ctx context.Context, in BugAnalysisPass, expectedRevision int64) (BugAnalysisPass, error) {
	var out BugAnalysisPass
	if in.SessionID <= 0 || in.Invocation == "" || len(in.Invocation) > 256 || !validSubmissionDigest(in.InputDigest) || in.GraphAttemptID <= 0 || expectedRevision <= 0 || in.Ordinal != 0 || in.RunID != 0 || in.State != "" || in.Reason != "" {
		return out, errors.New("store: invalid Bug analysis reservation")
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		old, err := bugPassTx(ctx, tx, in.SessionID, in.Invocation)
		if err == nil {
			if old.InputDigest != in.InputDigest || old.GraphAttemptID != in.GraphAttemptID {
				return errors.New("store: Bug invocation replay conflict")
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
		if state.Revision != expectedRevision {
			return ErrBugRevisionConflict
		}
		if state.AnalysisUsed >= 8 {
			return ErrBugAnalysisExhausted
		}
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analysis_passes WHERE session_id=? AND state IN('reserved','dispatched')`, in.SessionID).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return ErrBugAnalysisActive
		}
		var session int64
		var workflow, status string
		err = tx.QueryRowContext(ctx, `SELECT i.session_id,d.workflow,a.status FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.id=?`, in.GraphAttemptID).Scan(&session, &workflow, &status)
		if err != nil {
			return err
		}
		if session != in.SessionID || workflow != "bug" || status != "running" {
			return errors.New("store: Bug pass requires a running attempt in its own workflow")
		}
		in.Ordinal = state.AnalysisUsed + 1
		in.State = "reserved"
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_analysis_passes(session_id,invocation,input_digest,ordinal,graph_attempt_id,state) VALUES(?,?,?,?,?,'reserved')`, in.SessionID, in.Invocation, in.InputDigest, in.Ordinal, in.GraphAttemptID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET analysis_used=analysis_used+1,revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, in.SessionID, "reserved", in.Invocation, ""); err != nil {
			return err
		}
		out = in
		return nil
	})
	return out, err
}

// ClaimBugAnalysisDispatch allows exactly one launch of a proven undispatched
// reservation. A replay reports claimed=false instead of launching again.
func (d *DB) ClaimBugAnalysisDispatch(ctx context.Context, session int64, invocation, digest string, runID int64) (bool, error) {
	if runID <= 0 || !validSubmissionDigest(digest) {
		return false, errors.New("store: invalid Bug dispatch")
	}
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		pass, err := bugPassTx(ctx, tx, session, invocation)
		if err != nil {
			return err
		}
		if pass.InputDigest != digest {
			return errors.New("store: Bug dispatch input conflict")
		}
		if pass.State != "reserved" {
			if pass.RunID != runID {
				return errors.New("store: Bug dispatch run conflict")
			}
			return nil
		}
		run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, runID))
		if err != nil {
			return err
		}
		if run.SessionID != session || run.Workflow != "bug" || run.Mode != "" || (run.Status != RunPrelaunch && run.Status != RunRunning) {
			return errors.New("store: Bug dispatch needs its own unfinished run")
		}
		var boundRun int64
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(run_id,0),status FROM graph_node_attempts WHERE id=?`, pass.GraphAttemptID).Scan(&boundRun, &status); err != nil {
			return err
		}
		if boundRun != runID || status != "running" {
			return errors.New("store: Bug dispatch graph/run mismatch")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analysis_passes SET state='dispatched',run_id=? WHERE session_id=? AND invocation=?`, runID, session, invocation); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "dispatched", invocation, ""); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

// FinishBugAnalysisPass records process facts without applying application data.
// Succeeded requires the same durable successful run. Uncertain, failed and
// cancelled executions stay spent and cannot be revived by receipt recovery.
func (d *DB) FinishBugAnalysisPass(ctx context.Context, session int64, invocation, state, reason string) error {
	switch state {
	case "succeeded":
		if reason != "" {
			return errors.New("store: successful pass has failure reason")
		}
	case "failed", "cancelled", "uncertain":
		if strings.TrimSpace(reason) == "" {
			return errors.New("store: unsuccessful pass needs reason")
		}
	default:
		return errors.New("store: invalid Bug pass completion")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		pass, err := bugPassTx(ctx, tx, session, invocation)
		if err != nil {
			return err
		}
		if pass.State != "reserved" && pass.State != "dispatched" {
			if pass.State == state && pass.Reason == reason {
				return nil
			}
			return errors.New("store: terminal Bug pass cannot change")
		}
		if state == "succeeded" {
			if pass.State != "dispatched" {
				return errors.New("store: undispatched Bug pass cannot succeed")
			}
			run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, pass.RunID))
			if err != nil {
				return err
			}
			if !run.Succeeded() {
				return errors.New("store: Bug pass has no matching successful invocation")
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analysis_passes SET state=?,reason=? WHERE session_id=? AND invocation=?`, state, reason, session, invocation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "pass_"+state, invocation, reason)
	})
}

// GetBugAnalysisPass returns the retained reservation and process state.
func (d *DB) GetBugAnalysisPass(ctx context.Context, session int64, invocation string) (BugAnalysisPass, error) {
	var out BugAnalysisPass
	err := d.withTx(ctx, func(tx *sql.Tx) error { var err error; out, err = bugPassTx(ctx, tx, session, invocation); return err })
	if err != nil {
		return out, fmt.Errorf("store: read Bug pass: %w", err)
	}
	return out, nil
}
