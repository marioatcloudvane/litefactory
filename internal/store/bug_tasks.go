package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// BugIdentity is factory attribution. OwnerID is captured from operator
// configuration, never a field accepted from an analyst's result.
type BugIdentity struct {
	SessionID                     int64
	Repository                    string
	IssueID, IssueNumber, OwnerID int64
}

// BugExecution exposes the current analysis-owned state. The single optimistic
// revision on bug_tasks guards every domain write, not only analysis ones.
type BugExecution struct {
	Revision, CurrentAnalysis int64
	AnalysisUsed              int
}

// EnsureBugRecord creates the canonical identity once after sticky admission.
// Conflicting repository/issue/owner identities never overwrite that record.
func (d *DB) EnsureBugRecord(ctx context.Context, in BugIdentity) error {
	if in.SessionID <= 0 || in.IssueID <= 0 || in.IssueNumber <= 0 || in.OwnerID <= 0 {
		return errors.New("store: invalid Bug identity")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		a, err := readTaskWorkflowAdmission(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if a.Workflow != "bug" || a.Family != "standalone" {
			return errors.New("store: Bug record requires standalone admission")
		}
		var task string
		if err = tx.QueryRowContext(ctx, `SELECT task_ref FROM sessions WHERE id=?`, in.SessionID).Scan(&task); err != nil {
			return err
		}
		ref, err := naming.ParseRef(task)
		if err != nil {
			return err
		}
		if ref.String() != fmt.Sprintf("github:%s#%d", in.Repository, in.IssueNumber) {
			return errors.New("store: Bug issue differs from admitted task")
		}
		old, err := bugIdentityTx(ctx, tx, in.SessionID)
		if err == nil {
			if old != in {
				return errors.New("store: immutable Bug identity conflict")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bug_tasks(session_id,repository,issue_id,issue_number,owner_id,revision) VALUES(?,?,?,?,?,1)`, in.SessionID, in.Repository, in.IssueID, in.IssueNumber, in.OwnerID)
		return err
	})
}

func bugIdentityTx(ctx context.Context, tx *sql.Tx, session int64) (BugIdentity, error) {
	var out BugIdentity
	err := tx.QueryRowContext(ctx, `SELECT session_id,repository,issue_id,issue_number,owner_id FROM bug_tasks WHERE session_id=?`, session).Scan(&out.SessionID, &out.Repository, &out.IssueID, &out.IssueNumber, &out.OwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

func bugExecutionTx(ctx context.Context, tx *sql.Tx, session int64) (BugExecution, error) {
	var out BugExecution
	err := tx.QueryRowContext(ctx, `SELECT revision,current_analysis,analysis_used FROM bug_tasks WHERE session_id=?`, session).Scan(&out.Revision, &out.CurrentAnalysis, &out.AnalysisUsed)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

// bugBumpRevisionTx bumps the single optimistic revision every state-changing
// bug write must perform, whatever table it touched.
func bugBumpRevisionTx(ctx context.Context, tx *sql.Tx, session int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE bug_tasks SET revision=revision+1 WHERE session_id=?`, session)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// ClaimBugWork serializes repair, saving and child/check work for one task.
// Ownership survives process failure until the owning domain confirms quiescence.
func (d *DB) ClaimBugWork(ctx context.Context, session int64, owner, kind string) (bool, error) {
	if session <= 0 || owner == "" || len(owner) > 256 {
		return false, errors.New("store: invalid Bug work owner")
	}
	switch kind {
	case "repair", "checks", "child", "save":
	default:
		return false, errors.New("store: invalid Bug work kind")
	}
	var claimed bool
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		claimed, err = claimBugWorkTx(ctx, tx, session, owner, kind)
		return err
	})
	return claimed, err
}

func claimBugWorkTx(ctx context.Context, tx *sql.Tx, session int64, owner, kind string) (bool, error) {
	if kind != "repair" {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_repairs WHERE session_id=? AND state IN ('reserved','dispatched')`, session).Scan(&active); err != nil {
			return false, err
		}
		if active != 0 {
			return false, nil
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bug_tasks SET guard_owner=?,guard_kind=? WHERE session_id=? AND guard_owner IS NULL`, owner, kind, session); err != nil {
		return false, err
	}
	var oldOwner, oldKind sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT guard_owner,guard_kind FROM bug_tasks WHERE session_id=?`, session).Scan(&oldOwner, &oldKind)
	return oldOwner.String == owner && oldKind.String == kind, err
}

// ReleaseBugWork requires exact ownership; callers must first confirm cleanup.
func (d *DB) ReleaseBugWork(ctx context.Context, session int64, owner string) error {
	result, err := d.sql.ExecContext(ctx, `UPDATE bug_tasks SET guard_owner=NULL,guard_kind=NULL WHERE session_id=? AND guard_owner=?`, session, owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("store: Bug work ownership mismatch")
	}
	return nil
}

// releaseBugWorkOwnerTx clears the guard for one exact owner if held, inside a
// caller-owned transaction. It is a no-op when the guard is absent or held by
// another owner (used where a step's own cleanup should not disturb foreign work).
func releaseBugWorkOwnerTx(ctx context.Context, tx *sql.Tx, session int64, owner string) error {
	_, err := tx.ExecContext(ctx, `UPDATE bug_tasks SET guard_owner=NULL,guard_kind=NULL WHERE session_id=? AND guard_owner=?`, session, owner)
	return err
}

// BugLifecycle is task-level disposition, independent from graph execution status.
type BugLifecycle struct {
	SessionID, ParentInstance, ParentDefinition, ChildDefinition, Revision int64
	Disposition, Node, Reference                                           string
	Cancelled                                                              bool
}

func bugLifecycleTx(ctx context.Context, tx *sql.Tx, session int64) (BugLifecycle, error) {
	var l BugLifecycle
	var parent, parentDef, childDef sql.NullInt64
	e := tx.QueryRowContext(ctx, `SELECT session_id,parent_instance,parent_definition,child_definition,revision,disposition,node,reference,cancelled FROM bug_tasks WHERE session_id=?`, session).Scan(&l.SessionID, &parent, &parentDef, &childDef, &l.Revision, &l.Disposition, &l.Node, &l.Reference, &l.Cancelled)
	if errors.Is(e, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	if e != nil {
		return l, e
	}
	if !parent.Valid {
		return l, ErrNotFound
	}
	l.ParentInstance, l.ParentDefinition, l.ChildDefinition = parent.Int64, parentDef.Int64, childDef.Int64
	return l, nil
}

// GetBugLifecycle retrieves the current parent binding and domain disposition.
func (d *DB) GetBugLifecycle(ctx context.Context, session int64) (BugLifecycle, error) {
	var l BugLifecycle
	e := d.withTx(ctx, func(tx *sql.Tx) error { var e error; l, e = bugLifecycleTx(ctx, tx, session); return e })
	return l, e
}

// EnsureBugLifecycle pins the parent and child definitions without replacing existing lineage.
func (d *DB) EnsureBugLifecycle(ctx context.Context, session, parent, childDefinition int64) (BugLifecycle, error) {
	var out BugLifecycle
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		i, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, parent))
		if e != nil {
			return e
		}
		if i.SessionID != session {
			return errors.New("bug lifecycle parent task mismatch")
		}
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_definitions WHERE id IN (?,?) AND workflow='bug' AND mode=?`, i.DefinitionID, childDefinition, standaloneGraphStorageKey).Scan(&count); e != nil {
			return e
		}
		if count != 2 {
			return errors.New("bug lifecycle requires distinct pinned parent and child definitions")
		}
		if _, e = bugIdentityTx(ctx, tx, session); e != nil {
			return e
		}
		old, e := bugLifecycleTx(ctx, tx, session)
		if e == nil {
			if old.ParentInstance != parent || old.ParentDefinition != i.DefinitionID || old.ChildDefinition != childDefinition {
				return errors.New("bug lifecycle cannot replace pinned envelope")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_tasks SET parent_instance=?,parent_definition=?,child_definition=? WHERE session_id=? AND parent_instance IS NULL`, parent, i.DefinitionID, childDefinition, session); e != nil {
			return e
		}
		out, e = bugLifecycleTx(ctx, tx, session)
		return e
	})
	return out, e
}

// RecordBugLifecycleDisposition parks distinct waits without changing domain authority.
func (d *DB) RecordBugLifecycleDisposition(ctx context.Context, session, instance int64, node, disposition, reference string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		return bugRecordLifecycleDispositionTx(ctx, tx, session, instance, node, disposition, reference)
	})
}

// bugRecordLifecycleDispositionTx is RecordBugLifecycleDisposition's
// transaction body, shared with DecideBugExhaustion's stop path so the
// decision and the terminal disposition it sets commit together.
func bugRecordLifecycleDispositionTx(ctx context.Context, tx *sql.Tx, session, instance int64, node, disposition, reference string) error {
	switch disposition {
	case "running", "question", "analysis-review", "delivery-review", "child", "pending-effect", "blocked", "exhausted", "cancelled", "owner-stopped":
	default:
		return errors.New("invalid Bug lifecycle disposition")
	}
	if node == "" || len(reference) > 8192 {
		return errors.New("invalid Bug lifecycle reference")
	}
	l, e := bugLifecycleTx(ctx, tx, session)
	if e != nil {
		return e
	}
	if l.ParentInstance != instance {
		return errors.New("bug lifecycle instance mismatch")
	}
	if l.Disposition == "completed" {
		return errors.New("completed Bug lifecycle cannot be rewritten")
	}
	if l.Cancelled && disposition == "running" {
		return errors.New("bug lifecycle cancelled")
	}
	_, e = tx.ExecContext(ctx, `UPDATE bug_tasks SET node=?,disposition=?,reference=?,revision=revision+1 WHERE session_id=?`, node, disposition, reference, session)
	return e
}

// CompleteBugLifecycle checks closure and all owed process/effect/report work.
func (d *DB) CompleteBugLifecycle(ctx context.Context, session int64) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		l, e := bugLifecycleTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if l.Cancelled {
			return errors.New("cancelled Bug lifecycle cannot complete")
		}
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if c.Outcome != "confirmed" || !c.Confirmed || c.Conflict {
			return errors.New("bug lifecycle requires confirmed issue closure")
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		var owed int
		e = tx.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM bug_events WHERE session_id=? AND kind='report' AND reported=0)+
  (SELECT count(*) FROM bug_tasks WHERE session_id=? AND guard_owner IS NOT NULL)+
  (SELECT count(*) FROM bug_repairs WHERE session_id=? AND test_outcome='pending')+
  (SELECT count(*) FROM bug_repairs WHERE session_id=? AND state IN ('reserved','dispatched','uncertain'))+
  (SELECT count(*) FROM bug_delivery WHERE session_id=? AND merge_outcome='pending')+
  (SELECT count(*) FROM bug_analyses WHERE session_id=? AND state IN ('reserved','dispatched','uncertain'))+
  (SELECT count(*) FROM bug_checks WHERE session_id=? AND cleanup=0)+
  (SELECT count(*) FROM report_deliveries WHERE session_id=? AND completed=0)`, session, session, session, session, session, session, session, session).Scan(&owed)
		if e != nil {
			return e
		}
		if owed != 0 {
			return errors.New("bug lifecycle still owes effects, cleanup or reports")
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_tasks SET disposition='completed',reference=?,revision=revision+1 WHERE session_id=?`, c.Operation, session)
		return e
	})
}

// BugLifecycleReport names one durable lifecycle report's provenance. Its exact
// posted body is no longer retained: only the delivery key, the source attempt
// and the reported flag are, as a bug_events row of kind "report". The generic
// report delivery path regenerates the body on a retry; delivery-key dedup on
// report_deliveries prevents a duplicate post.
type BugLifecycleReport struct {
	SessionID, AttemptID int64
	Event, Kind          string
	DeliveryKey          string
	Reported             bool
}

type bugReportEventContent struct {
	Kind        string `json:"kind"`
	DeliveryKey string `json:"delivery_key"`
}

// RetainBugLifecycleReport journals immutable report provenance (delivery key
// and source attempt), without the rendered body.
func (d *DB) RetainBugLifecycleReport(ctx context.Context, r BugLifecycleReport) error {
	if r.Event == "" || r.DeliveryKey == "" || len(r.Event) > 256 {
		return errors.New("invalid Bug lifecycle report")
	}
	if r.Kind != "failure" && r.Kind != "notice" && r.Kind != "question" && r.Kind != "pre-dispatch" {
		return errors.New("invalid Bug report source type")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := bugIdentityTx(ctx, tx, r.SessionID); e != nil {
			return e
		}
		if r.Kind == "pre-dispatch" {
			if r.AttemptID != 0 {
				return errors.New("pre-dispatch failure cannot invent an attempt")
			}
		} else {
			var status string
			var session int64
			e := tx.QueryRowContext(ctx, `SELECT a.status,i.session_id FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id WHERE a.id=?`, r.AttemptID).Scan(&status, &session)
			if e != nil {
				return e
			}
			want := GraphSucceeded
			if r.Kind == "failure" {
				want = GraphFailed
			}
			if status != want || session != r.SessionID {
				return errors.New("bug report source status or task mismatch")
			}
		}
		return bugEventJSONFullTx(ctx, tx, r.SessionID, "report", r.Event, r.AttemptID, r.DeliveryKey, bugReportEventContent{Kind: r.Kind, DeliveryKey: r.DeliveryKey})
	})
}

// ConfirmBugLifecycleReport confirms the exact completed publication journal.
func (d *DB) ConfirmBugLifecycleReport(ctx context.Context, session int64, event string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var key string
		var attempt sql.NullInt64
		if e := tx.QueryRowContext(ctx, `SELECT delivery_key,attempt_id FROM bug_events WHERE session_id=? AND kind='report' AND reference=?`, session, event).Scan(&key, &attempt); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return ErrNotFound
			}
			return e
		}
		var completed bool
		var comment, actualSession int64
		if e := tx.QueryRowContext(ctx, `SELECT completed,COALESCE(comment_id,0),session_id FROM report_deliveries WHERE delivery_key=?`, key).Scan(&completed, &comment, &actualSession); e != nil {
			return e
		}
		if !completed || comment <= 0 || actualSession != session {
			return errors.New("bug lifecycle report publication is unconfirmed")
		}
		_, e := tx.ExecContext(ctx, `UPDATE bug_events SET reported=1 WHERE session_id=? AND kind='report' AND reference=?`, session, event)
		return e
	})
}

// PendingBugLifecycleReports includes confirmed transport whose lifecycle
// acknowledgment was interrupted. Reading it never turns an acknowledgment into
// completion; the report guard is checked again when the factory settles it.
func (d *DB) PendingBugLifecycleReports(ctx context.Context) ([]BugLifecycleReport, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT session_id,COALESCE(attempt_id,0),reference,content,delivery_key,reported FROM bug_events WHERE kind='report' AND reported=0 AND session_id NOT IN (SELECT session_id FROM doctor_held_session_owners) ORDER BY session_id,reference`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugLifecycleReport
	for rows.Next() {
		var r BugLifecycleReport
		var content []byte
		var key sql.NullString
		if err = rows.Scan(&r.SessionID, &r.AttemptID, &r.Event, &content, &key, &r.Reported); err != nil {
			return nil, err
		}
		var c bugReportEventContent
		if err = json.Unmarshal(content, &c); err != nil {
			return nil, err
		}
		r.Kind, r.DeliveryKey = c.Kind, key.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// BugParentContinuation names a factory-retained cause, never an arbitrary node.
type BugParentContinuation struct {
	SessionID                   int64
	Operation, Cause, Reference string
	ExpectedRevision            int64
	At                          time.Time
}

func bugContinuationEntryTx(ctx context.Context, tx *sql.Tx, l BugLifecycle, c BugParentContinuation) (string, error) {
	var wait GraphOwnerWait
	ownerCause := c.Cause == "analysis-approved" || c.Cause == "analysis-answers" || c.Cause == "analysis-change" || c.Cause == "delivery-approved" || c.Cause == "delivery-feedback"
	if ownerCause {
		var e error
		wait, e = scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? ORDER BY source_attempt_id DESC LIMIT 1`, l.ParentInstance))
		if e != nil {
			return "", e
		}
		identity, e := bugIdentityTx(ctx, tx, c.SessionID)
		if e != nil {
			return "", e
		}
		if wait.State != "allocated" || wait.ReplyID <= 0 || wait.ReplyAuthorID != identity.OwnerID {
			return "", errors.New("bug continuation lacks exact authenticated consumed wait")
		}
	}
	switch c.Cause {
	case "analysis-answers", "analysis-change":
		if c.Reference != strconv.FormatInt(wait.ReplyID, 10) {
			return "", errors.New("bug analysis continuation reply mismatch")
		}
		var count int
		var e error
		if c.Cause == "analysis-answers" {
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='question_reply' AND reference=?`, c.SessionID, fmt.Sprintf("question-reply:%s:%d", wait.DeliveryKey, wait.ReplyID)).Scan(&count)
		} else {
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind='review_change' AND reference=?`, c.SessionID, fmt.Sprint(wait.ReplyID)).Scan(&count)
		}
		if e != nil {
			return "", e
		}
		if count != 1 {
			return "", errors.New("bug analysis continuation missing retained owner context")
		}
		return "analyst", nil
	case "analysis-approved":
		var count int
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analyses WHERE session_id=? AND review=? AND approval_reply_id=? AND review_delivery_key=? AND review_active=1 AND revision=(SELECT current_analysis FROM bug_tasks WHERE session_id=?)`, c.SessionID, c.Reference, wait.ReplyID, wait.DeliveryKey, c.SessionID).Scan(&count)
		if e != nil {
			return "", e
		}
		if count != 1 {
			return "", errors.New("bug continuation lacks exact analysis approval")
		}
		return "record", nil
	case "delivery-approved":
		review, decision, e := approvedBugDeliveryTx(ctx, tx, c.SessionID, c.Reference)
		if e != nil {
			return "", e
		}
		if review.DeliveryKey != wait.DeliveryKey || decision.CommentID != wait.ReplyID {
			return "", errors.New("bug delivery continuation reply mismatch")
		}
		return "merge", nil
	case "delivery-feedback":
		id, e := strconv.ParseInt(c.Reference, 10, 64)
		if e != nil {
			return "", e
		}
		if id != wait.ReplyID {
			return "", errors.New("bug feedback continuation reply mismatch")
		}
		kind, e := bugDeliveryReplyKindTx(ctx, tx, c.SessionID, id)
		if e != nil {
			return "", e
		}
		switch kind {
		case "repair":
			return "attempt", nil
		case "reshape":
			return "analyst", nil
		case "clarify", "discussion":
			return "pr", nil
		}
		return "", errors.New("unsupported Bug feedback continuation")
	case "test-resolved":
		call, e := strconv.ParseInt(c.Reference, 10, 64)
		if e != nil {
			return "", e
		}
		if e = bugTestResolutionGuardTx(ctx, tx, c.SessionID, call); e != nil {
			return "", e
		}
		content, e := bugEventByReferenceTx(ctx, tx, c.SessionID, "test_resolution", c.Reference)
		if e != nil {
			return "", e
		}
		var resolution bugTestResolutionContent
		if e = json.Unmarshal(content, &resolution); e != nil {
			return "", e
		}
		if c.Operation != fmt.Sprintf("owner-reply:%d", resolution.CommentID) {
			return "", errors.New("bug test continuation lacks exact retained resolution")
		}
		return "test", nil
	case "failure-retry":
		// An authenticated owner comment retries a parent that failed in an
		// agent node, or in recording the analyst's output (whose cause is that
		// output, so the analyst runs again). Effect nodes are never retried this
		// way: their retained intents own recovery, and a blind retry could repeat
		// a remote effect.
		comment, e := strconv.ParseInt(c.Reference, 10, 64)
		if e != nil || comment <= 0 || c.Operation != fmt.Sprintf("owner-reply:%d", comment) {
			return "", errors.New("bug failure retry requires the owner comment that asked for it")
		}
		prior, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, l.ParentInstance))
		if e != nil {
			return "", e
		}
		// Recording the repair is factory work over the fixer's retained
		// result, so it is retried in place and runs no agent.
		entry := map[string]string{"analyst": "analyst", "record_analysis": "analyst", "fixer": "fixer", "record_repair": "record_repair"}[prior.CurrentNode]
		if prior.Status == GraphFailed && entry != "" {
			return entry, nil
		}
		// A parent that reported the analyst's own stop (blocked analysis or a
		// failed analyst outcome) and ended has no owner wait either; the owner
		// comment runs the analyst again. Every other reported stop stays final.
		if prior.Status == GraphSucceeded && l.Disposition == "blocked" {
			var from, on string
			e = tx.QueryRowContext(ctx, `SELECT from_node,on_outcome FROM graph_transitions WHERE instance_id=? AND to_node='report' ORDER BY id DESC LIMIT 1`, prior.ID).Scan(&from, &on)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return "", e
			}
			if (from == "record_analysis" && on == "blocked") || (from == "analyst" && on == "failed") {
				return "analyst", nil
			}
		}
		return "", errors.New("only a Bug parent that failed in its analyst or fixer, or stopped on a blocked analysis, can be retried")
	case "budget-grant":
		// An exhaustion's grant decision buys exactly one attempt: the decision
		// must exist, be a grant, and no repair row may already carry this exact
		// grant reference (AC-006).
		content, e := bugEventByReferenceTx(ctx, tx, c.SessionID, "exhaustion_decision", c.Reference)
		if e != nil {
			return "", e
		}
		var decision bugExhaustionDecisionContent
		if e = json.Unmarshal(content, &decision); e != nil {
			return "", e
		}
		if decision.Decision != "grant" {
			return "", errors.New("bug budget grant continuation requires a grant decision")
		}
		var used int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_repairs WHERE session_id=? AND grant_reference=?`, c.SessionID, c.Reference).Scan(&used); e != nil {
			return "", e
		}
		if used != 0 {
			return "", errors.New("bug budget grant already spent")
		}
		return "attempt", nil
	case "merge-reconcile":
		m, e := bugMergeTx(ctx, tx, c.SessionID, c.Reference)
		if e != nil {
			return "", e
		}
		if !m.Dispatched && m.Outcome != "pending" {
			return "", errors.New("bug merge has no owed reconciliation")
		}
		return "merge", nil
	case "close-reconcile":
		v, e := bugClosureTx(ctx, tx, c.SessionID)
		if e != nil {
			return "", e
		}
		if v.Operation != c.Reference || v.Conflict {
			return "", errors.New("bug closure continuation mismatch")
		}
		return "close", nil
	default:
		return "", fmt.Errorf("unsupported Bug continuation cause %q", c.Cause)
	}
}

// BugContinuationPrior is the instance a continuation succeeded, or ErrNotFound
// when the instance was not created by one.
func (d *DB) BugContinuationPrior(ctx context.Context, session, successor int64) (int64, error) {
	content, err := bugEventByReferenceContinuationTx(ctx, d, session, successor)
	if err != nil {
		return 0, err
	}
	return content.PriorInstance, nil
}

type bugContinuationEventContent struct {
	Cause, Reference string
	PriorInstance    int64 `json:"prior_instance"`
	Successor        int64
	Entry            string
}

func bugEventByReferenceContinuationTx(ctx context.Context, d *DB, session, successor int64) (bugContinuationEventContent, error) {
	var out bugContinuationEventContent
	rows, err := d.sql.QueryContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='continuation'`, session)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var content []byte
		if err = rows.Scan(&content); err != nil {
			return out, err
		}
		var c bugContinuationEventContent
		if err = json.Unmarshal(content, &c); err != nil {
			return out, err
		}
		if c.Successor == successor {
			return c, nil
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, ErrNotFound
}

// BugRetryComment is the owner comment that restarted this parent instance
// after a failure or blocked analysis, or ErrNotFound when another cause did.
func (d *DB) BugRetryComment(ctx context.Context, session, instance int64) (int64, error) {
	c, err := bugEventByReferenceContinuationTx(ctx, d, session, instance)
	if err != nil {
		return 0, err
	}
	if c.Cause != "failure-retry" {
		return 0, ErrNotFound
	}
	return strconv.ParseInt(c.Reference, 10, 64)
}

// ClaimBugContinuation atomically consumes a retained cause and creates one
// successor+dispatch. Terminal history, definitions and lifetime budgets survive.
func (d *DB) ClaimBugContinuation(ctx context.Context, c BugParentContinuation) (GraphInstance, error) {
	if c.Operation == "" || len(c.Operation) > 128 || c.Reference == "" || c.At.IsZero() {
		return GraphInstance{}, errors.New("invalid Bug parent continuation")
	}
	var out GraphInstance
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = claimBugContinuationTx(ctx, tx, c)
		return e
	})
	return out, e
}

// claimBugContinuationTx is ClaimBugContinuation's transaction body, shared
// with DecideBugExhaustion so a grant decision and its continuation commit or
// fail together (AC-012).
func claimBugContinuationTx(ctx context.Context, tx *sql.Tx, c BugParentContinuation) (GraphInstance, error) {
	var out GraphInstance
	existing, e := bugEventByReferenceTx(ctx, tx, c.SessionID, "continuation", c.Operation)
	if e == nil {
		var old bugContinuationEventContent
		if e = json.Unmarshal(existing, &old); e != nil {
			return out, e
		}
		if old.Cause != c.Cause || old.Reference != c.Reference {
			return out, errors.New("bug continuation replay conflict")
		}
		return scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, old.Successor))
	}
	if !errors.Is(e, ErrNotFound) {
		return out, e
	}
	l, e := bugLifecycleTx(ctx, tx, c.SessionID)
	if e != nil {
		return out, e
	}
	if l.Revision != c.ExpectedRevision {
		return out, ErrBugRevisionConflict
	}
	if l.Disposition == "completed" || l.Cancelled {
		return out, errors.New("bug lifecycle does not admit fresh work")
	}
	prior, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, l.ParentInstance))
	if e != nil {
		return out, e
	}

	entry, e := bugContinuationEntryTx(ctx, tx, l, c)
	if e != nil {
		return out, e
	}
	if prior.Status == GraphRunning {
		switch c.Cause {
		case "analysis-approved", "analysis-answers", "analysis-change", "delivery-approved", "delivery-feedback":
		default:
			return out, errors.New("active Bug operation must resume its existing envelope")
		}
		var active int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts WHERE instance_id=? AND status='running'`, prior.ID).Scan(&active); e != nil {
			return out, e
		}
		if active != 0 {
			return out, errors.New("bug parent still has active work")
		}
		if e = setGraphInstanceTx(ctx, tx, prior.ID, prior.CurrentNode, GraphSucceeded, time.Time{}, c.At); e != nil {
			return out, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(c.At), prior.ID); e != nil {
			return out, e
		}
	}
	out, e = scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,next_due_at,started_at) VALUES(?,?,'running',?,?,?) RETURNING `+graphInstanceColumns, c.SessionID, l.ParentDefinition, entry, FormatTime(c.At), FormatTime(c.At)))
	if e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,created_at) VALUES(?,?,?,?,?)`, out.ID, c.SessionID, entry, FormatTime(c.At), FormatTime(c.At)); e != nil {
		return out, e
	}
	if e = bugEventJSONTx(ctx, tx, c.SessionID, "continuation", c.Operation, bugContinuationEventContent{Cause: c.Cause, Reference: c.Reference, PriorInstance: l.ParentInstance, Successor: out.ID, Entry: entry}); e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE bug_tasks SET parent_instance=?,node=?,disposition='running',reference=?,revision=revision+1 WHERE session_id=?`, out.ID, entry, c.Reference, c.SessionID)
	return out, e
}

// CancelBugLifecycle persists cancellation before suppressing new domain claims.
// Active child commands observe their own cancellation flags; no cleanup is inferred.
func (d *DB) CancelBugLifecycle(ctx context.Context, session int64) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := bugLifecycleTx(ctx, tx, session); e != nil {
			return e
		}
		for _, query := range []string{
			`UPDATE bug_tasks SET cancelled=1,disposition='cancelled',revision=revision+1 WHERE session_id=? AND cancelled=0`,
			`UPDATE bug_delivery SET pr_cancelled=1 WHERE session_id=?`,
			`UPDATE bug_delivery SET review_active=0,review_reason='cancelled' WHERE session_id=? AND review_active=1`,
			`UPDATE bug_repairs SET test_cancelled=1 WHERE session_id=? AND test_consumed=0`,
			`UPDATE bug_checks SET cancelled=1 WHERE session_id=? AND cleanup=0`,
		} {
			if _, e := tx.ExecContext(ctx, query, session); e != nil {
				return e
			}
		}
		return nil
	})
}

// GetBugOwnerContinuation recovers a consumed exact owner reply even after the
// lifecycle advances. Changed text or author cannot borrow its earlier dispatch.
func (d *DB) GetBugOwnerContinuation(ctx context.Context, session, comment, author int64, body string) (GraphInstance, error) {
	var out GraphInstance
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		identity, e := bugIdentityTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if identity.OwnerID != author || comment <= 0 {
			return errors.New("bug continuation owner mismatch")
		}
		operation := fmt.Sprintf("owner-reply:%d", comment)
		existing, e := bugEventByReferenceTx(ctx, tx, session, "continuation", operation)
		if errors.Is(e, ErrNotFound) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		var c bugContinuationEventContent
		if e = json.Unmarshal(existing, &c); e != nil {
			return e
		}
		var actualBody string
		var actualAuthor, actualComment int64
		switch c.Cause {
		case "analysis-answers", "analysis-change", "analysis-approved", "delivery-approved", "delivery-feedback":
			e = tx.QueryRowContext(ctx, `SELECT reply_body,reply_author_id,reply_id FROM graph_owner_waits WHERE instance_id=? AND state='allocated' AND reply_id=?`, c.PriorInstance, comment).Scan(&actualBody, &actualAuthor, &actualComment)
		case "test-resolved":
			var content []byte
			content, e = bugEventByReferenceTx(ctx, tx, session, "test_resolution", c.Reference)
			if e == nil {
				var resolution bugTestResolutionContent
				if e = json.Unmarshal(content, &resolution); e == nil {
					if resolution.CommentID != comment {
						e = ErrNotFound
					} else {
						actualBody, actualAuthor, actualComment = resolution.Body, resolution.AuthorID, resolution.CommentID
					}
				}
			}
		case "failure-retry":
			actualBody, actualAuthor, actualComment = body, author, comment
		case "budget-grant":
			var content []byte
			content, e = bugEventByReferenceTx(ctx, tx, session, "exhaustion_decision", c.Reference)
			if e == nil {
				var decision bugExhaustionDecisionContent
				if e = json.Unmarshal(content, &decision); e == nil {
					if decision.Decision != "grant" || decision.CommentID != comment {
						e = ErrNotFound
					} else {
						actualBody, actualAuthor, actualComment = body, decision.AuthorID, decision.CommentID
					}
				}
			}
		default:
			e = ErrNotFound
		}
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if actualBody != body || actualAuthor != author || actualComment != comment {
			return errors.New("bug continuation reply replay mismatch")
		}
		out, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, c.Successor))
		return e
	})
	return out, e
}

// bugFactoryRecoveryNode names every graph node CurrentBugFactoryAttempt and
// ReconcileBugParentGraphs are willing to reconcile.
func bugFactoryRecoveryNode(node string) bool {
	switch node {
	case "record", "record_analysis", "analysis_needed", "analysis_done", "analysis_questions", "analysis_review", "attempt", "record_repair", "fixer_reshape", "test", "pr", "delivery_review", "merge", "close", "report":
		return true
	default:
		return false
	}
}

// CurrentBugFactoryAttempt exposes only the exact unfinished factory operation.
// A missing agent run binding is never evidence that an agent was undispatched.
func (d *DB) CurrentBugFactoryAttempt(ctx context.Context, instance int64, node string) (GraphNodeAttempt, error) {
	var out GraphNodeAttempt
	if !bugFactoryRecoveryNode(node) {
		return out, ErrNotFound
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_tasks t JOIN graph_instances i ON i.id=t.parent_instance JOIN graph_definitions g ON g.id=i.definition_id WHERE i.id=? AND i.current_node=? AND i.status='running' AND g.workflow='bug'`, instance, node).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrNotFound
		}
		var e error
		out, e = scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? AND node=? AND status IN ('pending','running') AND run_id IS NULL ORDER BY id DESC LIMIT 1`, instance, node))
		return e
	})
	return out, err
}

// ReconcileBugParentGraphs runs once at startup after child reconciliation.
// Factory effects retain their original attempt and dispatch identities; their
// owning activities must reconcile journals before attempting any external write.
// Interrupted agent work remains spent and cannot be blindly launched again.
func (d *DB) ReconcileBugParentGraphs(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		return errors.New("bug parent recovery requires observation time")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT a.id,a.instance_id,a.node,COALESCE(a.run_id,0),i.session_id,a.started_at FROM bug_tasks t JOIN graph_instances i ON i.id=t.parent_instance JOIN graph_definitions g ON g.id=i.definition_id JOIN graph_node_attempts a ON a.instance_id=i.id AND a.node=i.current_node WHERE g.workflow='bug' AND i.session_id NOT IN (SELECT session_id FROM doctor_held_session_owners) AND i.status='running' AND a.status IN ('pending','running') AND NOT EXISTS(SELECT 1 FROM bug_repairs c WHERE c.test_parent_attempt=a.id AND c.test_consumed=0) ORDER BY a.id`)
		if e != nil {
			return e
		}
		type pending struct {
			id, instance, run, session int64
			node, started              string
		}
		var work []pending
		for rows.Next() {
			var p pending
			if e = rows.Scan(&p.id, &p.instance, &p.node, &p.run, &p.session, &p.started); e != nil {
				break
			}
			work = append(work, p)
		}
		if e = errors.Join(e, rows.Err(), rows.Close()); e != nil {
			return e
		}
		for _, p := range work {
			stamp := FormatTime(at)
			if stamp < p.started {
				stamp = p.started
			}
			if p.run == 0 && bugFactoryRecoveryNode(p.node) {
				var dispatchID, allocated int64
				var dispatchNode string
				e = tx.QueryRowContext(ctx, `SELECT id,node,COALESCE(allocated_run_id,0) FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running')`, p.instance).Scan(&dispatchID, &dispatchNode, &allocated)
				switch {
				case e == nil:
					if allocated != 0 || dispatchNode != p.node {
						return errors.New("bug factory recovery dispatch binding mismatch")
					}
					if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='pending',reserved_at=NULL,due_at=? WHERE id=?`, stamp, dispatchID); e != nil {
						return e
					}
				case errors.Is(e, sql.ErrNoRows):
					if _, e = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,created_at) VALUES(?,?,?,?,?)`, p.instance, p.session, p.node, stamp, stamp); e != nil {
						return e
					}
				default:
					return e
				}
				continue
			}
			reason := "Bug agent execution interrupted before graph completion; retained work and reservations require reconciliation, not relaunch"
			if p.run == 0 && p.node != "analyst" && p.node != "fixer" {
				reason = "Unknown Bug node interrupted; factory recovery refused"
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='failed',outcome=NULL,finished_at=? WHERE id=?`, stamp, p.id); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'diagnostic',?,?)`, p.id, []byte(reason), stamp); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',next_due_at=NULL,finished_at=? WHERE id=?`, stamp, p.instance); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled',finished_at=?,cancellation_reason=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, stamp, reason, p.instance); e != nil {
				return e
			}
			event := fmt.Sprintf("restart-attempt:%d", p.id)
			restartKey := fmt.Sprintf("bug-restart:%d:%d", p.session, p.id)
			if e = bugEventJSONFullTx(ctx, tx, p.session, "report", event, p.id, restartKey, bugReportEventContent{Kind: "failure", DeliveryKey: restartKey}); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE bug_tasks SET disposition='blocked',reference=?,revision=revision+1 WHERE session_id=?`, event, p.session); e != nil {
				return e
			}
		}
		return nil
	})
}

// BindBugRestartReport binds the one factory-rendered projection to a restart
// diagnostic. It cannot change an already bound or published report.
func (d *DB) BindBugRestartReport(ctx context.Context, session int64, event, key, body string) error {
	if key == "" || body == "" || len(body) > 65536 {
		return errors.New("invalid Bug restart report projection")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var oldKey string
		var attempt sql.NullInt64
		var reported bool
		var content []byte
		if e := tx.QueryRowContext(ctx, `SELECT attempt_id,delivery_key,reported,content FROM bug_events WHERE session_id=? AND kind='report' AND reference=?`, session, event).Scan(&attempt, &oldKey, &reported, &content); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return ErrNotFound
			}
			return e
		}
		var c bugReportEventContent
		if e := json.Unmarshal(content, &c); e != nil {
			return e
		}
		if c.Kind != "failure" || !attempt.Valid || attempt.Int64 <= 0 || event != fmt.Sprintf("restart-attempt:%d", attempt.Int64) {
			return errors.New("bug report is not a restart diagnostic")
		}
		if oldKey == key {
			return nil
		}
		if reported || oldKey != fmt.Sprintf("bug-restart:%d:%d", session, attempt.Int64) {
			return errors.New("bug restart projection already bound")
		}
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT a.status FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id WHERE a.id=? AND i.session_id=?`, attempt.Int64, session).Scan(&status); e != nil {
			return e
		}
		if status != GraphFailed {
			return errors.New("bug restart projection lacks failed source")
		}
		content, e := json.Marshal(bugReportEventContent{Kind: c.Kind, DeliveryKey: key})
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_events SET delivery_key=?,content=? WHERE session_id=? AND kind='report' AND reference=? AND reported=0`, key, content, session, event)
		return e
	})
}

// GetBugDeliveryReviewForWait retains inactive history for reply recovery; it
// grants no current approval or repair authority.
func (d *DB) GetBugDeliveryReviewForWait(ctx context.Context, session int64, key string) (BugDeliveryReview, error) {
	var out BugDeliveryReview
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugDeliveryReviewByDeliveryKeyTx(ctx, tx, session, key)
		return e
	})
	return out, e
}

// GetBugAnalysisReviewForWait returns retained presentation history for replay.
// Callers must still consume the authenticated reply through its typed operation.
func (d *DB) GetBugAnalysisReviewForWait(ctx context.Context, session int64, key string) (BugAnalysisReview, error) {
	var out BugAnalysisReview
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugAnalysisReviewByDeliveryKeyTx(ctx, tx, session, key)
		return e
	})
	return out, e
}

// PendingBugRemoteContinuations selects observation-only recovery checkpoints.
// ClaimBugContinuation rechecks each domain operation and the lifecycle revision.
func (d *DB) PendingBugRemoteContinuations(ctx context.Context) ([]BugParentContinuation, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT t.session_id,t.parent_instance,t.revision,CASE WHEN c.close_operation IS NOT NULL THEN 'close-reconcile' ELSE 'merge-reconcile' END,COALESCE(c.close_operation,c.merge_operation) FROM bug_tasks t JOIN graph_instances i ON i.id=t.parent_instance JOIN bug_delivery c ON c.session_id=t.session_id WHERE t.cancelled=0 AND t.session_id NOT IN (SELECT session_id FROM doctor_held_session_owners) AND i.status IN ('succeeded','failed') AND ((c.close_outcome='pending' AND c.close_dispatched=1 AND c.close_conflict=0) OR (c.merge_outcome='pending' AND c.merge_dispatched=1 AND c.merge_exception=''))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugParentContinuation
	for rows.Next() {
		var v BugParentContinuation
		var parent int64
		if err = rows.Scan(&v.SessionID, &parent, &v.ExpectedRevision, &v.Cause, &v.Reference); err != nil {
			return nil, err
		}
		v.Operation = fmt.Sprintf("remote-reconcile:%d:%d", v.SessionID, parent)
		out = append(out, v)
	}
	return out, rows.Err()
}
