package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// BugAnalysisApplication names retained factory evidence, never terminal prose.
// InputDigest is calculated by the activity over its complete canonical input.
type BugAnalysisApplication struct {
	SessionID, ExpectedRevision        int64
	Invocation, Operation, InputDigest string
	ResultID, ResultRevision           int64
}

type bugRetainedAnalysis struct {
	Result struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
		Payload struct {
			Bug *struct {
				Evidence     []string                 `json:"evidence"`
				Response     string                   `json:"response"`
				Invocation   string                   `json:"invocation"`
				BaseRevision int64                    `json:"base_revision"`
				Analysis     *buganalysis.BugAnalysis `json:"analysis,omitempty"`
			} `json:"bug"`
		} `json:"payload"`
	} `json:"result"`
}

// ApplyBugAnalysis appends a revision from a retained shared result only after
// the matching successful invocation and graph consistency guard. Replays do
// not charge another pass, create another revision or disturb later sections.
func (d *DB) ApplyBugAnalysis(ctx context.Context, in BugAnalysisApplication) (BugAnalysisRevision, error) {
	var out BugAnalysisRevision
	if in.SessionID <= 0 || in.ExpectedRevision <= 0 || in.Invocation == "" || in.Operation == "" || !validSubmissionDigest(in.InputDigest) || in.ResultID <= 0 || in.ResultRevision <= 0 {
		return out, errors.New("store: invalid Bug analysis application")
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var revision int64
		var digest string
		err := tx.QueryRowContext(ctx, `SELECT revision,input_digest FROM bug_analysis_revisions WHERE session_id=? AND operation=?`, in.SessionID, in.Operation).Scan(&revision, &digest)
		if err == nil {
			if digest != in.InputDigest {
				return errors.New("store: Bug application replay conflict")
			}
			out, err = bugRevisionTx(ctx, tx, in.SessionID, revision)
			if err != nil {
				return err
			}
			if out.Invocation != in.Invocation || out.ResultID != in.ResultID || out.ResultRevision != in.ResultRevision {
				return errors.New("store: Bug application evidence conflict")
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
		if state.Revision != in.ExpectedRevision {
			return ErrBugRevisionConflict
		}
		pass, err := bugPassTx(ctx, tx, in.SessionID, in.Invocation)
		if err != nil {
			return err
		}
		if pass.State != "succeeded" {
			return errors.New("store: Bug application needs successful invocation")
		}
		var raw []byte
		var runID int64
		var accepted string
		err = tx.QueryRowContext(ctx, `SELECT v.snapshot,v.run_id,q.input_state FROM application_result_revisions v JOIN application_result_requests q ON q.id=v.request_row JOIN application_results r ON r.id=v.result_id JOIN sessions s ON s.task_ref=r.task WHERE v.result_id=? AND v.revision=? AND s.id=?`, in.ResultID, in.ResultRevision, in.SessionID).Scan(&raw, &runID, &accepted)
		if err != nil {
			return err
		}
		if runID != pass.RunID || accepted != "accepted" {
			return errors.New("store: Bug application shared-result binding mismatch")
		}
		run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, runID))
		if err != nil {
			return err
		}
		if !run.Succeeded() {
			return errors.New("store: Bug application run did not succeed")
		}
		var graphStatus, graphOutcome string
		if err = tx.QueryRowContext(ctx, `SELECT status,COALESCE(outcome,'') FROM graph_node_attempts WHERE id=? AND run_id=?`, pass.GraphAttemptID, runID).Scan(&graphStatus, &graphOutcome); err != nil {
			return err
		}
		var snapshot bugRetainedAnalysis
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			return fmt.Errorf("store: Bug retained result: %w", err)
		}
		proposal := snapshot.Result.Payload.Bug
		if proposal == nil || proposal.Invocation != in.Invocation || proposal.BaseRevision != state.CurrentAnalysis || (proposal.Analysis == nil && proposal.Response != "blocked") {
			return errors.New("store: retained Bug proposal missing or stale")
		}
		if graphStatus != "succeeded" || graphOutcome != "success" {
			return errors.New("store: Bug graph consistency evidence missing")
		}
		var retainedOutput []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload FROM graph_outputs WHERE node_attempt_id=? AND kind='json' ORDER BY id DESC LIMIT 1`, pass.GraphAttemptID).Scan(&retainedOutput); err != nil {
			return err
		}
		var binding struct {
			ResultID       int64 `json:"result_id"`
			ResultRevision int64 `json:"result_revision"`
		}
		if err = json.Unmarshal(retainedOutput, &binding); err != nil {
			return err
		}
		if binding.ResultID != in.ResultID || binding.ResultRevision != in.ResultRevision {
			return errors.New("store: Bug graph retained-result reference mismatch")
		}
		ready := proposal.Response == "done"
		if (ready && snapshot.Result.Outcome != "completed") || (proposal.Response == "ask" && snapshot.Result.Outcome != "incomplete") || (proposal.Response == "blocked" && snapshot.Result.Outcome != "blocked") {
			return errors.New("store: Bug proposal response/outcome mismatch")
		}
		if proposal.Response != "done" && proposal.Response != "ask" && proposal.Response != "blocked" {
			return errors.New("store: Bug proposal is not an analysis revision")
		}
		analysis := buganalysis.BugAnalysis{}
		if proposal.Analysis != nil {
			analysis = *proposal.Analysis
		}
		if proposal.Response == "blocked" && (snapshot.Result.Reason == "" || len(proposal.Evidence) == 0) {
			return errors.New("store: blocked Bug result requires reason and evidence")
		}
		if err = analysis.Validate(ready); err != nil {
			return err
		}
		if err = validateBugHistoryTx(ctx, tx, in.SessionID, analysis, ready); err != nil {
			return err
		}
		content, err := json.Marshal(analysis)
		if err != nil {
			return err
		}
		digest, err = analysis.Digest()
		if err != nil {
			return err
		}
		evidence, err := json.Marshal(proposal.Evidence)
		if err != nil {
			return err
		}
		revision = state.CurrentAnalysis + 1
		_, err = tx.ExecContext(ctx, `INSERT INTO bug_analysis_revisions(session_id,revision,digest,content,review_ready,invocation,operation,input_digest,result_id,result_revision,response,reason,evidence) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.SessionID, revision, digest, content, ready, in.Invocation, in.Operation, in.InputDigest, in.ResultID, in.ResultRevision, proposal.Response, snapshot.Result.Reason, evidence)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analysis_reviews SET active=0 WHERE session_id=? AND active=1`, in.SessionID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET current_analysis=?,revision=revision+1 WHERE session_id=?`, revision, in.SessionID); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, in.SessionID, "analysis", in.Operation, proposal.Response); err != nil {
			return err
		}
		out, err = bugRevisionTx(ctx, tx, in.SessionID, revision)
		return err
	})
	return out, err
}

func validateBugHistoryTx(ctx context.Context, tx *sql.Tx, session int64, a buganalysis.BugAnalysis, review bool) error {
	// Read and close the cursor before further SQL: the single connection is a
	// correctness boundary, and nested queries on an open cursor are avoided.
	rows, err := tx.QueryContext(ctx, `SELECT content FROM bug_analysis_revisions WHERE session_id=? ORDER BY revision`, session)
	if err != nil {
		return err
	}
	oldCriteria := make(map[string]buganalysis.BugCriterion)
	oldQuestions := make(map[string]buganalysis.BugQuestion)
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return errors.Join(err, rows.Close())
		}
		var prior buganalysis.BugAnalysis
		if err = json.Unmarshal(b, &prior); err != nil {
			return errors.Join(err, rows.Close())
		}
		for _, c := range prior.Criteria {
			oldCriteria[c.ID] = c
		}
		for _, q := range prior.Questions {
			oldQuestions[q.ID] = q
		}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, c := range a.Criteria {
		if old, ok := oldCriteria[c.ID]; ok && old != c {
			return fmt.Errorf("store: Bug criterion %q changes an existing meaning", c.ID)
		}
	}
	for _, q := range a.Questions {
		if old, ok := oldQuestions[q.ID]; ok && old != q {
			return fmt.Errorf("store: Bug question %q changes an existing meaning", q.ID)
		}
	}
	answered := make(map[string]bool)
	for _, ref := range a.AnswerReferences {
		var question string
		if err = tx.QueryRowContext(ctx, `SELECT question_id FROM bug_analysis_answers WHERE session_id=? AND observation=?`, session, ref).Scan(&question); err != nil {
			return fmt.Errorf("store: unresolved Bug answer reference: %w", err)
		}
		answered[question] = true
	}
	if review {
		// Omitting a previously asked material question cannot make it disappear.
		for _, q := range a.Questions {
			oldQuestions[q.ID] = q
		}
		for _, q := range oldQuestions {
			if q.Material && !answered[q.ID] {
				return fmt.Errorf("store: unresolved material Bug question %q", q.ID)
			}
		}
	}
	return nil
}
