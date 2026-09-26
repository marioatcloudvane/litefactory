package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/bugrepair"
)

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
		if err = e.Validate(criteria, false); err != nil {
			return err
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
		if _, err = tx.ExecContext(ctx, `DELETE FROM bug_work_guards WHERE session_id=? AND owner=?`, in.SessionID, "repair:"+in.Request); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, in.SessionID, "repair_"+p.Response, in.Request, retained.Result.Reason)
	})
}
