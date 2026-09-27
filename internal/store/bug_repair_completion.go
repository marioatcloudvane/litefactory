package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/bugrepair"
)

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
		var prior BugRepairCompletion
		err := tx.QueryRowContext(ctx, `SELECT session_id,request,result_id,result_revision,source_commit,source_tree,save_operation FROM bug_repair_completions WHERE session_id=? AND request=?`, in.SessionID, in.Request).Scan(&prior.SessionID, &prior.Request, &prior.ResultID, &prior.ResultRevision, &prior.SourceCommit, &prior.SourceTree, &prior.SaveOperation)
		if err == nil {
			if prior != in {
				return errors.New("store: repair completion replay conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state, err := bugExecutionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		r, err := bugRepairTx(ctx, tx, in.SessionID, in.Request)
		if err != nil {
			return err
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
		if err = e.Validate(criteria, true); err != nil {
			return err
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
		var scopeCount int
		// The factory must have observed the actual changes; whether they stay
		// within the analysis's expected files is evidence for the delivery
		// review, not a gate.
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_repair_scope_observations WHERE session_id=? AND request=? AND source_commit=? AND source_tree=?`, in.SessionID, in.Request, in.SourceCommit, in.SourceTree).Scan(&scopeCount); err != nil {
			return err
		}
		if scopeCount < 1 {
			return errors.New("store: repair scope is not established for immutable source")
		}
		evidence, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_repair_completions(session_id,request,result_id,result_revision,evidence,source_commit,source_tree,save_operation) VALUES(?,?,?,?,?,?,?,?)`, in.SessionID, in.Request, in.ResultID, in.ResultRevision, evidence, in.SourceCommit, in.SourceTree, in.SaveOperation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_repairs SET state='completed' WHERE session_id=? AND request=?`, in.SessionID, in.Request); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=?`, in.SessionID, "repair:"+in.Request); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, in.SessionID, "repair_completed", in.Request, in.SaveOperation)
	})
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
		err = tx.QueryRowContext(ctx, `SELECT session_id,request,result_id,result_revision,source_commit,source_tree,save_operation FROM bug_repair_completions WHERE session_id=? AND request=?`, session, request).Scan(&out.SessionID, &out.Request, &out.ResultID, &out.ResultRevision, &out.SourceCommit, &out.SourceTree, &out.SaveOperation)
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
