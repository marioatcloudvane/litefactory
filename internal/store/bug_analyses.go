package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/buganalysis"
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

// BugAnalysisRevision is an immutable analysis and its retained result provenance.
type BugAnalysisRevision struct {
	Revision                 int64
	Digest                   string
	Analysis                 buganalysis.BugAnalysis
	ReviewReady              bool
	Response, Reason         string
	Evidence                 []string
	Invocation, Operation    string
	ResultID, ResultRevision int64
}

// BugAnalysisApproval authorizes one exact published analysis revision.
type BugAnalysisApproval struct {
	Review           string
	AnalysisRevision int64
	AnalysisDigest   string
	ReplyID, OwnerID int64
	ApprovedAt       time.Time
}

func bugPassTx(ctx context.Context, tx *sql.Tx, session int64, invocation string) (BugAnalysisPass, error) {
	var p BugAnalysisPass
	var runID sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT session_id,invocation,input_digest,ordinal,graph_attempt_id,run_id,state,pass_reason FROM bug_analyses WHERE session_id=? AND invocation=?`, session, invocation).Scan(&p.SessionID, &p.Invocation, &p.InputDigest, &p.Ordinal, &p.GraphAttemptID, &runID, &p.State, &p.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.RunID = runID.Int64
	return p, nil
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
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analyses WHERE session_id=? AND state IN('reserved','dispatched')`, in.SessionID).Scan(&active); err != nil {
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
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_analyses(session_id,invocation,input_digest,ordinal,graph_attempt_id,state) VALUES(?,?,?,?,?,'reserved')`, in.SessionID, in.Invocation, in.InputDigest, in.Ordinal, in.GraphAttemptID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_tasks SET analysis_used=analysis_used+1,revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, in.SessionID, "reserved", in.Invocation, 0, "", nil); err != nil {
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
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET state='dispatched',run_id=? WHERE session_id=? AND invocation=?`, runID, session, invocation); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "dispatched", invocation, 0, "", nil); err != nil {
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
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET state=?,pass_reason=? WHERE session_id=? AND invocation=?`, state, reason, session, invocation); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, session); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "pass_"+state, invocation, 0, "", []byte(bugJSONString(reason)))
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

func bugRevisionTx(ctx context.Context, tx *sql.Tx, session, revision int64) (BugAnalysisRevision, error) {
	var out BugAnalysisRevision
	var content, evidence []byte
	var reviewReady bool
	err := tx.QueryRowContext(ctx, `SELECT revision,digest,content,review_ready,invocation,operation,result_id,result_revision,response,reason,evidence FROM bug_analyses WHERE session_id=? AND revision=?`, session, revision).Scan(&out.Revision, &out.Digest, &content, &reviewReady, &out.Invocation, &out.Operation, &out.ResultID, &out.ResultRevision, &out.Response, &out.Reason, &evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.ReviewReady = reviewReady
	if err = json.Unmarshal(evidence, &out.Evidence); err != nil {
		return out, err
	}
	if err = json.Unmarshal(content, &out.Analysis); err != nil {
		return out, fmt.Errorf("store: decode Bug analysis: %w", err)
	}
	digest, err := out.Analysis.Digest()
	if err != nil {
		return out, err
	}
	if digest != out.Digest {
		return out, errors.New("store: retained Bug analysis digest mismatch")
	}
	return out, nil
}

// CurrentBugAnalysis returns only approval applicable to the current revision.
// Historical approvals remain retained when a new revision supersedes them.
func (d *DB) CurrentBugAnalysis(ctx context.Context, session int64) (BugExecution, BugAnalysisRevision, *BugAnalysisApproval, error) {
	var state BugExecution
	var revision BugAnalysisRevision
	var approval *BugAnalysisApproval
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		state, err = bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if state.CurrentAnalysis == 0 {
			return nil
		}
		revision, err = bugRevisionTx(ctx, tx, session, state.CurrentAnalysis)
		if err != nil {
			return err
		}
		var a BugAnalysisApproval
		var at sql.NullString
		var replyID, ownerID sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT review,approval_reply_id,approval_owner_id,approval_approved_at FROM bug_analyses WHERE session_id=? AND revision=? AND digest=? AND review_active=1 AND approval_reply_id IS NOT NULL`, session, revision.Revision, revision.Digest).Scan(&a.Review, &replyID, &ownerID, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		a.ReplyID, a.OwnerID, a.AnalysisRevision, a.AnalysisDigest = replyID.Int64, ownerID.Int64, revision.Revision, revision.Digest
		a.ApprovedAt, err = time.Parse(time.RFC3339Nano, at.String)
		if err != nil {
			return err
		}
		approval = &a
		return nil
	})
	return state, revision, approval, err
}

// GetBugAnalysisRevision reads immutable history without granting approval.
func (d *DB) GetBugAnalysisRevision(ctx context.Context, session, revision int64) (BugAnalysisRevision, error) {
	var out BugAnalysisRevision
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = bugRevisionTx(ctx, tx, session, revision)
		return err
	})
	return out, err
}

// BugAnalysisAnswer is an append-only authenticated owner observation, folded
// into bug_events (kind "answer") instead of its own dedicated table.
type BugAnalysisAnswer struct {
	Observation, QuestionID string
	CommentID, AuthorID     int64
	Body                    string
	ObservedAt              time.Time
}

// RecordBugAnalysisAnswer appends an authenticated observation; an edit uses a
// new observation identity and preserves the original answer.
func (d *DB) RecordBugAnalysisAnswer(ctx context.Context, session int64, in BugAnalysisAnswer) error {
	return d.withTx(ctx, func(tx *sql.Tx) error { return recordBugAnalysisAnswerTx(ctx, tx, session, in) })
}

func recordBugAnalysisAnswerTx(ctx context.Context, tx *sql.Tx, session int64, in BugAnalysisAnswer) error {
	if in.Observation == "" || in.QuestionID == "" || in.CommentID <= 0 || in.AuthorID <= 0 || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || in.ObservedAt.IsZero() {
		return errors.New("store: invalid Bug answer")
	}
	identity, err := bugIdentityTx(ctx, tx, session)
	if err != nil {
		return err
	}
	if identity.OwnerID != in.AuthorID {
		return errors.New("store: Bug answer author is not the numeric owner")
	}
	state, err := bugExecutionTx(ctx, tx, session)
	if err != nil {
		return err
	}
	revision, err := bugRevisionTx(ctx, tx, session, state.CurrentAnalysis)
	if err != nil {
		return err
	}
	found := false
	for _, q := range revision.Analysis.Questions {
		if q.ID == in.QuestionID {
			found = true
		}
	}
	if !found {
		return errors.New("store: Bug answer references no current question")
	}
	if err = bugEventJSONTx(ctx, tx, session, "answer", in.Observation, in); err != nil {
		return err
	}
	return bugBumpRevisionTx(ctx, tx, session)
}

// BugAnalysisAnswers supplies the same append-only context for warm and cold
// preparation. Bounded pages prevent unbounded owner discussion allocation.
func (d *DB) BugAnalysisAnswers(ctx context.Context, session int64, after string, limit int) ([]BugAnalysisAnswer, error) {
	if session <= 0 || limit < 1 || limit > 100 {
		return nil, errors.New("store: invalid Bug answer page")
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='answer' AND reference>? ORDER BY reference LIMIT ?`, session, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugAnalysisAnswer
	for rows.Next() {
		var content []byte
		if err = rows.Scan(&content); err != nil {
			return nil, err
		}
		var a BugAnalysisAnswer
		if err = json.Unmarshal(content, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// bugAnalysisAnsweredQuestions returns the question ids with at least one
// retained answer, for review-time material-question checks.
func bugAnalysisAnsweredQuestionTx(ctx context.Context, tx *sql.Tx, session int64, observation string) (string, error) {
	content, err := bugEventByReferenceTx(ctx, tx, session, "answer", observation)
	if err != nil {
		return "", fmt.Errorf("store: unresolved Bug answer reference: %w", err)
	}
	var a BugAnalysisAnswer
	if err = json.Unmarshal(content, &a); err != nil {
		return "", err
	}
	return a.QuestionID, nil
}

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
		err := tx.QueryRowContext(ctx, `SELECT revision,revision_input_digest FROM bug_analyses WHERE session_id=? AND operation=?`, in.SessionID, in.Operation).Scan(&revision, &digest)
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
		notices, err := analysis.Validate(ready)
		if err != nil {
			return err
		}
		for _, n := range notices {
			if err = recordBugFactTx(ctx, tx, in.SessionID, pass.GraphAttemptID, BugFactStageAnalysis, n.Reference, n.Text); err != nil {
				return err
			}
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
		// pass.Ordinal only counts reservations (including failed ones) against the
		// eight-pass budget; it is not the applied-revision sequence and must not be
		// asserted equal to it (a failed earlier pass in the bug's lifetime makes
		// them diverge by construction). The optimistic-concurrency/no-lost-update
		// property this used to (incorrectly) guard is already enforced above: the
		// caller-supplied ExpectedRevision was checked against state.Revision, and
		// proposal.BaseRevision (the trusted CurrentAnalysis the pass was actually
		// dispatched against, validated by agentresult before this result could ever
		// be retained) was checked against the current state.CurrentAnalysis. Either
		// mismatch already rejects a stale or racing application before this point.
		revision = state.CurrentAnalysis + 1
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET revision=?,digest=?,content=?,review_ready=?,operation=?,revision_input_digest=?,result_id=?,result_revision=?,response=?,reason=?,evidence=? WHERE session_id=? AND invocation=?`, revision, digest, content, ready, in.Operation, in.InputDigest, in.ResultID, in.ResultRevision, proposal.Response, snapshot.Result.Reason, evidence, in.SessionID, in.Invocation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_tasks SET current_analysis=?,revision=revision+1 WHERE session_id=?`, revision, in.SessionID); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, in.SessionID, "analysis", in.Operation, 0, "", []byte(bugJSONString(proposal.Response))); err != nil {
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
	rows, err := tx.QueryContext(ctx, `SELECT content FROM bug_analyses WHERE session_id=? AND content IS NOT NULL ORDER BY revision`, session)
	if err != nil {
		return err
	}
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
		for _, q := range prior.Questions {
			oldQuestions[q.ID] = q
		}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	// Criteria may be reworded in a new revision: the owner approves each
	// revision, and repair and checks bind to the approved one. Questions keep
	// their text because owner answers refer to their IDs.
	for _, q := range a.Questions {
		if old, ok := oldQuestions[q.ID]; ok && old != q {
			return fmt.Errorf("store: Bug question %q changes an existing meaning", q.ID)
		}
	}
	answered := make(map[string]bool)
	for _, ref := range a.AnswerReferences {
		question, err := bugAnalysisAnsweredQuestionTx(ctx, tx, session, ref)
		if err != nil {
			return err
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

// ConsumeBugAnalysisAnswers retains an authenticated question reply and consumes
// its published wait atomically. Each selected question receives the unchanged
// owner reply as an observation, never an inferred answer. The parent decides
// whether another substantive investigation is allowed; this operation spends no
// pass, so answers to an eighth-pass question can still be retained.
func (d *DB) ConsumeBugAnalysisAnswers(ctx context.Context, session int64, in GraphOwnerReply, questionIDs []string, expectedRevision int64) error {
	if session <= 0 || in.InstanceID <= 0 || in.OwnerID <= 0 || in.AuthorID != in.OwnerID || in.CommentID <= in.QuestionCommentID || in.QuestionCommentID <= 0 || in.Mode != "" || in.TaskRef == "" || in.SourceRef != in.TaskRef || in.DeliveryKey == "" || in.At.IsZero() || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || len(questionIDs) == 0 || len(questionIDs) > 100 || expectedRevision <= 0 {
		return errors.New("store: invalid Bug question reply")
	}
	questionIDs = slices.Clone(questionIDs)
	slices.Sort(questionIDs)
	seen := make(map[string]bool, len(questionIDs))
	for _, id := range questionIDs {
		if id == "" || seen[id] {
			return errors.New("store: invalid or duplicate Bug answer question")
		}
		seen[id] = true
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		identity, err := bugIdentityTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if identity.OwnerID != in.OwnerID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
			return errors.New("store: Bug answer task/owner mismatch")
		}
		wait, err := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, in.InstanceID))
		if err != nil {
			return err
		}
		replay := wait.State == "allocated" && wait.ReplyID == in.CommentID && wait.ReplyAuthorID == in.AuthorID && wait.ReplyBody == in.Body
		if wait.State != "waiting" && !replay {
			return ErrGraphOwnerWait
		}
		var actualSession, comment, source int64
		var workflow, status, kind, body string
		var candidate []byte
		var published bool
		err = tx.QueryRowContext(ctx, `SELECT i.session_id,d.workflow,i.status,c.kind,c.candidate,c.source_attempt_id,p.comment_id,p.completed,p.body FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id JOIN graph_comment_intents c ON c.instance_id=i.id JOIN report_deliveries p ON p.delivery_key=c.delivery_key WHERE i.id=? AND c.delivery_key=?`, in.InstanceID, in.DeliveryKey).Scan(&actualSession, &workflow, &status, &kind, &candidate, &source, &comment, &published, &body)
		if err != nil {
			return err
		}
		if actualSession != session || workflow != "bug" || kind != "question" || source != wait.SourceAttemptID || !published || comment != in.QuestionCommentID || body == "" || (!replay && status != GraphRunning) {
			return errors.New("store: Bug answer requires its confirmed published question")
		}
		var displayed BugAnalysisRevision
		if err = json.Unmarshal(candidate, &displayed); err != nil {
			return err
		}
		if displayed.Response != "ask" || displayed.Revision <= 0 {
			return errors.New("store: invalid Bug question projection")
		}
		current, err := bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if !replay && (current.Revision != expectedRevision || current.CurrentAnalysis != displayed.Revision) {
			return ErrBugRevisionConflict
		}
		revision, err := bugRevisionTx(ctx, tx, session, displayed.Revision)
		if err != nil {
			return err
		}
		if revision.Digest != displayed.Digest || revision.Invocation != displayed.Invocation {
			return errors.New("store: Bug question projection differs from retained analysis")
		}
		pass, err := bugPassTx(ctx, tx, session, revision.Invocation)
		if err != nil {
			return err
		}
		if pass.GraphAttemptID != source || pass.State != "succeeded" {
			return errors.New("store: Bug question source differs from successful analysis")
		}
		for _, id := range questionIDs {
			found := false
			for _, q := range revision.Analysis.Questions {
				if q.ID == id {
					found = true
					break
				}
			}
			if !found {
				return errors.New("store: answer names a question outside the displayed batch")
			}
		}
		// One event stores the complete selected question set, so changed replay cannot
		// add an answer after the reply has already been consumed.
		selected, err := json.Marshal(questionIDs)
		if err != nil {
			return err
		}
		operation := fmt.Sprintf("question-reply:%s:%d", in.DeliveryKey, in.CommentID)
		if replay {
			existing, err := bugEventByReferenceTx(ctx, tx, session, "question_reply", operation)
			if err != nil {
				return err
			}
			if string(existing) != string(selected) {
				return errors.New("store: Bug answer replay changed its question batch")
			}
			return nil
		}
		for _, id := range questionIDs {
			observation := fmt.Sprintf("%s:%s", operation, id)
			if err = recordBugAnalysisAnswerTx(ctx, tx, session, BugAnalysisAnswer{Observation: observation, QuestionID: id, CommentID: in.CommentID, AuthorID: in.AuthorID, Body: in.Body, ObservedAt: in.At}); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), in.DeliveryKey); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "question_reply", operation, 0, "", selected)
	})
}

// BugAnalysisReview binds the displayed projection to one immutable analysis.
// The delivery journal independently establishes actual publication.
type BugAnalysisReview struct {
	SessionID                                  int64
	Review                                     string
	AnalysisRevision                           int64
	AnalysisDigest, DeliveryKey, ContentDigest string
	CommentID                                  int64
	Active                                     bool
}

type bugAnalysisReviewEventContent struct {
	Review                                     string
	AnalysisRevision                           int64
	AnalysisDigest, DeliveryKey, ContentDigest string
	CommentID                                  int64
}

// bugAnalysisReviewByReviewTx resolves a review by its own identifier, whether
// it is the pass's current review or one superseded into bug_events history.
func bugAnalysisReviewByReviewTx(ctx context.Context, tx *sql.Tx, session int64, review string) (BugAnalysisReview, error) {
	var r BugAnalysisReview
	var commentID sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT session_id,review,revision,digest,review_delivery_key,review_content_digest,review_comment_id,review_active FROM bug_analyses WHERE session_id=? AND review=?`, session, review).Scan(&r.SessionID, &r.Review, &r.AnalysisRevision, &r.AnalysisDigest, &r.DeliveryKey, &r.ContentDigest, &commentID, &r.Active)
	if err == nil {
		r.CommentID = commentID.Int64
		return r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	content, err := bugEventByReferenceTx(ctx, tx, session, "review", review)
	if err != nil {
		return r, err
	}
	var c bugAnalysisReviewEventContent
	if err = json.Unmarshal(content, &c); err != nil {
		return r, err
	}
	return BugAnalysisReview{SessionID: session, Review: c.Review, AnalysisRevision: c.AnalysisRevision, AnalysisDigest: c.AnalysisDigest, DeliveryKey: c.DeliveryKey, ContentDigest: c.ContentDigest, CommentID: c.CommentID, Active: false}, nil
}

// bugAnalysisReviewByDeliveryKeyTx resolves a review by its unique delivery
// key, checking both the live per-pass row and superseded event history so a
// late reply to an older review still resolves.
func bugAnalysisReviewByDeliveryKeyTx(ctx context.Context, tx *sql.Tx, session int64, key string) (BugAnalysisReview, error) {
	var review string
	err := tx.QueryRowContext(ctx, `SELECT review FROM bug_analyses WHERE session_id=? AND review_delivery_key=?`, session, key).Scan(&review)
	if err == nil {
		return bugAnalysisReviewByReviewTx(ctx, tx, session, review)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BugAnalysisReview{}, err
	}
	content, err := bugEventByDeliveryKeyTx(ctx, tx, session, "review", key)
	if err != nil {
		return BugAnalysisReview{}, err
	}
	var c bugAnalysisReviewEventContent
	if err = json.Unmarshal(content, &c); err != nil {
		return BugAnalysisReview{}, err
	}
	return BugAnalysisReview{SessionID: session, Review: c.Review, AnalysisRevision: c.AnalysisRevision, AnalysisDigest: c.AnalysisDigest, DeliveryKey: c.DeliveryKey, ContentDigest: c.ContentDigest, CommentID: c.CommentID, Active: false}, nil
}

// RetainBugAnalysisReview records the immutable review before publication. It
// does not mark publication or grant approval. Its source is the exact pass
// that produced the current reviewable analysis, never another task's comment.
func (d *DB) RetainBugAnalysisReview(ctx context.Context, in BugAnalysisReview, expectedRevision int64) error {
	if in.SessionID <= 0 || in.Review == "" || in.AnalysisRevision <= 0 || !validSubmissionDigest(in.AnalysisDigest) || in.DeliveryKey == "" || !validSubmissionDigest(in.ContentDigest) || in.CommentID != 0 || in.Active || expectedRevision <= 0 {
		return errors.New("store: invalid Bug analysis review")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		old, err := bugAnalysisReviewByReviewTx(ctx, tx, in.SessionID, in.Review)
		if err == nil {
			if old.AnalysisRevision != in.AnalysisRevision || old.AnalysisDigest != in.AnalysisDigest || old.DeliveryKey != in.DeliveryKey || old.ContentDigest != in.ContentDigest {
				return errors.New("store: Bug review identity conflict")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		state, err := bugExecutionTx(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if state.Revision != expectedRevision || state.CurrentAnalysis != in.AnalysisRevision {
			return ErrBugRevisionConflict
		}
		analysis, err := bugRevisionTx(ctx, tx, in.SessionID, in.AnalysisRevision)
		if err != nil {
			return err
		}
		if !analysis.ReviewReady || analysis.Digest != in.AnalysisDigest {
			return errors.New("store: Bug analysis is not review ready")
		}
		pass, err := bugPassTx(ctx, tx, in.SessionID, analysis.Invocation)
		if err != nil {
			return err
		}
		var session, source int64
		var workflow, kind string
		err = tx.QueryRowContext(ctx, `SELECT i.session_id,c.source_attempt_id,d.workflow,c.kind FROM graph_comment_intents c JOIN graph_instances i ON i.id=c.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE c.delivery_key=?`, in.DeliveryKey).Scan(&session, &source, &workflow, &kind)
		if err != nil {
			return err
		}
		if session != in.SessionID || source != pass.GraphAttemptID || workflow != "bug" || kind != "review" {
			return errors.New("store: Bug review source mismatch")
		}
		if err = bugRetireCurrentAnalysisReviewTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET review=?,review_delivery_key=?,review_content_digest=?,review_comment_id=NULL,review_active=1 WHERE session_id=? AND invocation=?`, in.Review, in.DeliveryKey, in.ContentDigest, in.SessionID, pass.Invocation); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		return bugEventJSONTx(ctx, tx, in.SessionID, "review", in.Review, bugAnalysisReviewEventContent{Review: in.Review, AnalysisRevision: in.AnalysisRevision, AnalysisDigest: in.AnalysisDigest, DeliveryKey: in.DeliveryKey, ContentDigest: in.ContentDigest})
	})
}

// bugRetireCurrentAnalysisReviewTx moves the current active review (if any) for
// every pass of the bug into bug_events, so it stays resolvable by delivery
// key even after superseded, then clears the active flag.
func bugRetireCurrentAnalysisReviewTx(ctx context.Context, tx *sql.Tx, session int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT invocation,review,revision,digest,review_delivery_key,review_content_digest,review_comment_id FROM bug_analyses WHERE session_id=? AND review_active=1`, session)
	if err != nil {
		return err
	}
	type retiring struct {
		invocation string
		content    bugAnalysisReviewEventContent
	}
	var toRetire []retiring
	for rows.Next() {
		var r retiring
		var commentID sql.NullInt64
		if err = rows.Scan(&r.invocation, &r.content.Review, &r.content.AnalysisRevision, &r.content.AnalysisDigest, &r.content.DeliveryKey, &r.content.ContentDigest, &commentID); err != nil {
			return errors.Join(err, rows.Close())
		}
		r.content.CommentID = commentID.Int64
		toRetire = append(toRetire, r)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, r := range toRetire {
		if err = bugEventJSONTx(ctx, tx, session, "review", r.content.Review, r.content); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET review_active=0 WHERE session_id=? AND invocation=?`, session, r.invocation); err != nil {
			return err
		}
	}
	return nil
}

// ConfirmBugAnalysisReview consumes the existing confirmed publication journal.
// It validates the configured bot and exact rendered bytes instead of treating
// retained content, a request acknowledgment or a pending POST as publication.
func (d *DB) ConfirmBugAnalysisReview(ctx context.Context, session int64, review string, botID int64) (BugAnalysisReview, error) {
	var out BugAnalysisReview
	if botID <= 0 {
		return out, errors.New("store: missing Bug review publisher identity")
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugAnalysisReviewByReviewTx(ctx, tx, session, review)
		if err != nil {
			return err
		}
		var observedBot, comment, observedSession int64
		var body string
		var complete bool
		err = tx.QueryRowContext(ctx, `SELECT session_id,bot_user_id,COALESCE(comment_id,0),body,completed FROM report_deliveries WHERE delivery_key=?`, r.DeliveryKey).Scan(&observedSession, &observedBot, &comment, &body, &complete)
		if err != nil {
			return err
		}
		if !r.Active || !complete || comment <= 0 || observedSession != session || observedBot != botID || submissionDigest([]byte(body)) != r.ContentDigest {
			return errors.New("store: Bug review lacks matching confirmed publication")
		}
		if r.CommentID != 0 && r.CommentID != comment {
			return errors.New("store: Bug review publication identity changed")
		}
		if r.CommentID == 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET review_comment_id=? WHERE session_id=? AND review=?`, comment, session, review); err != nil {
				return err
			}
			// The creation-time "review" event was retained with CommentID 0 (the
			// comment did not exist yet). Synchronize its content here so a later
			// retirement of this now-published review re-derives and re-writes the
			// SAME content under the SAME (session,kind,reference) key instead of
			// tripping bugEventTx's idempotent-append conflict guard against the
			// stale creation-time snapshot.
			content, err := json.Marshal(bugAnalysisReviewEventContent{Review: r.Review, AnalysisRevision: r.AnalysisRevision, AnalysisDigest: r.AnalysisDigest, DeliveryKey: r.DeliveryKey, ContentDigest: r.ContentDigest, CommentID: comment})
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE bug_events SET content=? WHERE session_id=? AND kind='review' AND reference=?`, content, session, review); err != nil {
				return err
			}
			if err = bugEventTx(ctx, tx, session, "review_published", review, 0, "", nil); err != nil {
				return err
			}
		}
		r.CommentID = comment
		out = r
		return nil
	})
	return out, err
}

// ApproveBugAnalysis is the narrowly scoped factory reply-consumption path.
// It does not allocate a graph attempt or invoke the analyst, so pass eight can
// still be approved. Generic graph reply allocation remains unchanged.
func (d *DB) ApproveBugAnalysis(ctx context.Context, session int64, review string, in GraphOwnerReply, expectedRevision int64) (BugAnalysisApproval, error) {
	var out BugAnalysisApproval
	if session <= 0 || review == "" || in.OwnerID <= 0 || in.AuthorID != in.OwnerID || in.CommentID <= 0 || in.QuestionCommentID <= 0 || in.Mode != "" || in.TaskRef == "" || in.SourceRef != in.TaskRef || in.At.IsZero() || expectedRevision <= 0 {
		return out, errors.New("store: invalid authenticated Bug approval")
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		identity, err := bugIdentityTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if identity.OwnerID != in.OwnerID {
			return errors.New("store: Bug owner differs from configured identity")
		}
		r, err := bugAnalysisReviewByReviewTx(ctx, tx, session, review)
		if err != nil {
			return err
		}
		var invocation string
		var approvedAt sql.NullString
		var replyID, ownerID sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT invocation,approval_reply_id,approval_owner_id,approval_approved_at FROM bug_analyses WHERE session_id=? AND review=? AND approval_reply_id IS NOT NULL`, session, review).Scan(&invocation, &replyID, &ownerID, &approvedAt)
		if err == nil {
			out = BugAnalysisApproval{Review: review, AnalysisRevision: r.AnalysisRevision, AnalysisDigest: r.AnalysisDigest, ReplyID: replyID.Int64, OwnerID: ownerID.Int64}
			if out.ReplyID != in.CommentID || in.DeliveryKey != r.DeliveryKey || strings.TrimSpace(in.Body) != fmt.Sprintf("/approve-bug analysis %d", r.AnalysisRevision) || in.QuestionCommentID != r.CommentID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
				return errors.New("store: Bug approval replay conflict")
			}
			out.ApprovedAt, err = time.Parse(time.RFC3339Nano, approvedAt.String)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state, err := bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if state.Revision != expectedRevision || state.CurrentAnalysis != r.AnalysisRevision {
			return ErrBugRevisionConflict
		}
		if !r.Active || r.CommentID <= 0 || in.QuestionCommentID != r.CommentID || in.CommentID <= r.CommentID || in.DeliveryKey != r.DeliveryKey || strings.TrimSpace(in.Body) != fmt.Sprintf("/approve-bug analysis %d", r.AnalysisRevision) {
			return errors.New("store: Bug approval must exactly match active published analysis review")
		}
		wait, err := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, r.DeliveryKey, in.InstanceID))
		if err != nil {
			return err
		}
		if wait.State != "waiting" {
			return ErrGraphOwnerWait
		}
		var task, workflow, status string
		var actualSession, comment int64
		var complete bool
		var body string
		err = tx.QueryRowContext(ctx, `SELECT s.task_ref,i.session_id,d.workflow,i.status,p.comment_id,p.completed,p.body FROM graph_instances i JOIN sessions s ON s.id=i.session_id JOIN graph_definitions d ON d.id=i.definition_id JOIN report_deliveries p ON p.delivery_key=? WHERE i.id=?`, r.DeliveryKey, in.InstanceID).Scan(&task, &actualSession, &workflow, &status, &comment, &complete, &body)
		if err != nil {
			return err
		}
		if task != in.TaskRef || actualSession != session || workflow != "bug" || status != "running" || !complete || comment != r.CommentID || submissionDigest([]byte(body)) != r.ContentDigest {
			return errors.New("store: Bug approval task/publication binding mismatch")
		}
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analyses WHERE session_id=? AND state IN('reserved','dispatched')`, session).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return ErrBugAnalysisActive
		}
		var invocationForReview string
		if err = tx.QueryRowContext(ctx, `SELECT invocation FROM bug_analyses WHERE session_id=? AND review=?`, session, review).Scan(&invocationForReview); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analyses SET approval_reply_id=?,approval_owner_id=?,approval_approved_at=? WHERE session_id=? AND invocation=?`, in.CommentID, in.OwnerID, in.At.UTC().Format(time.RFC3339Nano), session, invocationForReview); err != nil {
			return err
		}
		// The allocated state is terminal consumption. No fabricated agent/run or
		// continuation attempt is supplied; the parent owns its next transition.
		if _, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), r.DeliveryKey); err != nil {
			return err
		}
		if err = bugBumpRevisionTx(ctx, tx, session); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "approved", review, 0, "", nil); err != nil {
			return err
		}
		out = BugAnalysisApproval{Review: review, AnalysisRevision: r.AnalysisRevision, AnalysisDigest: r.AnalysisDigest, ReplyID: in.CommentID, OwnerID: in.OwnerID, ApprovedAt: in.At}
		return nil
	})
	return out, err
}

// CurrentBugAnalysisReview returns only the current active review and its
// matching owner wait. It is a read-only correlation input; approval rechecks
// these bindings atomically before consuming the owner reply.
func (d *DB) CurrentBugAnalysisReview(ctx context.Context, session int64) (BugExecution, BugAnalysisReview, GraphOwnerWait, error) {
	var state BugExecution
	var review BugAnalysisReview
	var wait GraphOwnerWait
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		state, err = bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		var id string
		if err = tx.QueryRowContext(ctx, `SELECT review FROM bug_analyses WHERE session_id=? AND revision=? AND review_active=1`, session, state.CurrentAnalysis).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		review, err = bugAnalysisReviewByReviewTx(ctx, tx, session, id)
		if err != nil {
			return err
		}
		wait, err = scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=?`, review.DeliveryKey))
		return err
	})
	return state, review, wait, err
}

// BugAnalysisReviewChange preserves authenticated owner discussion separately
// from question answers and approval. It never authorizes the changed remedy.
// Folded into bug_events (kind "review_change") instead of its own table.
type BugAnalysisReviewChange struct {
	CommentID, AuthorID int64
	Review, Body        string
	At                  time.Time
}

// BugAnalysisReviewChanges retrieves immutable authenticated review feedback.
func (d *DB) BugAnalysisReviewChanges(ctx context.Context, session int64) ([]BugAnalysisReviewChange, error) {
	rows, e := d.sql.QueryContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='review_change' ORDER BY reference LIMIT 4097`, session)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []BugAnalysisReviewChange
	for rows.Next() {
		var content []byte
		if e = rows.Scan(&content); e != nil {
			return nil, e
		}
		var r BugAnalysisReviewChange
		if e = json.Unmarshal(content, &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if len(out) > 4096 {
		return nil, errors.New("bug review context exceeds bound")
	}
	return out, rows.Err()
}

// ConsumeBugAnalysisReviewChange consumes the exact published review wait without granting approval.
func (d *DB) ConsumeBugAnalysisReviewChange(ctx context.Context, session int64, review string, in GraphOwnerReply, expected int64) error {
	if in.OwnerID <= 0 || in.AuthorID != in.OwnerID || in.CommentID <= in.QuestionCommentID || in.QuestionCommentID <= 0 || in.SourceRef != in.TaskRef || in.Mode != "" || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || in.At.IsZero() || strings.Contains(in.Body, "/approve-bug") {
		return errors.New("invalid Bug analysis review change")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		identity, e := bugIdentityTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if identity.OwnerID != in.OwnerID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
			return errors.New("bug review change task/owner mismatch")
		}
		r, e := bugAnalysisReviewByReviewTx(ctx, tx, session, review)
		if e != nil {
			return e
		}
		w, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, in.InstanceID))
		if e != nil {
			return e
		}
		replayKind := "review_change"
		if w.State == "allocated" && w.ReplyID == in.CommentID && w.ReplyAuthorID == in.AuthorID && w.ReplyBody == in.Body {
			content, e := bugEventByReferenceTx(ctx, tx, session, replayKind, fmt.Sprint(in.CommentID))
			if e != nil {
				return e
			}
			var old BugAnalysisReviewChange
			if e = json.Unmarshal(content, &old); e != nil {
				return e
			}
			if old.Review != review || old.Body != in.Body || old.AuthorID != in.AuthorID {
				return errors.New("bug review change replay mismatch")
			}
			return nil
		}
		if w.State != "waiting" || !r.Active || r.CommentID != in.QuestionCommentID || r.DeliveryKey != in.DeliveryKey {
			return ErrGraphOwnerWait
		}
		state, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if state.Revision != expected || state.CurrentAnalysis != r.AnalysisRevision {
			return ErrBugRevisionConflict
		}
		var published bool
		var comment, actualSession int64
		var body, workflow, status string
		e = tx.QueryRowContext(ctx, `SELECT p.completed,p.comment_id,p.body,i.session_id,d.workflow,i.status FROM report_deliveries p JOIN graph_instances i ON i.id=? JOIN graph_definitions d ON d.id=i.definition_id WHERE p.delivery_key=?`, in.InstanceID, r.DeliveryKey).Scan(&published, &comment, &body, &actualSession, &workflow, &status)
		if e != nil {
			return e
		}
		if !published || comment != r.CommentID || submissionDigest([]byte(body)) != r.ContentDigest || actualSession != session || workflow != "bug" || status != GraphRunning {
			return errors.New("bug review change lacks matching confirmed publication")
		}
		if e = bugEventJSONTx(ctx, tx, session, replayKind, fmt.Sprint(in.CommentID), BugAnalysisReviewChange{CommentID: in.CommentID, AuthorID: in.AuthorID, Review: review, Body: in.Body, At: in.At}); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), r.DeliveryKey); e != nil {
			return e
		}
		// Historical approval is retained, but an active change request cannot remain
		// the current approvable review. Fresh analysis and the same gate are owed.
		if e = bugRetireCurrentAnalysisReviewTx(ctx, tx, session); e != nil {
			return e
		}
		return bugBumpRevisionTx(ctx, tx, session)
	})
}

// bugJSONString is a tiny local helper avoiding an extra import for callers
// that only need a bounded, safely-quoted reason string as event content.
func bugJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
