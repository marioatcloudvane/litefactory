package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// BugLifecycle is task-level disposition, independent from graph execution status.
type BugLifecycle struct {
	SessionID, ParentInstance, ParentDefinition, ChildDefinition, Revision int64
	Disposition, Node, Reference                                           string
	Cancelled                                                              bool
}

func bugLifecycleTx(ctx context.Context, tx *sql.Tx, session int64) (BugLifecycle, error) {
	var l BugLifecycle
	e := tx.QueryRowContext(ctx, `SELECT session_id,parent_instance,parent_definition,child_definition,revision,disposition,node,reference,cancelled FROM bug_lifecycles WHERE session_id=?`, session).Scan(&l.SessionID, &l.ParentInstance, &l.ParentDefinition, &l.ChildDefinition, &l.Revision, &l.Disposition, &l.Node, &l.Reference, &l.Cancelled)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return l, e
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
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_lifecycles(session_id,parent_instance,parent_definition,child_definition) VALUES(?,?,?,?)`, session, parent, i.DefinitionID, childDefinition); e != nil {
			return e
		}
		out, e = bugLifecycleTx(ctx, tx, session)
		return e
	})
	return out, e
}

// RecordBugLifecycleDisposition parks distinct waits without changing domain authority.
func (d *DB) RecordBugLifecycleDisposition(ctx context.Context, session, instance int64, node, disposition, reference string) error {
	switch disposition {
	case "running", "question", "analysis-review", "delivery-review", "child", "pending-effect", "blocked", "exhausted", "cancelled":
	default:
		return errors.New("invalid Bug lifecycle disposition")
	}
	if node == "" || len(reference) > 8192 {
		return errors.New("invalid Bug lifecycle reference")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
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
		_, e = tx.ExecContext(ctx, `UPDATE bug_lifecycles SET node=?,disposition=?,reference=?,revision=revision+1 WHERE session_id=?`, node, disposition, reference, session)
		return e
	})
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
  (SELECT count(*) FROM bug_lifecycle_events WHERE session_id=? AND reported=0)+
  (SELECT count(*) FROM bug_work_guards WHERE session_id=?)+
  (SELECT count(*) FROM bug_test_calls WHERE session_id=? AND outcome='pending')+
  (SELECT count(*) FROM bug_repairs WHERE session_id=? AND state IN ('reserved','dispatched','uncertain'))+
  (SELECT count(*) FROM bug_merges WHERE session_id=? AND outcome='pending')+
  (SELECT count(*) FROM bug_analysis_passes WHERE session_id=? AND state IN ('reserved','dispatched','uncertain'))+
  (SELECT count(*) FROM bug_check_phases WHERE session_id=? AND cleanup=0)+
  (SELECT count(*) FROM report_deliveries WHERE session_id=? AND completed=0)`, session, session, session, session, session, session, session, session).Scan(&owed)
		if e != nil {
			return e
		}
		if owed != 0 {
			return errors.New("bug lifecycle still owes effects, cleanup or reports")
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_lifecycles SET disposition='completed',reference=?,revision=revision+1 WHERE session_id=?`, c.Operation, session)
		return e
	})
}

// BugLifecycleReport retains failure provenance independently from report delivery.
type BugLifecycleReport struct {
	SessionID, AttemptID           int64
	Event, Kind, Body, DeliveryKey string
	Reported                       bool
}

// RetainBugLifecycleReport journals immutable report content with its actual source provenance.
func (d *DB) RetainBugLifecycleReport(ctx context.Context, r BugLifecycleReport) error {
	if r.Event == "" || r.Body == "" || r.DeliveryKey == "" || len(r.Body) > 65536 || len(r.Event) > 256 {
		return errors.New("invalid Bug lifecycle report")
	}
	if r.Kind != "failure" && r.Kind != "notice" && r.Kind != "pre-dispatch" {
		return errors.New("invalid Bug report source type")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var oldKind, oldBody, oldKey string
		var oldAttempt int64
		e := tx.QueryRowContext(ctx, `SELECT kind,body,delivery_key,COALESCE(attempt_id,0) FROM bug_lifecycle_events WHERE session_id=? AND event=?`, r.SessionID, r.Event).Scan(&oldKind, &oldBody, &oldKey, &oldAttempt)
		if e == nil {
			if oldKind != r.Kind || oldBody != r.Body || oldKey != r.DeliveryKey || oldAttempt != r.AttemptID {
				return errors.New("bug report identity conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, e = bugIdentityTx(ctx, tx, r.SessionID); e != nil {
			return e
		}
		if r.Kind == "pre-dispatch" {
			if r.AttemptID != 0 {
				return errors.New("pre-dispatch failure cannot invent an attempt")
			}
		} else {
			var status string
			var session int64
			e = tx.QueryRowContext(ctx, `SELECT a.status,i.session_id FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id WHERE a.id=?`, r.AttemptID).Scan(&status, &session)
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
		_, e = tx.ExecContext(ctx, `INSERT INTO bug_lifecycle_events(session_id,event,kind,attempt_id,body,delivery_key) VALUES(?,?,?,?,?,?)`, r.SessionID, r.Event, r.Kind, nullID(r.AttemptID), r.Body, r.DeliveryKey)
		return e
	})
}

// ConfirmBugLifecycleReport confirms the exact completed publication journal.
func (d *DB) ConfirmBugLifecycleReport(ctx context.Context, session int64, event string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var key, expectedBody string
		if e := tx.QueryRowContext(ctx, `SELECT delivery_key,body FROM bug_lifecycle_events WHERE session_id=? AND event=?`, session, event).Scan(&key, &expectedBody); e != nil {
			return e
		}
		var completed bool
		var comment, actualSession int64
		var actualBody string
		if e := tx.QueryRowContext(ctx, `SELECT completed,COALESCE(comment_id,0),session_id,body FROM report_deliveries WHERE delivery_key=?`, key).Scan(&completed, &comment, &actualSession, &actualBody); e != nil {
			return e
		}
		if !completed || comment <= 0 || actualSession != session || actualBody != expectedBody {
			return errors.New("bug lifecycle report publication is unconfirmed")
		}
		_, e := tx.ExecContext(ctx, `UPDATE bug_lifecycle_events SET reported=1 WHERE session_id=? AND event=?`, session, event)
		return e
	})
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
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analysis_events WHERE session_id=? AND kind='question_reply' AND reference=?`, c.SessionID, fmt.Sprintf("question-reply:%s:%d", wait.DeliveryKey, wait.ReplyID)).Scan(&count)
		} else {
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analysis_review_changes WHERE session_id=? AND comment_id=? AND author_id=? AND body=?`, c.SessionID, wait.ReplyID, wait.ReplyAuthorID, wait.ReplyBody).Scan(&count)
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
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analysis_approvals a JOIN bug_execution x ON x.session_id=a.session_id AND x.current_analysis=a.analysis_revision JOIN bug_analysis_reviews r ON r.session_id=a.session_id AND r.review=a.review AND r.active=1 WHERE a.session_id=? AND a.review=? AND a.reply_id=? AND r.delivery_key=?`, c.SessionID, c.Reference, wait.ReplyID, wait.DeliveryKey).Scan(&count)
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
		var kind string
		if e = tx.QueryRowContext(ctx, `SELECT kind FROM bug_delivery_replies WHERE session_id=? AND comment_id=?`, c.SessionID, id).Scan(&kind); e != nil {
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
		var comment int64
		if e = tx.QueryRowContext(ctx, `SELECT comment_id FROM bug_test_resolutions WHERE session_id=? AND call_id=?`, c.SessionID, call).Scan(&comment); e != nil {
			return "", e
		}
		if c.Operation != fmt.Sprintf("owner-reply:%d", comment) {
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
		entry := map[string]string{"analyst": "analyst", "record_analysis": "analyst", "fixer": "fixer"}[prior.CurrentNode]
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

// BugRetryComment is the owner comment that restarted this parent instance
// after a failure or blocked analysis, or ErrNotFound when another cause did.
func (d *DB) BugRetryComment(ctx context.Context, session, instance int64) (int64, error) {
	var ref string
	err := d.sql.QueryRowContext(ctx, `SELECT reference FROM bug_parent_continuations WHERE session_id=? AND successor=? AND cause='failure-retry'`, session, instance).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(ref, 10, 64)
}

// ClaimBugContinuation atomically consumes a retained cause and creates one
// successor+dispatch. Terminal history, definitions and lifetime budgets survive.
func (d *DB) ClaimBugContinuation(ctx context.Context, c BugParentContinuation) (GraphInstance, error) {
	if c.Operation == "" || len(c.Operation) > 128 || c.Reference == "" || c.At.IsZero() {
		return GraphInstance{}, errors.New("invalid Bug parent continuation")
	}
	var out GraphInstance
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		var id int64
		var cause, ref string
		e := tx.QueryRowContext(ctx, `SELECT successor,cause,reference FROM bug_parent_continuations WHERE session_id=? AND operation=?`, c.SessionID, c.Operation).Scan(&id, &cause, &ref)
		if e == nil {
			if cause != c.Cause || ref != c.Reference {
				return errors.New("bug continuation replay conflict")
			}
			out, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, id))
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		l, e := bugLifecycleTx(ctx, tx, c.SessionID)
		if e != nil {
			return e
		}
		if l.Revision != c.ExpectedRevision {
			return ErrBugRevisionConflict
		}
		if l.Disposition == "completed" || l.Cancelled {
			return errors.New("bug lifecycle does not admit fresh work")
		}
		prior, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, l.ParentInstance))
		if e != nil {
			return e
		}

		entry, e := bugContinuationEntryTx(ctx, tx, l, c)
		if e != nil {
			return e
		}
		if prior.Status == GraphRunning {
			switch c.Cause {
			case "analysis-approved", "analysis-answers", "analysis-change", "delivery-approved", "delivery-feedback":
			default:
				return errors.New("active Bug operation must resume its existing envelope")
			}
			var active int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts WHERE instance_id=? AND status='running'`, prior.ID).Scan(&active); e != nil {
				return e
			}
			if active != 0 {
				return errors.New("bug parent still has active work")
			}
			if e = setGraphInstanceTx(ctx, tx, prior.ID, prior.CurrentNode, GraphSucceeded, time.Time{}, c.At); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(c.At), prior.ID); e != nil {
				return e
			}
		}
		out, e = scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,next_due_at,started_at) VALUES(?,?,'running',?,?,?) RETURNING `+graphInstanceColumns, c.SessionID, l.ParentDefinition, entry, FormatTime(c.At), FormatTime(c.At)))
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,created_at) VALUES(?,?,?,?,?)`, out.ID, c.SessionID, entry, FormatTime(c.At), FormatTime(c.At)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_parent_continuations(session_id,operation,cause,reference,prior_instance,successor,entry) VALUES(?,?,?,?,?,?,?)`, c.SessionID, c.Operation, c.Cause, c.Reference, l.ParentInstance, out.ID, entry); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_lifecycles SET parent_instance=?,node=?,disposition='running',reference=?,revision=revision+1 WHERE session_id=?`, out.ID, entry, c.Reference, c.SessionID)
		return e
	})
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
			`UPDATE bug_lifecycles SET cancelled=1,disposition='cancelled',revision=revision+1 WHERE session_id=? AND cancelled=0`,
			`UPDATE bug_pr_intents SET cancelled=1 WHERE session_id=?`,
			`UPDATE bug_delivery_reviews SET active=0,reason='cancelled' WHERE session_id=? AND active=1`,
			`UPDATE bug_test_calls SET cancelled=1 WHERE session_id=? AND consumed=0`,
			`UPDATE bug_check_phases SET cancelled=1 WHERE session_id=? AND cleanup=0`,
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
		var successor int64
		var actualBody string
		var actualAuthor, actualComment int64
		e = tx.QueryRowContext(ctx, `SELECT c.successor,w.reply_body,w.reply_author_id,w.reply_id FROM bug_parent_continuations c JOIN graph_owner_waits w ON w.instance_id=c.prior_instance WHERE c.session_id=? AND c.operation=? AND w.state='allocated' AND w.reply_id=?`, session, fmt.Sprintf("owner-reply:%d", comment), comment).Scan(&successor, &actualBody, &actualAuthor, &actualComment)
		if errors.Is(e, sql.ErrNoRows) {
			e = tx.QueryRowContext(ctx, `SELECT c.successor,r.body,r.author_id,r.comment_id FROM bug_parent_continuations c JOIN bug_test_resolutions r ON r.session_id=c.session_id AND CAST(r.call_id AS TEXT)=c.reference WHERE c.session_id=? AND c.operation=? AND c.cause='test-resolved' AND r.comment_id=?`, session, fmt.Sprintf("owner-reply:%d", comment), comment).Scan(&successor, &actualBody, &actualAuthor, &actualComment)
		}
		if errors.Is(e, sql.ErrNoRows) {
			// A failure retry has no wait or resolution row; the owner comment id
			// is its reference, and the owner was checked above.
			e = tx.QueryRowContext(ctx, `SELECT successor FROM bug_parent_continuations WHERE session_id=? AND operation=? AND cause='failure-retry' AND reference=?`, session, fmt.Sprintf("owner-reply:%d", comment), fmt.Sprint(comment)).Scan(&successor)
			if e == nil {
				actualBody, actualAuthor, actualComment = body, author, comment
			}
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
		out, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, successor))
		return e
	})
	return out, e
}

// GetBugDeliveryReviewForWait retains inactive history for reply recovery; it
// grants no current approval or repair authority.
func (d *DB) GetBugDeliveryReviewForWait(ctx context.Context, session int64, key string) (BugDeliveryReview, error) {
	var out BugDeliveryReview
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		var digest string
		e := tx.QueryRowContext(ctx, `SELECT digest FROM bug_delivery_reviews WHERE session_id=? AND delivery_key=?`, session, key).Scan(&digest)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		out, e = bugDeliveryReviewTx(ctx, tx, session, digest)
		return e
	})
	return out, e
}

// GetBugAnalysisReviewForWait returns retained presentation history for replay.
// Callers must still consume the authenticated reply through its typed operation.
func (d *DB) GetBugAnalysisReviewForWait(ctx context.Context, session int64, key string) (BugAnalysisReview, error) {
	var out BugAnalysisReview
	e := d.withTx(ctx, func(tx *sql.Tx) error {
		var review string
		e := tx.QueryRowContext(ctx, `SELECT review FROM bug_analysis_reviews WHERE session_id=? AND delivery_key=?`, session, key).Scan(&review)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		out, e = bugReviewTx(ctx, tx, session, review)
		return e
	})
	return out, e
}
