package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/bugrepair"
)

// BugPRIntent is the sole immutable task PR provenance and dispatch ledger.
type BugPRIntent struct {
	SessionID                 int64
	Operation, Branch, Marker string
	CreatorID                 int64
	Dispatched, Cancelled     bool
	PR                        *BugPullRequest
}

func bugPRIntentTx(ctx context.Context, tx *sql.Tx, session int64) (BugPRIntent, error) {
	var i BugPRIntent
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT session_id,pr_operation,pr_branch,pr_creator_id,pr_marker,pr_dispatched,pr_cancelled,pr FROM bug_delivery WHERE session_id=?`, session).Scan(&i.SessionID, &i.Operation, &i.Branch, &i.CreatorID, &i.Marker, &i.Dispatched, &i.Cancelled, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	if err != nil {
		return i, err
	}
	if len(raw) > 0 {
		var p BugPullRequest
		if err = json.Unmarshal(raw, &p); err != nil {
			return i, err
		}
		i.PR = &p
	}
	return i, nil
}

// GetBugPRIntent reads the retained request even when creation is uncertain.
func (d *DB) GetBugPRIntent(ctx context.Context, session int64) (BugPRIntent, error) {
	var i BugPRIntent
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; i, e = bugPRIntentTx(ctx, tx, session); return e })
	return i, err
}

// EnsureBugPRIntent retains the sole task provenance before any remote effect.
// A different call generation may reuse this PR, but never replace its identity.
func (d *DB) EnsureBugPRIntent(ctx context.Context, i BugPRIntent, callID, expected int64) (BugPRIntent, error) {
	if i.SessionID <= 0 || i.Operation == "" || len(i.Operation) > 128 || i.CreatorID <= 0 || i.Branch == "" || i.Branch == "main" || strings.ContainsAny(i.Branch, "\x00\r\n ") || i.Dispatched || i.Cancelled || i.PR != nil {
		return i, errors.New("invalid Bug PR intent")
	}
	i.Marker = fmt.Sprintf("<!-- lf:bug-pr session=%d operation=%s -->", i.SessionID, submissionDigest([]byte(i.Operation)))
	var out BugPRIntent
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var branch string
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(branch,'') FROM sessions WHERE id=?`, i.SessionID).Scan(&branch); e != nil {
			return e
		}
		if branch == "" || branch != i.Branch {
			return errors.New("bug PR branch differs from frozen task branch")
		}
		old, e := bugPRIntentTx(ctx, tx, i.SessionID)
		if e == nil {
			if old.Operation != i.Operation || old.Branch != i.Branch || old.CreatorID != i.CreatorID {
				return errors.New("bug PR immutable intent conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = bugDeliveryEvidenceTx(ctx, tx, i.SessionID, callID); e != nil {
			return e
		}
		state, e := bugExecutionTx(ctx, tx, i.SessionID)
		if e != nil {
			return e
		}
		if state.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_delivery(session_id,pr_operation,pr_input_digest,pr_branch,pr_creator_id,pr_marker) VALUES(?,?,?,?,?,?)`, i.SessionID, i.Operation, submissionDigest([]byte(i.Operation)), i.Branch, i.CreatorID, i.Marker); e != nil {
			return e
		}
		out = i
		return bugEventTx(ctx, tx, i.SessionID, "pr_intent", i.Operation, 0, "", nil)
	})
	return out, err
}

// ClaimBugPRCreate is a one-shot dispatch. Empty subsequent listings cannot
// reset this claim; uncertain requests remain pending for independent readback.
func (d *DB) ClaimBugPRCreate(ctx context.Context, session, callID, expected int64) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		i, e := bugPRIntentTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if i.Cancelled || i.Dispatched || i.PR != nil {
			return nil
		}
		if _, e = bugDeliveryEvidenceTx(ctx, tx, session, callID); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET pr_dispatched=1 WHERE session_id=?`, session); e != nil {
			return e
		}
		claimed = true
		return bugEventTx(ctx, tx, session, "pr_dispatch", i.Operation, 0, "", nil)
	})
	return claimed, err
}

// BugPRObservation retains errors and returned identity before validation. The
// first numeric PR is sticky even when its other observed fields conflict.
type BugPRObservation struct {
	Operation, Reason string
	PR                *BugPullRequest
	At                time.Time
}

// ObserveBugPR appends a bounded remote fact without replacing numeric identity.
func (d *DB) ObserveBugPR(ctx context.Context, session int64, o BugPRObservation) error {
	if o.Operation == "" || len(o.Operation) > 256 || o.At.IsZero() || len(o.Reason) > 8192 {
		return errors.New("invalid Bug PR observation")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		existing, e := bugEventByReferenceTx(ctx, tx, session, "pr_observation", o.Operation)
		if e == nil {
			raw, e := json.Marshal(o)
			if e != nil {
				return e
			}
			if string(existing) != string(raw) {
				return errors.New("bug PR observation replay conflict")
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		i, e := bugPRIntentTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if o.PR != nil && o.PR.ID > 0 && o.PR.Number > 0 {
			if i.PR != nil && (i.PR.ID != o.PR.ID || i.PR.Number != o.PR.Number) {
				return errors.New("bug PR identity cannot be replaced")
			}
			if i.PR == nil {
				p, e := json.Marshal(o.PR)
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET pr=? WHERE session_id=?`, p, session); e != nil {
					return e
				}
			}
		}
		return bugEventJSONTx(ctx, tx, session, "pr_observation", o.Operation, o)
	})
}

// CancelBugDelivery suppresses new delivery effects without erasing prior effects.
func (d *DB) CancelBugDelivery(ctx context.Context, session int64) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE bug_delivery SET pr_cancelled=1 WHERE session_id=?`, session)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrNotFound
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET review_active=0,review_reason='cancelled' WHERE session_id=? AND review_active=1`, session); e != nil {
			return e
		}
		return bugBumpRevisionTx(ctx, tx, session)
	})
}

// BugDeliveryEvidence is immutable review input derived from domain records.
// The child identity binds all three retained command evidence sets.
type BugDeliveryEvidence struct {
	Identity BugIdentity
	Analysis BugAnalysisRevision
	Approval BugAnalysisApproval
	Repair   BugRepairCompletion
	Coverage bugrepair.Evidence
	Child    BugTestCall
}

func bugDeliveryEvidenceTx(ctx context.Context, tx *sql.Tx, session, callID int64) (BugDeliveryEvidence, error) {
	var out BugDeliveryEvidence
	var err error
	out.Identity, err = bugIdentityTx(ctx, tx, session)
	if err != nil {
		return out, err
	}
	out.Child, err = scanBugTestRow(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_repairs WHERE test_child_instance=? AND session_id=?`, callID, session))
	if err != nil {
		return out, err
	}
	c := out.Child
	if c.Outcome != "passed" || !c.Consumed || !c.Current || c.Cancelled {
		return out, errors.New("bug delivery requires a current consumed passed child")
	}
	outcome, _, err := bugTestEvidenceTx(ctx, tx, c)
	if err != nil {
		return out, err
	}
	if outcome != "passed" {
		return out, errors.New("bug delivery phase evidence no longer passes")
	}
	if err = bugTestRepairTx(ctx, tx, c.Request); err != nil {
		return out, err
	}
	var guarded, activeRepairs int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM bug_tasks WHERE session_id=? AND guard_owner IS NOT NULL),(SELECT count(*) FROM bug_repairs WHERE session_id=? AND state IN ('reserved','dispatched'))`, session, session).Scan(&guarded, &activeRepairs); err != nil {
		return out, err
	}
	if guarded+activeRepairs != 0 {
		return out, ErrBugWorkActive
	}
	r, err := bugRepairTx(ctx, tx, session, c.Request.Binding.Repair)
	if err != nil {
		return out, err
	}
	out.Analysis, err = bugRevisionTx(ctx, tx, session, r.AnalysisRevision)
	if err != nil {
		return out, err
	}
	var approvedAt sql.NullString
	var replyID, ownerID sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT approval_reply_id,approval_owner_id,approval_approved_at FROM bug_analyses WHERE session_id=? AND review=? AND approval_reply_id IS NOT NULL`, session, r.ApprovalReview).Scan(&replyID, &ownerID, &approvedAt)
	if err != nil {
		return out, err
	}
	out.Approval = BugAnalysisApproval{Review: r.ApprovalReview, AnalysisRevision: r.AnalysisRevision, AnalysisDigest: r.AnalysisDigest, ReplyID: replyID.Int64, OwnerID: ownerID.Int64}
	out.Approval.ApprovedAt, err = time.Parse(time.RFC3339Nano, approvedAt.String)
	if err != nil {
		return out, err
	}
	completion, err := bugRepairCompletionTx(ctx, tx, session, r.Request)
	if err != nil {
		return out, err
	}
	out.Repair = completion
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT completion_evidence FROM bug_repairs WHERE session_id=? AND request=?`, session, r.Request).Scan(&raw); err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out.Coverage)
	return out, err
}

// BugDeliveryPrerequisites revalidates completed repair, exact approval and
// independently retained passing phases without granting any delivery authority.
func (d *DB) BugDeliveryPrerequisites(ctx context.Context, session, callID int64) (BugDeliveryEvidence, int64, error) {
	var out BugDeliveryEvidence
	var revision int64
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugDeliveryEvidenceTx(ctx, tx, session, callID)
		if e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		revision = s.Revision
		return e
	})
	return out, revision, err
}

// BugPullRequest records remote numeric provenance without importing a forge
// client into persistence. Body is already redacted before retention.
type BugPullRequest struct {
	ID, CreatorID                                         int64
	Number                                                int
	Head, Branch, Target, HeadRepo, BaseRepo, State, Body string
	Merged                                                bool
}

// BugDeliveryClarification points only to existing immutable evidence sections.
// Empty references explicitly retain an unanswered point.
type BugDeliveryClarification struct {
	ReviewDigest string
	CommentID    int64
	Body         string
	References   []string
}

// BugDeliveryReview binds the displayed content to exact immutable evidence.
type BugDeliveryReview struct {
	SessionID                        int64
	Digest                           string
	Evidence                         BugDeliveryEvidence
	PR                               BugPullRequest
	Body, ContentDigest, DeliveryKey string
	BotID, CommentID                 int64
	Active                           bool
	Reason                           string
	Presentation                     []BugDeliveryClarification
}

// BugDeliveryReviewDigest excludes publication fields, avoiding a circular
// dependency between the displayed command and its confirmed content digest.
// facts is the complete recorded fact list for the bug (every stage); binding
// it into the digest means a fact recorded after this review was published
// changes the digest, so the review goes stale through the existing
// invalidate-and-re-present path (AC-007) instead of silently omitting it.
// selfChecks is the repair attempt's fixer self-check summary (AC-009); a new
// self-check run recorded after this review was published changes the digest
// the same way a new fact does.
func BugDeliveryReviewDigest(e BugDeliveryEvidence, p BugPullRequest, facts []BugFact, selfChecks []BugSelfCheckSummary, presentation ...BugDeliveryClarification) (string, error) {
	raw, err := json.Marshal(struct {
		Evidence     BugDeliveryEvidence
		PR           BugPullRequest
		Facts        []BugFact
		SelfChecks   []BugSelfCheckSummary
		Presentation []BugDeliveryClarification
	}{e, p, facts, selfChecks, presentation})
	if err != nil {
		return "", err
	}
	return submissionDigest(raw), nil
}

// bugDeliveryReviewByDigestTx resolves a review by its exact digest, whether it
// is the bug's current review or one superseded into bug_events history.
func bugDeliveryReviewByDigestTx(ctx context.Context, tx *sql.Tx, session int64, digest string) (BugDeliveryReview, error) {
	var r BugDeliveryReview
	var e, p, presentation []byte
	var commentID sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT digest,evidence,review_pr,review_presentation,body,content_digest,delivery_key,bot_id,comment_id,active,reason FROM (SELECT review_digest AS digest,review_evidence AS evidence,review_pr,review_presentation,review_body AS body,review_content_digest AS content_digest,review_delivery_key AS delivery_key,review_bot_id AS bot_id,review_comment_id AS comment_id,review_active AS active,review_reason AS reason FROM bug_delivery WHERE session_id=?) WHERE digest=?`, session, digest).Scan(&r.Digest, &e, &p, &presentation, &r.Body, &r.ContentDigest, &r.DeliveryKey, &r.BotID, &commentID, &r.Active, &r.Reason)
	if err == nil {
		r.SessionID, r.CommentID = session, commentID.Int64
		if err = json.Unmarshal(e, &r.Evidence); err != nil {
			return r, err
		}
		if err = json.Unmarshal(p, &r.PR); err != nil {
			return r, err
		}
		err = json.Unmarshal(presentation, &r.Presentation)
		return r, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	content, err := bugEventByReferenceTx(ctx, tx, session, "delivery_review", digest)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(content, &r)
	return r, err
}

// bugDeliveryReviewByDeliveryKeyTx resolves a review by its unique delivery
// key, checking both the live row and superseded event history so a late
// reply to an older review still resolves.
func bugDeliveryReviewByDeliveryKeyTx(ctx context.Context, tx *sql.Tx, session int64, key string) (BugDeliveryReview, error) {
	var digest string
	err := tx.QueryRowContext(ctx, `SELECT review_digest FROM bug_delivery WHERE session_id=? AND review_delivery_key=?`, session, key).Scan(&digest)
	if err == nil {
		return bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BugDeliveryReview{}, err
	}
	content, err := bugEventByDeliveryKeyTx(ctx, tx, session, "delivery_review", key)
	if err != nil {
		return BugDeliveryReview{}, err
	}
	var r BugDeliveryReview
	err = json.Unmarshal(content, &r)
	return r, err
}

// GetBugDeliveryReview retrieves a historical or active immutable revision.
func (d *DB) GetBugDeliveryReview(ctx context.Context, session int64, digest string) (BugDeliveryReview, error) {
	var r BugDeliveryReview
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
		return e
	})
	return r, err
}

// CurrentBugDeliveryReview loads the active immutable delivery review.
func (d *DB) CurrentBugDeliveryReview(ctx context.Context, session int64) (BugDeliveryReview, error) {
	var out BugDeliveryReview
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var digest string
		if err := tx.QueryRowContext(ctx, `SELECT review_digest FROM bug_delivery WHERE session_id=? AND review_active=1`, session).Scan(&digest); err != nil {
			return err
		}
		var err error
		out, err = bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

func bugDeliveryGuardTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview, expected int64, facts []BugFact, selfChecks []BugSelfCheckSummary) error {
	state, err := bugExecutionTx(ctx, tx, r.SessionID)
	if err != nil {
		return err
	}
	if state.Revision != expected {
		return ErrBugRevisionConflict
	}
	current, err := bugDeliveryEvidenceTx(ctx, tx, r.SessionID, r.Evidence.Child.ID)
	if err != nil {
		return err
	}
	want, err := BugDeliveryReviewDigest(current, r.PR, facts, selfChecks, r.Presentation...)
	if err != nil {
		return err
	}
	if want != r.Digest {
		return errors.New("bug delivery evidence changed")
	}
	var cancelled bool
	if err = tx.QueryRowContext(ctx, `SELECT pr_cancelled FROM bug_delivery WHERE session_id=?`, r.SessionID).Scan(&cancelled); err != nil {
		return err
	}
	if cancelled {
		return errors.New("bug delivery cancelled")
	}
	return nil
}

// bugRetireCurrentDeliveryReviewTx moves the current review row into bug_events
// (kind "delivery_review"), so it stays resolvable by digest and delivery key
// even after superseded.
func bugRetireCurrentDeliveryReviewTx(ctx context.Context, tx *sql.Tx, session int64, reason string) error {
	var digest sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT review_digest FROM bug_delivery WHERE session_id=?`, session).Scan(&digest); err != nil {
		return err
	}
	if !digest.Valid {
		return nil
	}
	old, err := bugDeliveryReviewByDigestTx(ctx, tx, session, digest.String)
	if err != nil {
		return err
	}
	old.Active, old.Reason = false, reason
	return bugEventJSONFullTx(ctx, tx, session, "delivery_review", old.Digest, 0, old.DeliveryKey, old)
}

// RetainBugDeliveryReview atomically supersedes the prior presentation only
// after checking its current repair, test, PR and clarification bindings.
func (d *DB) RetainBugDeliveryReview(ctx context.Context, r BugDeliveryReview, expected int64) error {
	if r.SessionID != r.Evidence.Identity.SessionID || r.Body == "" || len(r.Body) > 60000 || r.ContentDigest != submissionDigest([]byte(r.Body)) || r.DeliveryKey == "" || r.BotID <= 0 || r.CommentID != 0 || r.Reason != "" {
		return errors.New("invalid Bug delivery review")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		// Facts and self-checks are read fresh, inside this transaction, rather
		// than carried on r: one recorded between the caller composing r and this
		// call commits still changes the digest here, so it never gets silently
		// folded into a review that never saw it (AC-007, AC-009).
		facts, e := bugFactsTx(ctx, tx, r.SessionID, "")
		if e != nil {
			return e
		}
		selfChecksRaw, e := bugSelfChecksTx(ctx, tx, r.SessionID, r.Evidence.Repair.Request)
		if e != nil {
			return e
		}
		selfChecks := SummarizeBugSelfChecks(selfChecksRaw)
		digest, e := BugDeliveryReviewDigest(r.Evidence, r.PR, facts, selfChecks, r.Presentation...)
		if e != nil {
			return e
		}
		if digest != r.Digest {
			return errors.New("invalid Bug delivery review")
		}
		old, e := bugDeliveryReviewByDigestTx(ctx, tx, r.SessionID, r.Digest)
		if e == nil {
			if old.ContentDigest != r.ContentDigest || old.DeliveryKey != r.DeliveryKey || old.BotID != r.BotID {
				return errors.New("bug review replay conflict")
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if e = bugDeliveryGuardTx(ctx, tx, r, expected, facts, selfChecks); e != nil {
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
		if e = bugRetireCurrentDeliveryReviewTx(ctx, tx, r.SessionID, "superseded"); e != nil {
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
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET review_digest=?,review_evidence=?,review_pr=?,review_presentation=?,review_body=?,review_content_digest=?,review_delivery_key=?,review_bot_id=?,review_comment_id=NULL,review_active=1,review_reason='' WHERE session_id=?`, r.Digest, evidence, pr, presentation, r.Body, r.ContentDigest, r.DeliveryKey, r.BotID, r.SessionID); e != nil {
			return e
		}
		return bugBumpRevisionTx(ctx, tx, r.SessionID)
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
		r, e := bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
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
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET review_comment_id=? WHERE session_id=? AND review_digest=?`, comment, session, digest); e != nil {
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

type bugDeliveryReplyEventContent struct {
	Digest, InputDigest, Kind, Body string
	AuthorID                        int64
	ObservedAt                      string
}

func bugDeliveryReplyKindTx(ctx context.Context, tx *sql.Tx, session, comment int64) (string, error) {
	var approvalComment sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT approval_comment_id FROM bug_delivery WHERE session_id=?`, session).Scan(&approvalComment); err != nil {
		return "", err
	}
	if approvalComment.Valid && approvalComment.Int64 == comment {
		return "approved", nil
	}
	content, err := bugEventByReferenceTx(ctx, tx, session, "delivery_reply", fmt.Sprint(comment))
	if err != nil {
		return "", err
	}
	var c bugDeliveryReplyEventContent
	if err = json.Unmarshal(content, &c); err != nil {
		return "", err
	}
	return c.Kind, nil
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
		existingKind, e := bugDeliveryReplyKindTx(ctx, tx, session, in.CommentID)
		if e == nil {
			if existingKind == "approved" {
				var approvedAt sql.NullString
				var owner int64
				var approvalDigest string
				if e = tx.QueryRowContext(ctx, `SELECT approval_owner_id,approval_digest,approval_at FROM bug_delivery WHERE session_id=?`, session).Scan(&owner, &approvalDigest, &approvedAt); e != nil {
					return e
				}
				if approvalDigest != digest {
					return errors.New("bug delivery reply replay conflict")
				}
				t, e := time.Parse(time.RFC3339Nano, approvedAt.String)
				out = BugDeliveryDecision{session, in.CommentID, owner, digest, "approved", strings.TrimSpace(in.Body), t}
				return e
			}
			content, e := bugEventByReferenceTx(ctx, tx, session, "delivery_reply", fmt.Sprint(in.CommentID))
			if e != nil {
				return e
			}
			var c bugDeliveryReplyEventContent
			if e = json.Unmarshal(content, &c); e != nil {
				return e
			}
			if c.InputDigest != inputDigest {
				return errors.New("bug delivery reply replay conflict")
			}
			t, e := time.Parse(time.RFC3339Nano, c.ObservedAt)
			out = BugDeliveryDecision{session, in.CommentID, c.AuthorID, c.Digest, c.Kind, c.Body, t}
			return e
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		r, e := bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		facts, e := bugFactsTx(ctx, tx, session, "")
		if e != nil {
			return e
		}
		selfChecksRaw, e := bugSelfChecksTx(ctx, tx, session, r.Evidence.Repair.Request)
		if e != nil {
			return e
		}
		if e = bugDeliveryGuardTx(ctx, tx, r, expected, facts, SummarizeBugSelfChecks(selfChecksRaw)); e != nil {
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
		body := strings.TrimSpace(in.Body)
		kind, e := ParseBugDeliveryControl(r, body)
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
			rows, e := tx.QueryContext(ctx, `SELECT reference,content FROM bug_events WHERE session_id=? AND kind='delivery_reply'`, session)
			if e != nil {
				return e
			}
			for rows.Next() {
				var reference string
				var content []byte
				if e = rows.Scan(&reference, &content); e != nil {
					_ = rows.Close()
					return e
				}
				var c bugDeliveryReplyEventContent
				if e = json.Unmarshal(content, &c); e != nil {
					_ = rows.Close()
					return e
				}
				if c.Digest == digest && (c.Kind == "clarify" || c.Kind == "discussion" || c.Kind == "repair" || c.Kind == "reshape") {
					unresolved++
				}
			}
			if e = errors.Join(rows.Err(), rows.Close()); e != nil {
				return e
			}
			if unresolved != 0 {
				return errors.New("unresolved Bug feedback prevents delivery approval")
			}
		}
		if kind == "approved" {
			if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET approval_comment_id=?,approval_owner_id=?,approval_digest=?,approval_body=?,approval_at=? WHERE session_id=?`, in.CommentID, in.AuthorID, digest, body, in.At.UTC().Format(time.RFC3339Nano), session); e != nil {
				return e
			}
		} else {
			if e = bugEventJSONTx(ctx, tx, session, "delivery_reply", fmt.Sprint(in.CommentID), bugDeliveryReplyEventContent{Digest: digest, InputDigest: inputDigest, Kind: kind, Body: body, AuthorID: in.AuthorID, ObservedAt: in.At.UTC().Format(time.RFC3339Nano)}); e != nil {
				return e
			}
		}
		if kind == "repair" || kind == "reshape" {
			if e = bugRetireCurrentDeliveryReviewTx(ctx, tx, session, kind); e != nil {
				return e
			}
		}
		if e = bugBumpRevisionTx(ctx, tx, session); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=? AND state='waiting'`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), r.DeliveryKey); e != nil {
			return e
		}
		out = BugDeliveryDecision{session, in.CommentID, in.AuthorID, digest, kind, body, in.At}
		return bugEventTx(ctx, tx, session, "delivery_reply_seen", fmt.Sprint(in.CommentID), 0, "", []byte(bugJSONString(kind)))
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
		if _, e := tx.ExecContext(ctx, `UPDATE bug_delivery SET review_active=0,review_reason=? WHERE session_id=? AND review_digest=?`, reason, session, digest); e != nil {
			return e
		}
		return bugBumpRevisionTx(ctx, tx, session)
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
		r, e = bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		facts, e := bugFactsTx(ctx, tx, session, "")
		if e != nil {
			return e
		}
		selfChecksRaw, e := bugSelfChecksTx(ctx, tx, session, r.Evidence.Repair.Request)
		if e != nil {
			return e
		}
		if e = bugDeliveryGuardTx(ctx, tx, r, s.Revision, facts, SummarizeBugSelfChecks(selfChecksRaw)); e != nil {
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
		var comment, owner int64
		var body, at string
		e = tx.QueryRowContext(ctx, `SELECT approval_comment_id,approval_owner_id,approval_body,approval_at FROM bug_delivery WHERE session_id=? AND approval_digest=?`, session, digest).Scan(&comment, &owner, &body, &at)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		out = BugDeliveryDecision{SessionID: session, CommentID: comment, OwnerID: owner, Digest: digest, Kind: "approved", Body: body}
		out.At, e = time.Parse(time.RFC3339Nano, at)
		if e != nil {
			return e
		}
		var pending int
		rows, e := tx.QueryContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='delivery_reply'`, session)
		if e != nil {
			return e
		}
		for rows.Next() {
			var content []byte
			if e = rows.Scan(&content); e != nil {
				_ = rows.Close()
				return e
			}
			var c bugDeliveryReplyEventContent
			if e = json.Unmarshal(content, &c); e != nil {
				_ = rows.Close()
				return e
			}
			if c.Digest == digest {
				pending++
			}
		}
		if e = errors.Join(rows.Err(), rows.Close()); e != nil {
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
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		kind, e := bugDeliveryReplyKindTx(ctx, tx, session, in.CommentID)
		if errors.Is(e, ErrNotFound) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if kind == "approved" {
			var owner int64
			var approvalDigest, at string
			if e = tx.QueryRowContext(ctx, `SELECT approval_owner_id,approval_digest,approval_at FROM bug_delivery WHERE session_id=?`, session).Scan(&owner, &approvalDigest, &at); e != nil {
				return e
			}
			if approvalDigest != digest {
				return errors.New("bug delivery reply replay conflict")
			}
			out.At, e = time.Parse(time.RFC3339Nano, at)
			out.SessionID, out.CommentID, out.OwnerID, out.Digest, out.Kind = session, in.CommentID, owner, digest, "approved"
			return e
		}
		content, e := bugEventByReferenceTx(ctx, tx, session, "delivery_reply", fmt.Sprint(in.CommentID))
		if e != nil {
			return e
		}
		var c bugDeliveryReplyEventContent
		if e = json.Unmarshal(content, &c); e != nil {
			return e
		}
		if submissionDigest(raw) != c.InputDigest {
			return errors.New("bug delivery reply replay conflict")
		}
		out.SessionID, out.CommentID, out.OwnerID, out.Digest, out.Kind, out.Body = session, in.CommentID, c.AuthorID, c.Digest, c.Kind, c.Body
		out.At, e = time.Parse(time.RFC3339Nano, c.ObservedAt)
		return e
	})
	if errors.Is(err, ErrNotFound) {
		return out, ErrNotFound
	}
	return out, err
}

func bugClarificationsTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview) error {
	if len(r.Presentation) > 32 {
		return errors.New("bug clarification presentation exceeds bound")
	}
	seen := make(map[int64]bool)
	for _, c := range r.Presentation {
		if c.CommentID <= 0 || seen[c.CommentID] || len(c.References) > 5 {
			return errors.New("invalid Bug clarification reference")
		}
		seen[c.CommentID] = true
		kind, err := bugDeliveryReplyKindTx(ctx, tx, r.SessionID, c.CommentID)
		if err != nil {
			return err
		}
		content, err := bugEventByReferenceTx(ctx, tx, r.SessionID, "delivery_reply", fmt.Sprint(c.CommentID))
		if err != nil {
			return err
		}
		var replied bugDeliveryReplyEventContent
		if err = json.Unmarshal(content, &replied); err != nil {
			return err
		}
		if c.Body != replied.Body || replied.Digest != c.ReviewDigest || (kind != "clarify" && kind != "discussion") {
			return errors.New("bug clarification differs from retained discussion")
		}
		refs := make(map[string]bool)
		for _, ref := range c.References {
			if refs[ref] {
				return errors.New("duplicate Bug clarification evidence")
			}
			refs[ref] = true
			switch ref {
			case "analysis", "repair", "regression", "focused", "full":
			default:
				return errors.New("bug clarification can reference only retained evidence")
			}
		}
	}
	var priorDigest sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT review_digest FROM bug_delivery WHERE session_id=? AND review_active=1`, r.SessionID).Scan(&priorDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if !priorDigest.Valid {
		return nil
	}
	prior, err := bugDeliveryReviewByDigestTx(ctx, tx, r.SessionID, priorDigest.String)
	if err != nil {
		return err
	}
	if prior.Evidence.Child.ID != r.Evidence.Child.ID {
		return nil
	}
	// A changed presentation cannot silently drop pending discussion or earlier
	// clarification history while reusing the very same check evidence.
	for _, c := range prior.Presentation {
		if !seen[c.CommentID] {
			return errors.New("bug presentation omits earlier clarification")
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT reference,content FROM bug_events WHERE session_id=? AND kind='delivery_reply'`, r.SessionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var reference string
		var content []byte
		if err = rows.Scan(&reference, &content); err != nil {
			return err
		}
		var c bugDeliveryReplyEventContent
		if err = json.Unmarshal(content, &c); err != nil {
			return err
		}
		if c.Digest != priorDigest.String || c.Kind == "approved" {
			continue
		}
		id, err := strconv.ParseInt(reference, 10, 64)
		if err != nil {
			return err
		}
		if !seen[id] {
			return errors.New("bug presentation omits unresolved feedback")
		}
	}
	return rows.Err()
}

// BugDeliveryDiscussion supplies retained text; the factory may select relevant
// existing evidence sections as a response. No new cause or remedy can be added.
func (d *DB) BugDeliveryDiscussion(ctx context.Context, session int64, digest string) ([]BugDeliveryClarification, error) {
	var out []BugDeliveryClarification
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, e := bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		out = append(out, r.Presentation...)
		rows, e := tx.QueryContext(ctx, `SELECT reference,content FROM bug_events WHERE session_id=? AND kind='delivery_reply' ORDER BY reference`, session)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var reference string
			var content []byte
			if e = rows.Scan(&reference, &content); e != nil {
				return e
			}
			var c bugDeliveryReplyEventContent
			if e = json.Unmarshal(content, &c); e != nil {
				return e
			}
			if c.Digest != digest || (c.Kind != "clarify" && c.Kind != "discussion") {
				continue
			}
			id, e := strconv.ParseInt(reference, 10, 64)
			if e != nil {
				return e
			}
			out = append(out, BugDeliveryClarification{ReviewDigest: digest, CommentID: id, Body: c.Body})
		}
		return rows.Err()
	})
	return out, err
}

func bugDeliveryWaitTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview, in GraphOwnerReply) error {
	var session int64
	var workflow, status, waitState string
	err := tx.QueryRowContext(ctx, `SELECT i.session_id,d.workflow,i.status,w.state FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE w.delivery_key=? AND w.instance_id=?`, r.DeliveryKey, in.InstanceID).Scan(&session, &workflow, &status, &waitState)
	if err != nil {
		return err
	}
	if session != r.SessionID || workflow != "bug" || status != "running" || waitState != "waiting" {
		return errors.New("bug delivery reply has no active factory owner wait")
	}
	return nil
}

// BugMergeBinding snapshots the two authorizations and their full evidence chain.
type BugMergeBinding struct {
	Review   BugDeliveryReview
	Approval BugDeliveryDecision
}

// BugIntegrationObservation contains independent forge facts, never response-only proof.
type BugIntegrationObservation struct {
	At           time.Time
	PR           BugPullRequest
	MergeCommit  string
	Parents      []string
	Main         string
	MergedAt     time.Time
	RequestError string
	BindingError string
	Kind         string
}

// BugMergeOperation separates dispatch, observed integration and closure eligibility.
type BugMergeOperation struct {
	SessionID                  int64
	Operation, ReviewDigest    string
	Binding                    BugMergeBinding
	Dispatched                 bool
	Outcome, Reason, Exception string
	Observations               []BugIntegrationObservation
}

func bugMergeTx(ctx context.Context, tx *sql.Tx, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	var raw []byte
	var op, reviewDigest sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT merge_operation,merge_review_digest,merge_binding,merge_dispatched,merge_outcome,merge_reason,merge_exception FROM bug_delivery WHERE session_id=?`, session).Scan(&op, &reviewDigest, &raw, &out.Dispatched, &out.Outcome, &out.Reason, &out.Exception)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (!op.Valid || op.String != operation)) {
		content, e := bugEventByReferenceTx(ctx, tx, session, "merge", operation)
		if e != nil {
			return out, e
		}
		e = json.Unmarshal(content, &out)
		return out, e
	}
	if err != nil {
		return out, err
	}
	out.SessionID, out.Operation, out.ReviewDigest = session, op.String, reviewDigest.String
	if err = json.Unmarshal(raw, &out.Binding); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='merge_observation' AND reference LIKE ? ORDER BY id`, session, operation+":%")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var o BugIntegrationObservation
		var content []byte
		if err = rows.Scan(&content); err != nil {
			return out, err
		}
		if err = json.Unmarshal(content, &o); err != nil {
			return out, err
		}
		out.Observations = append(out.Observations, o)
	}
	return out, rows.Err()
}

// GetBugMerge retrieves a merge operation without inferring remote success.
func (d *DB) GetBugMerge(ctx context.Context, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = bugMergeTx(ctx, tx, session, operation); return e })
	return out, err
}

// bugMergeAuthorityTx excludes only this operation's own save guard while
// validating domain evidence. Removal and restoration occur in one transaction,
// so no concurrent accessor can observe an unlocked task.
func bugMergeAuthorityTx(ctx context.Context, tx *sql.Tx, m BugMergeOperation) (BugMergeBinding, error) {
	owner := "merge:" + m.Operation
	var guard sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT guard_owner FROM bug_tasks WHERE session_id=?`, m.SessionID).Scan(&guard)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return BugMergeBinding{}, err
	}
	if guard.String == owner {
		if err = releaseBugWorkOwnerTx(ctx, tx, m.SessionID, owner); err != nil {
			return BugMergeBinding{}, err
		}
	}
	r, a, err := approvedBugDeliveryTx(ctx, tx, m.SessionID, m.ReviewDigest)
	if err != nil {
		return BugMergeBinding{}, err
	}
	if guard.String == owner {
		if _, err = tx.ExecContext(ctx, `UPDATE bug_tasks SET guard_owner=?,guard_kind='save' WHERE session_id=?`, owner, m.SessionID); err != nil {
			return BugMergeBinding{}, err
		}
	}
	return BugMergeBinding{Review: r, Approval: a}, nil
}

// EnsureBugMerge retains immutable intent before any merge-related forge read.
func (d *DB) EnsureBugMerge(ctx context.Context, session int64, operation, digest string) (BugMergeOperation, error) {
	if session <= 0 || operation == "" || len(operation) > 128 || strings.ContainsAny(operation, "\x00\r\n") || len(digest) != 64 {
		return BugMergeOperation{}, errors.New("invalid Bug merge input")
	}
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		old, e := bugMergeTx(ctx, tx, session, operation)
		if e == nil {
			if old.ReviewDigest != digest {
				return errors.New("bug merge input identity conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		out = BugMergeOperation{SessionID: session, Operation: operation, ReviewDigest: digest, Outcome: "pending"}
		out.Binding.Review, e = bugDeliveryReviewByDigestTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		// Preserve historical authorization, including its absence, for external
		// integration reconciliation. Intent itself grants no dispatch authority.
		var approvalComment sql.NullInt64
		if e = tx.QueryRowContext(ctx, `SELECT approval_comment_id FROM bug_delivery WHERE session_id=? AND approval_digest=?`, session, digest).Scan(&approvalComment); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if approvalComment.Valid {
			var owner int64
			var body, at string
			if e = tx.QueryRowContext(ctx, `SELECT approval_owner_id,approval_body,approval_at FROM bug_delivery WHERE session_id=?`, session).Scan(&owner, &body, &at); e != nil {
				return e
			}
			out.Binding.Approval = BugDeliveryDecision{SessionID: session, CommentID: approvalComment.Int64, OwnerID: owner, Digest: digest, Kind: "approved", Body: body}
			out.Binding.Approval.At, e = time.Parse(time.RFC3339Nano, at)
			if e != nil {
				return e
			}
		}
		var unresolved int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_delivery WHERE session_id=? AND merge_operation IS NOT NULL AND merge_operation!='' AND (merge_dispatched=1 OR merge_exception!='' OR merge_outcome IN ('pending','confirmed'))`, session).Scan(&unresolved); e != nil {
			return e
		}
		if unresolved != 0 {
			return errors.New("previous Bug merge effect remains unresolved")
		}

		raw, e := json.Marshal(out.Binding)
		if e != nil {
			return e
		}
		var priorOperation sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT merge_operation FROM bug_delivery WHERE session_id=?`, session).Scan(&priorOperation); e != nil {
			return e
		}
		if priorOperation.Valid {
			prior, e := bugMergeTx(ctx, tx, session, priorOperation.String)
			if e != nil {
				return e
			}
			if e = bugEventJSONTx(ctx, tx, session, "merge", prior.Operation, prior); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET merge_operation=?,merge_review_digest=?,merge_binding=?,merge_dispatched=0,merge_outcome='pending',merge_reason='',merge_exception='',merge_retry_continuation=NULL,merge_retry_evidence=NULL WHERE session_id=?`, operation, digest, raw, session); e != nil {
			return e
		}
		return bugEventTx(ctx, tx, session, "merge_intent", operation, 0, "", nil)
	})
	return out, err
}

// BugMergePrerequisites revalidates current local authority without changing it.
func (d *DB) BugMergePrerequisites(ctx context.Context, session int64, operation string) (BugMergeOperation, int64, error) {
	var out BugMergeOperation
	var revision int64
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		binding, e := bugMergeAuthorityTx(ctx, tx, out)
		if e != nil {
			return e
		}
		if binding.Review.Digest != out.Binding.Review.Digest || binding.Approval.CommentID != out.Binding.Approval.CommentID {
			return errors.New("bug merge authorization changed")
		}
		s, e := bugExecutionTx(ctx, tx, session)
		revision = s.Revision
		return e
	})
	return out, revision, err
}

// ClaimBugMerge records one possible remote mutation and excludes repair/check/save work.
func (d *DB) ClaimBugMerge(ctx context.Context, session int64, operation string, expected int64) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if m.Dispatched || m.Outcome != "pending" || m.Exception != "" {
			return nil
		}
		current, e := bugMergeAuthorityTx(ctx, tx, m)
		if e != nil {
			return e
		}
		if current.Approval.CommentID != m.Binding.Approval.CommentID || current.Review.Digest != m.Binding.Review.Digest {
			return errors.New("bug merge intent does not bind current approval")
		}

		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		locked, e := claimBugWorkTx(ctx, tx, session, "merge:"+operation, "save")
		if e != nil {
			return e
		}
		if !locked {
			return ErrBugWorkActive
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET merge_dispatched=1 WHERE session_id=?`, session); e != nil {
			return e
		}
		claimed = true
		return bugEventTx(ctx, tx, session, "merge_dispatch", operation, 0, "", nil)
	})
	return claimed, err
}

func validBugMergeSHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

// BugIntegrationProof derives confirmation and exceptions from independent facts.
func BugIntegrationProof(m BugMergeOperation, o BugIntegrationObservation) (string, string) {
	r := m.Binding.Review
	p := o.PR
	want := r.PR
	t := r.Evidence.Child.Request.Binding.TargetCommit
	if p.ID != want.ID || p.Number != want.Number || p.CreatorID != want.CreatorID || p.HeadRepo != want.HeadRepo || p.BaseRepo != want.BaseRepo || p.Branch != want.Branch || p.Target != "main" || p.Head != want.Head {
		return "blocked", "remote PR differs from approved identity, head or target"
	}
	if !p.Merged {
		return "pending", "merge has not been independently observed"
	}
	if p.State != "closed" || !validBugMergeSHA(o.MergeCommit) {
		return "pending", "merged PR lacks complete commit observations"
	}
	if o.MergedAt.IsZero() || m.Binding.Approval.At.IsZero() || r.Evidence.Approval.ApprovedAt.IsZero() || !m.Binding.Approval.At.Before(o.MergedAt) || !r.Evidence.Approval.ApprovedAt.Before(o.MergedAt) {
		return "blocked", "both exact approvals must precede observed integration"
	}
	if len(o.Parents) == 0 || o.Main == "" {
		return "pending", "merge commit or main observation is missing"
	}
	if len(o.Parents) != 2 || o.Parents[0] != t || o.Parents[1] != want.Head {
		return "blocked", "integration used unexpected target or merge shape"
	}
	if o.Main != o.MergeCommit {
		return "blocked", "main moved away from merge commit during confirmation"
	}
	if o.BindingError != "" {
		return "blocked", o.BindingError
	}
	if m.Exception != "" {
		return "blocked", m.Exception
	}
	return "confirmed", ""
}

// ObserveBugMerge appends facts and derives confirmation atomically. Exceptions
// are sticky; subsequent ancestry or newer observations cannot waive them.
func (d *DB) ObserveBugMerge(ctx context.Context, session int64, operation string, o BugIntegrationObservation) (BugMergeOperation, error) {
	if o.At.IsZero() || len(o.RequestError) > 8192 || len(o.BindingError) > 8192 || len(o.Parents) > 64 {
		return BugMergeOperation{}, errors.New("invalid Bug merge observation")
	}
	switch o.Kind {
	case "request", "request_refused", "precheck", "integration", "unavailable":
	default:
		return BugMergeOperation{}, errors.New("invalid Bug merge observation kind")
	}
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if e = bugEventJSONTx(ctx, tx, session, "merge_observation", fmt.Sprintf("%s:%d", operation, len(m.Observations)+1), o); e != nil {
			return e
		}
		outcome, reason := m.Outcome, m.Reason
		exception := m.Exception
		if m.Outcome != "confirmed" || o.Kind == "integration" {
			if o.Kind == "request_refused" {
				outcome, reason = "failed", o.RequestError
			}
			if o.Kind == "integration" {
				outcome, reason = BugIntegrationProof(m, o)
				if m.Outcome == "failed" && !o.PR.Merged {
					outcome, reason = "failed", m.Reason
				}
				if m.Outcome == "confirmed" && outcome == "pending" {
					outcome, reason = "confirmed", m.Reason
				}
				if outcome == "confirmed" {
					if _, e = bugMergeAuthorityTx(ctx, tx, m); e != nil {
						outcome, reason = "blocked", "integration observed but current authorization is no longer applicable"
					}
				}
				if outcome == "blocked" && o.PR.Merged {
					exception = reason
				}
			} else if o.Kind == "precheck" && o.Main != "" && o.Main != m.Binding.Review.Evidence.Child.Request.Binding.TargetCommit {
				outcome, reason = "stale", "main changed before merge dispatch"
			}
			if o.RequestError != "" {
				reason = o.RequestError
			}
			if exception != "" {
				outcome = "blocked"
			}
			if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET merge_outcome=?,merge_reason=?,merge_exception=? WHERE session_id=?`, outcome, reason, exception, session); e != nil {
				return e
			}
			knownMerged := o.PR.Merged && o.PR.ID == m.Binding.Review.PR.ID && o.PR.Number == m.Binding.Review.PR.Number && o.PR.BaseRepo == m.Binding.Review.PR.BaseRepo && o.PR.HeadRepo == m.Binding.Review.PR.HeadRepo
			if outcome == "confirmed" || outcome == "failed" || outcome == "blocked" && knownMerged {
				if e = releaseBugWorkOwnerTx(ctx, tx, session, "merge:"+operation); e != nil {
					return e
				}
			}
		}
		out, e = bugMergeTx(ctx, tx, session, operation)
		return e
	})
	return out, err
}

// ConfirmedBugIntegration is the closure handoff. It introduces no fresh remote
// target-tip rule and cannot turn a receipt or externally merged PR into authority.
func (d *DB) ConfirmedBugIntegration(ctx context.Context, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = confirmedBugIntegrationTx(ctx, tx, session, operation)
		return e
	})
	return out, err
}

func confirmedBugIntegrationTx(ctx context.Context, tx *sql.Tx, session int64, operation string) (BugMergeOperation, error) {
	var out BugMergeOperation
	err := func() error {
		var e error
		out, e = bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if out.Outcome != "confirmed" || out.Exception != "" {
			return errors.New("bug integration is not confirmed without exceptions")
		}
		if _, e = bugMergeAuthorityTx(ctx, tx, out); e != nil {
			return e
		}
		for _, o := range out.Observations {
			if o.Kind == "integration" {
				if status, _ := BugIntegrationProof(out, o); status == "confirmed" {
					return nil
				}
			}
		}
		return errors.New("bug integration proof is missing")
	}()
	return out, err
}

// StopBugMerge retains pre-dispatch refusal without hiding dispatched uncertainty.
func (d *DB) StopBugMerge(ctx context.Context, session int64, operation, outcome, reason string) (BugMergeOperation, error) {
	if (outcome != "stale" && outcome != "blocked" && outcome != "failed") || reason == "" || len(reason) > 8192 {
		return BugMergeOperation{}, errors.New("invalid Bug merge stop")
	}
	var out BugMergeOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if m.Dispatched || m.Outcome == "confirmed" {
			out = m
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET merge_outcome=?,merge_reason=? WHERE session_id=?`, outcome, reason, session); e != nil {
			return e
		}
		if e = bugEventTx(ctx, tx, session, "merge_stop", operation, 0, "", []byte(bugJSONString(reason))); e != nil {
			return e
		}
		out, e = bugMergeTx(ctx, tx, session, operation)
		return e
	})
	return out, err
}

// PrepareBugMergeRetry admits an explicit factory continuation only after a
// retained definitive refusal and independent open-PR reconciliation. It never
// starts a mutation and cannot resolve a transport timeout as nonexecution.
func (d *DB) PrepareBugMergeRetry(ctx context.Context, session int64, operation, continuation, evidence string, expected int64) error {
	if continuation == "" || len(continuation) > 128 || evidence == "" || len(evidence) > 8192 {
		return errors.New("invalid Bug merge retry continuation")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var old sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT merge_retry_evidence FROM bug_delivery WHERE session_id=? AND merge_operation=? AND merge_retry_continuation=?`, session, operation, continuation).Scan(&old); err == nil {
			if old.String != evidence {
				return errors.New("bug merge retry identity conflict")
			}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		m, e := bugMergeTx(ctx, tx, session, operation)
		if e != nil {
			return e
		}
		if m.Outcome != "failed" || !m.Dispatched || m.Exception != "" {
			return errors.New("bug merge retry requires a resolved definitive refusal")
		}
		refused, reconciled := false, false
		for _, o := range m.Observations {
			if o.Kind == "request" {
				refused, reconciled = false, false
			}
			if o.Kind == "request_refused" {
				refused, reconciled = true, false
			}
			if o.Kind == "integration" && refused && !o.PR.Merged && o.PR.State == "open" && o.PR.ID == m.Binding.Review.PR.ID {
				reconciled = true
			}
		}
		if !refused || !reconciled {
			return errors.New("bug merge retry lacks independent refusal reconciliation")
		}
		b, e := bugMergeAuthorityTx(ctx, tx, m)
		if e != nil {
			return e
		}
		if b.Approval.CommentID != m.Binding.Approval.CommentID {
			return errors.New("bug merge retry authorization changed")
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET merge_dispatched=0,merge_outcome='pending',merge_reason='',merge_retry_continuation=?,merge_retry_evidence=? WHERE session_id=?`, continuation, evidence, session); e != nil {
			return e
		}
		return bugEventTx(ctx, tx, session, "merge_retry", continuation, 0, "", []byte(bugJSONString(evidence)))
	})
}

// BugClosureObservation excludes untrusted issue prose; state is a remote fact.
type BugClosureObservation struct {
	Kind, Endpoint, State, Reason string
	ID                            int64
	Number                        int
	At                            time.Time
}

// BugClosureOperation preserves historical confirmation even after an observed reopen.
type BugClosureOperation struct {
	SessionID                           int64
	Operation, MergeOperation, Endpoint string
	Integration                         BugMergeOperation
	Dispatched, Confirmed, Conflict     bool
	Outcome, Reason                     string
	Observations                        []BugClosureObservation
}

func bugClosureTx(ctx context.Context, tx *sql.Tx, session int64) (BugClosureOperation, error) {
	var out BugClosureOperation
	var raw []byte
	var op, merge, endpoint sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT close_operation,close_merge_operation,close_endpoint,close_binding,close_dispatched,close_confirmed,close_outcome,close_reason,close_conflict FROM bug_delivery WHERE session_id=?`, session).Scan(&op, &merge, &endpoint, &raw, &out.Dispatched, &out.Confirmed, &out.Outcome, &out.Reason, &out.Conflict)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !op.Valid) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.SessionID, out.Operation, out.MergeOperation, out.Endpoint = session, op.String, merge.String, endpoint.String
	if err = json.Unmarshal(raw, &out.Integration); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind='close_observation' ORDER BY id`, session)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var o BugClosureObservation
		var content []byte
		if err = rows.Scan(&content); err != nil {
			return out, err
		}
		if err = json.Unmarshal(content, &o); err != nil {
			return out, err
		}
		out.Observations = append(out.Observations, o)
	}
	return out, rows.Err()
}

// GetBugClosure retrieves the retained closure operation and its observations.
func (d *DB) GetBugClosure(ctx context.Context, session int64) (BugClosureOperation, error) {
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = bugClosureTx(ctx, tx, session); return e })
	return out, err
}

// EnsureBugClosure retains identity before a forge read, even if subsequent
// authorization checks block. Endpoint comes from factory forge configuration.
func (d *DB) EnsureBugClosure(ctx context.Context, session int64, operation, merge, endpoint string) (BugClosureOperation, error) {
	if operation == "" || len(operation) > 128 || merge == "" || len(merge) > 128 {
		return BugClosureOperation{}, errors.New("invalid Bug closure identity")
	}
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		old, e := bugClosureTx(ctx, tx, session)
		if e == nil {
			if old.Operation != operation || old.MergeOperation != merge || old.Endpoint != endpoint {
				return errors.New("bug closure identity conflict")
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		m, e := bugMergeTx(ctx, tx, session, merge)
		if e != nil {
			return e
		}
		id := m.Binding.Review.Evidence.Identity
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, fmt.Sprintf("/repos/%s/issues/%d", id.Repository, id.IssueNumber)) {
			return errors.New("invalid Bug closure endpoint")
		}
		raw, e := json.Marshal(m)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET close_operation=?,close_merge_operation=?,close_endpoint=?,close_binding=? WHERE session_id=?`, operation, merge, endpoint, raw, session); e != nil {
			return e
		}
		out, e = bugClosureTx(ctx, tx, session)
		return e
	})
	return out, err
}

func bugClosureGuardTx(ctx context.Context, tx *sql.Tx, c BugClosureOperation) error {
	if c.Integration.Outcome != "confirmed" || c.Integration.Exception != "" {
		return errors.New("bug closure intent lacks confirmed authorized integration")
	}
	m, e := confirmedBugIntegrationTx(ctx, tx, c.SessionID, c.MergeOperation)
	if e != nil {
		return e
	}
	if c.Conflict || m.Binding.Review.Digest != c.Integration.Binding.Review.Digest || m.Binding.Approval.CommentID != c.Integration.Binding.Approval.CommentID {
		return errors.New("bug closure integration binding changed or issue reopened")
	}
	return nil
}

// BugClosurePrerequisites checks the confirmed authorization and integration chain.
func (d *DB) BugClosurePrerequisites(ctx context.Context, session int64) (int64, error) {
	var revision int64
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		revision = s.Revision
		return e
	})
	return revision, err
}

// ClaimBugClosure atomically reserves the remote effect under the current evidence revision.
func (d *DB) ClaimBugClosure(ctx context.Context, session, expected int64) (bool, error) {
	claimed := false
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if c.Dispatched || c.Confirmed || c.Conflict {
			return nil
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		if len(c.Observations) == 0 {
			return errors.New("bug closure requires independent open observation")
		}
		o := c.Observations[len(c.Observations)-1]
		if o.Kind != "issue" || o.State != "open" || !validBugClosureObservation(c, o) {
			return errors.New("bug closure lacks matching open issue")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET close_dispatched=1 WHERE session_id=?`, session); e != nil {
			return e
		}
		claimed = true
		return bugEventTx(ctx, tx, session, "close_dispatch", c.Operation, 0, "", nil)
	})
	return claimed, err
}

func validBugClosureObservation(c BugClosureOperation, o BugClosureObservation) bool {
	i := c.Integration.Binding.Review.Evidence.Identity
	return o.Endpoint == c.Endpoint && o.ID == i.IssueID && int64(o.Number) == i.IssueNumber && (o.State == "open" || o.State == "closed")
}

// ObserveBugClosure atomically retains the observation and current outcome.
func (d *DB) ObserveBugClosure(ctx context.Context, session int64, o BugClosureObservation) (BugClosureOperation, error) {
	if o.At.IsZero() || len(o.Reason) > 8192 || len(o.Endpoint) > 8192 {
		return BugClosureOperation{}, errors.New("invalid Bug closure observation")
	}
	switch o.Kind {
	case "issue", "request", "request_refused", "unavailable", "blocked":
	default:
		return BugClosureOperation{}, errors.New("invalid Bug closure observation kind")
	}
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if e = bugEventJSONTx(ctx, tx, session, "close_observation", fmt.Sprint(len(c.Observations)+1), o); e != nil {
			return e
		}
		outcome, reason := c.Outcome, c.Reason
		confirmed, conflict := c.Confirmed, c.Conflict
		switch o.Kind {
		case "request_refused":
			outcome, reason = "failed", o.Reason
		case "request", "unavailable":
			outcome, reason = "pending", o.Reason
		case "blocked":
			outcome, reason = "blocked", o.Reason
		case "issue":
			if !validBugClosureObservation(c, o) {
				outcome, reason = "blocked", "issue observation does not match recorded repository and numeric identity"
				break
			}
			hadClosed := c.Confirmed
			for _, old := range c.Observations {
				if old.Kind == "issue" && old.State == "closed" && validBugClosureObservation(c, old) {
					hadClosed = true
				}
			}
			if o.State == "open" && hadClosed {
				conflict = true
				outcome, reason = "pending", "issue reopened after an observed closure; operator reconciliation required"
				break
			}
			if e = bugClosureGuardTx(ctx, tx, c); e != nil {
				outcome, reason = "blocked", "authorized integration is not currently applicable"
				break
			}
			if o.State == "closed" {
				confirmed = true
				outcome, reason = "confirmed", ""
			} else if c.Outcome != "failed" {
				outcome, reason = "pending", "issue remains open"
			}
		}
		if conflict {
			outcome, reason = "pending", "issue reopened after an observed closure; operator reconciliation required"
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET close_confirmed=?,close_conflict=?,close_outcome=?,close_reason=? WHERE session_id=?`, confirmed, conflict, outcome, reason, session); e != nil {
			return e
		}
		out, e = bugClosureTx(ctx, tx, session)
		return e
	})
	return out, err
}

// ConfirmedBugClosure supplies lifecycle evidence without repeating merge or close.
func (d *DB) ConfirmedBugClosure(ctx context.Context, session int64) (BugClosureOperation, error) {
	var out BugClosureOperation
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if !out.Confirmed || out.Conflict || out.Outcome != "confirmed" {
			return errors.New("bug closure is not confirmed")
		}
		return bugClosureGuardTx(ctx, tx, out)
	})
	return out, err
}

// PrepareBugClosureRetry records an explicit continuation after definitive
// refusal and a matching open read, never from open state or a timeout alone.
func (d *DB) PrepareBugClosureRetry(ctx context.Context, session int64, continuation, evidence string, expected int64) error {
	if continuation == "" || len(continuation) > 128 || evidence == "" || len(evidence) > 8192 {
		return errors.New("invalid Bug closure retry")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var old sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT close_retry_evidence FROM bug_delivery WHERE session_id=? AND close_retry_continuation=?`, session, continuation).Scan(&old); err == nil {
			if old.String != evidence {
				return errors.New("bug closure retry conflict")
			}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c, e := bugClosureTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if c.Outcome != "failed" || !c.Dispatched || c.Confirmed || c.Conflict {
			return errors.New("bug closure retry requires definitive nonapplication")
		}
		refused, reconciled := false, false
		for _, o := range c.Observations {
			if o.Kind == "request" {
				refused, reconciled = false, false
			}
			if o.Kind == "request_refused" {
				refused, reconciled = true, false
			}
			if o.Kind == "issue" && o.State == "open" && validBugClosureObservation(c, o) && refused {
				reconciled = true
			}
		}
		if !refused || !reconciled {
			return errors.New("bug closure refusal is not reconciled")
		}
		if e = bugClosureGuardTx(ctx, tx, c); e != nil {
			return e
		}
		s, e := bugExecutionTx(ctx, tx, session)
		if e != nil {
			return e
		}
		if s.Revision != expected {
			return ErrBugRevisionConflict
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_delivery SET close_dispatched=0,close_outcome='pending',close_reason='',close_retry_continuation=?,close_retry_evidence=? WHERE session_id=?`, continuation, evidence, session)
		return e
	})
}
