package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RetainBugDeliveryReview atomically supersedes the prior presentation only
// after checking its current repair, test, PR and clarification bindings.
func (d *DB) RetainBugDeliveryReview(ctx context.Context, r BugDeliveryReview, expected int64) error {
	digest, err := BugDeliveryReviewDigest(r.Evidence, r.PR, r.Presentation...)
	if err != nil {
		return err
	}
	if r.SessionID != r.Evidence.Identity.SessionID || digest != r.Digest || r.Body == "" || len(r.Body) > 60000 || r.ContentDigest != submissionDigest([]byte(r.Body)) || r.DeliveryKey == "" || r.BotID <= 0 || r.CommentID != 0 || r.Reason != "" {
		return errors.New("invalid Bug delivery review")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		old, e := bugDeliveryReviewTx(ctx, tx, r.SessionID, r.Digest)
		if e == nil {
			if old.ContentDigest != r.ContentDigest || old.DeliveryKey != r.DeliveryKey || old.BotID != r.BotID {
				return errors.New("bug review replay conflict")
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if e = bugDeliveryGuardTx(ctx, tx, r, expected); e != nil {
			return e
		}
		i, e := bugPRIntentTx(ctx, tx, r.SessionID)
		if e != nil {
			return e
		}
		if i.PR == nil || i.PR.ID != r.PR.ID || i.PR.Number != r.PR.Number || r.PR.CreatorID != i.CreatorID || r.PR.Branch != i.Branch || r.PR.Target != "main" || r.PR.Head != r.Evidence.Repair.SourceCommit || r.PR.State != "open" || r.PR.Merged || r.PR.BaseRepo != r.Evidence.Identity.Repository || r.PR.HeadRepo != r.PR.BaseRepo || !strings.Contains(r.PR.Body, i.Marker) {
			return errors.New("bug review PR provenance mismatch")
		}
		if e = bugClarificationsTx(ctx, tx, r); e != nil {
			return e
		}
		presentation, e := json.Marshal(r.Presentation)
		if e != nil {
			return e
		}
		evidence, e := json.Marshal(r.Evidence)
		if e != nil {
			return e
		}
		pr, e := json.Marshal(r.PR)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery_reviews SET active=0,reason='superseded' WHERE session_id=? AND active=1`, r.SessionID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_delivery_reviews(session_id,digest,evidence,pr,presentation,body,content_digest,delivery_key,bot_id) VALUES(?,?,?,?,?,?,?,?,?)`, r.SessionID, r.Digest, evidence, pr, presentation, r.Body, r.ContentDigest, r.DeliveryKey, r.BotID); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, r.SessionID)
		return e
	})
}

func bugDeliveryPublishedTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview) (int64, error) {
	var session, bot, comment int64
	var body string
	var complete bool
	err := tx.QueryRowContext(ctx, `SELECT session_id,bot_user_id,COALESCE(comment_id,0),body,completed FROM report_deliveries WHERE delivery_key=?`, r.DeliveryKey).Scan(&session, &bot, &comment, &body, &complete)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if !complete || session != r.SessionID || bot != r.BotID || comment <= 0 || submissionDigest([]byte(body)) != r.ContentDigest {
		return 0, errors.New("bug delivery review lacks confirmed matching publication")
	}
	return comment, nil
}

// ConfirmBugDeliveryReview uses the confirmed report journal as publication proof.
func (d *DB) ConfirmBugDeliveryReview(ctx context.Context, session int64, digest string) (BugDeliveryReview, error) {
	var out BugDeliveryReview
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, e := bugDeliveryReviewTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		if !r.Active {
			return errors.New("bug delivery review inactive")
		}
		comment, e := bugDeliveryPublishedTx(ctx, tx, r)
		if e != nil {
			return e
		}
		if r.CommentID != 0 && r.CommentID != comment {
			return errors.New("bug delivery publication changed")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery_reviews SET comment_id=? WHERE session_id=? AND digest=?`, comment, session, digest); e != nil {
			return e
		}
		r.CommentID = comment
		out = r
		return nil
	})
	return out, err
}

// BugDeliveryDecision is a consumed authenticated reply, not an agent assertion.
// Repair/reshape request identity is the unique session/comment pair.
type BugDeliveryDecision struct {
	SessionID, CommentID, OwnerID int64
	Digest, Kind, Body            string
	At                            time.Time
}

// ApprovalCommand names the exact published review, tested head and main tip.
func (r BugDeliveryReview) ApprovalCommand() string {
	return fmt.Sprintf("/approve-bug delivery %s %d %s main %s", r.Digest, r.PR.Number, r.PR.Head, r.Evidence.Child.Request.Binding.TargetCommit)
}

// ApplyBugDeliveryReply parses only exact owner controls on the active published
// review. Free text remains retained discussion and cannot authorize an effect.
func (d *DB) ApplyBugDeliveryReply(ctx context.Context, session int64, digest string, in GraphOwnerReply, expected int64) (BugDeliveryDecision, error) {
	var out BugDeliveryDecision
	if in.OwnerID <= 0 || in.OwnerID != in.AuthorID || in.CommentID <= 0 || in.At.IsZero() || in.Mode != "" || in.TaskRef != in.SourceRef || len(in.Body) > 8192 || strings.TrimSpace(in.Body) == "" {
		return out, errors.New("invalid Bug delivery owner reply")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	inputDigest := submissionDigest(raw)
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		var old, kind, body, at string
		var owner int64
		e := tx.QueryRowContext(ctx, `SELECT input_digest,kind,body,author_id,observed_at FROM bug_delivery_replies WHERE session_id=? AND comment_id=? AND digest=?`, session, in.CommentID, digest).Scan(&old, &kind, &body, &owner, &at)
		if e == nil {
			if old != inputDigest {
				return errors.New("bug delivery reply replay conflict")
			}
			t, e := time.Parse(time.RFC3339Nano, at)
			out = BugDeliveryDecision{session, in.CommentID, owner, digest, kind, body, t}
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		r, e := bugDeliveryReviewTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		if e = bugDeliveryGuardTx(ctx, tx, r, expected); e != nil {
			return e
		}
		identity := r.Evidence.Identity
		if !r.Active || in.OwnerID != identity.OwnerID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) || r.CommentID <= 0 || in.QuestionCommentID != r.CommentID || in.CommentID <= r.CommentID || in.DeliveryKey != r.DeliveryKey {
			return errors.New("bug delivery reply does not match published active review")
		}
		comment, e := bugDeliveryPublishedTx(ctx, tx, r)
		if e != nil {
			return e
		}
		if comment != r.CommentID {
			return errors.New("bug delivery publication conflict")
		}
		if e = bugDeliveryWaitTx(ctx, tx, r, in); e != nil {
			return e
		}
		body = strings.TrimSpace(in.Body)
		kind, e = ParseBugDeliveryControl(r, body)
		if e != nil {
			return e
		}

		if kind == "approved" {
			for _, c := range r.Presentation {
				if len(c.References) == 0 {
					return errors.New("unanswered Bug clarification prevents approval")
				}
			}
			var unresolved int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_delivery_replies WHERE session_id=? AND digest=? AND kind IN ('clarify','discussion','repair','reshape')`, session, digest).Scan(&unresolved); e != nil {
				return e
			}
			if unresolved != 0 {
				return errors.New("unresolved Bug feedback prevents delivery approval")
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_delivery_replies(session_id,comment_id,digest,input_digest,kind,body,author_id,observed_at) VALUES(?,?,?,?,?,?,?,?)`, session, in.CommentID, digest, inputDigest, kind, body, in.AuthorID, FormatTime(in.At)); e != nil {
			return e
		}
		if kind == "repair" || kind == "reshape" {
			if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery_reviews SET active=0,reason=? WHERE session_id=? AND digest=?`, kind, session, digest); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=? AND state='waiting'`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), r.DeliveryKey); e != nil {
			return e
		}
		out = BugDeliveryDecision{session, in.CommentID, in.AuthorID, digest, kind, body, in.At}
		return bugEventTx(ctx, tx, session, "delivery_reply", fmt.Sprint(in.CommentID), kind)
	})
	return out, err
}

// InvalidateBugDeliveryReview preserves historical decisions; they cease to be
// applicable. Only owned child/check invalidation can authorize fresh testing.
func (d *DB) InvalidateBugDeliveryReview(ctx context.Context, session int64, digest, reason string) error {
	if strings.TrimSpace(reason) == "" || len(reason) > 8192 {
		return errors.New("missing Bug review invalidation reason")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE bug_delivery_reviews SET active=0,reason=? WHERE session_id=? AND digest=?`, reason, session, digest); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session)
		return e
	})
}

// ApprovedBugDelivery is the read-only merge handoff. Historical approval alone
// cannot pass current evidence, publication and unresolved-discussion guards.
func (d *DB) ApprovedBugDelivery(ctx context.Context, session int64, digest string) (BugDeliveryReview, BugDeliveryDecision, error) {
	var r BugDeliveryReview
	var out BugDeliveryDecision
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, out, e = approvedBugDeliveryTx(ctx, tx, session, digest)
		return e
	})
	return r, out, err
}

func approvedBugDeliveryTx(ctx context.Context, tx *sql.Tx, session int64, digest string) (BugDeliveryReview, BugDeliveryDecision, error) {
	var r BugDeliveryReview
	var out BugDeliveryDecision
	err := func() error {
		var e error
		r, e = bugDeliveryReviewTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if e = bugDeliveryGuardTx(ctx, tx, r, s.Revision); e != nil {
			return e
		}
		for _, c := range r.Presentation {
			if len(c.References) == 0 {
				return errors.New("unanswered Bug clarification prevents approval")
			}
		}
		if !r.Active {
			return errors.New("bug delivery review no longer applicable")
		}
		if _, e = bugDeliveryPublishedTx(ctx, tx, r); e != nil {
			return e
		}
		var at string
		e = tx.QueryRowContext(ctx, `SELECT session_id,comment_id,author_id,digest,kind,body,observed_at FROM bug_delivery_replies WHERE session_id=? AND digest=? AND kind='approved'`, session, digest).Scan(&out.SessionID, &out.CommentID, &out.OwnerID, &out.Digest, &out.Kind, &out.Body, &at)
		if e != nil {
			return e
		}
		out.At, e = time.Parse(time.RFC3339Nano, at)
		if e != nil {
			return e
		}
		var pending int
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_delivery_replies WHERE session_id=? AND digest=? AND kind!='approved'`, session, digest).Scan(&pending)
		if e != nil {
			return e
		}
		if pending != 0 {
			return errors.New("bug delivery has unresolved feedback")
		}
		return nil
	}()
	return r, out, err
}

// ParseBugDeliveryControl recognizes exact controls but grants no authority.
// Authentication, publication, freshness and reply consumption remain in store.
func ParseBugDeliveryControl(r BugDeliveryReview, body string) (string, error) {
	if !validSubmissionDigest(r.Digest) || r.PR.Number <= 0 || len(r.PR.Head) != 40 || strings.Trim(r.PR.Head, "0123456789abcdef") != "" || len(r.Evidence.Child.Request.Binding.TargetCommit) != 40 || strings.Trim(r.Evidence.Child.Request.Binding.TargetCommit, "0123456789abcdef") != "" {
		return "", errors.New("invalid Bug delivery control binding")
	}
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 8192 || strings.ContainsRune(body, '\x00') {
		return "", errors.New("invalid Bug feedback text")
	}
	if body == r.ApprovalCommand() {
		return "approved", nil
	}
	if strings.HasPrefix(body, "/approve-bug") {
		return "", errors.New("delivery approval must exactly match displayed command")
	}
	if strings.HasPrefix(body, "/bug-review") {
		fields := strings.SplitN(body, " ", 4)
		if len(fields) != 4 || fields[0] != "/bug-review" || fields[1] != r.Digest || strings.TrimSpace(fields[3]) == "" || strings.Contains(fields[3], "/approve-bug") || strings.Contains(fields[3], "/bug-review") {
			return "", errors.New("invalid or mixed Bug feedback control")
		}
		switch fields[2] {
		case "clarify", "repair", "reshape":
			return fields[2], nil
		}
		return "", errors.New("unknown Bug feedback classification")
	}
	return "discussion", nil
}

// RecoverBugDeliveryReply returns an identical previously consumed reply without
// repeating remote reads or minting another continuation. Consumers still must
// use ApprovedBugDelivery for current merge authorization.
func (d *DB) RecoverBugDeliveryReply(ctx context.Context, session int64, digest string, in GraphOwnerReply) (BugDeliveryDecision, error) {
	var out BugDeliveryDecision
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	var retained, at string
	err = d.sql.QueryRowContext(ctx, `SELECT session_id,comment_id,author_id,digest,kind,body,observed_at,input_digest FROM bug_delivery_replies WHERE session_id=? AND digest=? AND comment_id=?`, session, digest, in.CommentID).Scan(&out.SessionID, &out.CommentID, &out.OwnerID, &out.Digest, &out.Kind, &out.Body, &at, &retained)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if submissionDigest(raw) != retained {
		return out, errors.New("bug delivery reply replay conflict")
	}
	out.At, err = time.Parse(time.RFC3339Nano, at)
	return out, err
}
