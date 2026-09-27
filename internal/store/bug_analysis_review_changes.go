package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BugAnalysisReviewChange preserves authenticated owner discussion separately
// from question answers and approval. It never authorizes the changed remedy.
type BugAnalysisReviewChange struct {
	CommentID, AuthorID int64
	Review, Body        string
	At                  time.Time
}

// BugAnalysisReviewChanges retrieves immutable authenticated review feedback.
func (d *DB) BugAnalysisReviewChanges(ctx context.Context, session int64) ([]BugAnalysisReviewChange, error) {
	rows, e := d.sql.QueryContext(ctx, `SELECT comment_id,author_id,review,body,observed_at FROM bug_analysis_review_changes WHERE session_id=? ORDER BY comment_id LIMIT 4097`, session)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []BugAnalysisReviewChange
	for rows.Next() {
		var r BugAnalysisReviewChange
		var at string
		if e = rows.Scan(&r.CommentID, &r.AuthorID, &r.Review, &r.Body, &at); e != nil {
			return nil, e
		}
		r.At, e = time.Parse(time.RFC3339Nano, at)
		if e != nil {
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
		r, e := bugReviewTx(ctx, tx, session, review)
		if e != nil {
			return e
		}
		w, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, in.InstanceID))
		if e != nil {
			return e
		}
		if w.State == "allocated" && w.ReplyID == in.CommentID && w.ReplyAuthorID == in.AuthorID && w.ReplyBody == in.Body {
			var count int
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_analysis_review_changes WHERE session_id=? AND comment_id=? AND review=? AND body=?`, session, in.CommentID, review, in.Body).Scan(&count)
			if e != nil {
				return e
			}
			if count != 1 {
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
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_analysis_review_changes(session_id,comment_id,review,author_id,body,observed_at) VALUES(?,?,?,?,?,?)`, session, in.CommentID, review, in.AuthorID, in.Body, in.At.UTC().Format(time.RFC3339Nano)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), r.DeliveryKey); e != nil {
			return e
		}
		// Historical approval is retained, but an active change request cannot remain
		// the current approvable review. Fresh analysis and the same gate are owed.
		if _, e = tx.ExecContext(ctx, `UPDATE bug_analysis_reviews SET active=0 WHERE session_id=? AND review=?`, session, review); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); e != nil {
			return e
		}
		return bugEventTx(ctx, tx, session, "review_change", fmt.Sprint(in.CommentID), in.Body)
	})
}
