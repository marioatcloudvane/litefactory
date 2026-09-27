package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

func bugReviewTx(ctx context.Context, tx *sql.Tx, session int64, review string) (BugAnalysisReview, error) {
	var r BugAnalysisReview
	err := tx.QueryRowContext(ctx, `SELECT session_id,review,analysis_revision,analysis_digest,delivery_key,content_digest,COALESCE(comment_id,0),active FROM bug_analysis_reviews WHERE session_id=? AND review=?`, session, review).Scan(&r.SessionID, &r.Review, &r.AnalysisRevision, &r.AnalysisDigest, &r.DeliveryKey, &r.ContentDigest, &r.CommentID, &r.Active)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

// RetainBugAnalysisReview records the immutable review before publication. It
// does not mark publication or grant approval. Its source is the exact pass
// that produced the current reviewable analysis, never another task's comment.
func (d *DB) RetainBugAnalysisReview(ctx context.Context, in BugAnalysisReview, expectedRevision int64) error {
	if in.SessionID <= 0 || in.Review == "" || in.AnalysisRevision <= 0 || !validSubmissionDigest(in.AnalysisDigest) || in.DeliveryKey == "" || !validSubmissionDigest(in.ContentDigest) || in.CommentID != 0 || in.Active || expectedRevision <= 0 {
		return errors.New("store: invalid Bug analysis review")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		old, err := bugReviewTx(ctx, tx, in.SessionID, in.Review)
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
		if _, err = tx.ExecContext(ctx, `UPDATE bug_analysis_reviews SET active=0 WHERE session_id=? AND active=1`, in.SessionID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_analysis_reviews(session_id,review,analysis_revision,analysis_digest,delivery_key,content_digest,active) VALUES(?,?,?,?,?,?,1)`, in.SessionID, in.Review, in.AnalysisRevision, in.AnalysisDigest, in.DeliveryKey, in.ContentDigest); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, in.SessionID, "review", in.Review, "")
	})
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
		r, err := bugReviewTx(ctx, tx, session, review)
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
			if _, err = tx.ExecContext(ctx, `UPDATE bug_analysis_reviews SET comment_id=? WHERE session_id=? AND review=?`, comment, session, review); err != nil {
				return err
			}
			if err = bugEventTx(ctx, tx, session, "review_published", review, ""); err != nil {
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
		r, err := bugReviewTx(ctx, tx, session, review)
		if err != nil {
			return err
		}
		var approvedAt string
		err = tx.QueryRowContext(ctx, `SELECT review,analysis_revision,analysis_digest,reply_id,owner_id,approved_at FROM bug_analysis_approvals WHERE session_id=? AND review=?`, session, review).Scan(&out.Review, &out.AnalysisRevision, &out.AnalysisDigest, &out.ReplyID, &out.OwnerID, &approvedAt)
		if err == nil {
			if out.ReplyID != in.CommentID || in.DeliveryKey != r.DeliveryKey || strings.TrimSpace(in.Body) != fmt.Sprintf("/approve-bug analysis %d", r.AnalysisRevision) || in.QuestionCommentID != r.CommentID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
				return errors.New("store: Bug approval replay conflict")
			}
			out.ApprovedAt, err = time.Parse(time.RFC3339Nano, approvedAt)
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
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analysis_passes WHERE session_id=? AND state IN('reserved','dispatched')`, session).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return ErrBugAnalysisActive
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bug_analysis_approvals(session_id,review,analysis_revision,analysis_digest,reply_id,owner_id,approved_at) VALUES(?,?,?,?,?,?,?)`, session, review, r.AnalysisRevision, r.AnalysisDigest, in.CommentID, in.OwnerID, in.At.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		// The allocated state is terminal consumption. No fabricated agent/run or
		// continuation attempt is supplied; the parent owns its next transition.
		if _, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), r.DeliveryKey); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); err != nil {
			return err
		}
		if err = bugEventTx(ctx, tx, session, "approved", review, ""); err != nil {
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
		if err = tx.QueryRowContext(ctx, `SELECT review FROM bug_analysis_reviews WHERE session_id=? AND analysis_revision=? AND active=1`, session, state.CurrentAnalysis).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		review, err = bugReviewTx(ctx, tx, session, id)
		if err != nil {
			return err
		}
		wait, err = scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=?`, review.DeliveryKey))
		return err
	})
	return state, review, wait, err
}
