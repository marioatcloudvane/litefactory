package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/bugrepair"
)

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
	out.Child, err = scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=? AND session_id=?`, callID, session))
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
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM bug_work_guards WHERE session_id=?)+(SELECT count(*) FROM bug_repairs WHERE session_id=? AND state IN ('reserved','dispatched'))`, session, session).Scan(&active); err != nil {
		return out, err
	}
	if active != 0 {
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
	var at string
	err = tx.QueryRowContext(ctx, `SELECT review,analysis_revision,analysis_digest,reply_id,owner_id,approved_at FROM bug_analysis_approvals WHERE session_id=? AND review=?`, session, r.ApprovalReview).Scan(&out.Approval.Review, &out.Approval.AnalysisRevision, &out.Approval.AnalysisDigest, &out.Approval.ReplyID, &out.Approval.OwnerID, &at)
	if err != nil {
		return out, err
	}
	out.Approval.ApprovedAt, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return out, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT session_id,request,result_id,result_revision,source_commit,source_tree,save_operation,evidence FROM bug_repair_completions WHERE session_id=? AND request=?`, session, r.Request).Scan(&out.Repair.SessionID, &out.Repair.Request, &out.Repair.ResultID, &out.Repair.ResultRevision, &out.Repair.SourceCommit, &out.Repair.SourceTree, &out.Repair.SaveOperation, &raw)
	if err != nil {
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
func BugDeliveryReviewDigest(e BugDeliveryEvidence, p BugPullRequest, presentation ...BugDeliveryClarification) (string, error) {
	raw, err := json.Marshal(struct {
		Evidence     BugDeliveryEvidence
		PR           BugPullRequest
		Presentation []BugDeliveryClarification
	}{e, p, presentation})
	if err != nil {
		return "", err
	}
	return submissionDigest(raw), nil
}
func bugDeliveryReviewTx(ctx context.Context, tx *sql.Tx, session int64, digest string) (BugDeliveryReview, error) {
	var r BugDeliveryReview
	var e, p, presentation []byte
	err := tx.QueryRowContext(ctx, `SELECT session_id,digest,evidence,pr,presentation,body,content_digest,delivery_key,bot_id,comment_id,active,reason FROM bug_delivery_reviews WHERE session_id=? AND digest=?`, session, digest).Scan(&r.SessionID, &r.Digest, &e, &p, &presentation, &r.Body, &r.ContentDigest, &r.DeliveryKey, &r.BotID, &r.CommentID, &r.Active, &r.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(e, &r.Evidence); err != nil {
		return r, err
	}
	if err = json.Unmarshal(p, &r.PR); err != nil {
		return r, err
	}
	err = json.Unmarshal(presentation, &r.Presentation)
	return r, err
}

// GetBugDeliveryReview retrieves a historical or active immutable revision.
func (d *DB) GetBugDeliveryReview(ctx context.Context, session int64, digest string) (BugDeliveryReview, error) {
	var r BugDeliveryReview
	err := d.withTx(ctx, func(tx *sql.Tx) error { var e error; r, e = bugDeliveryReviewTx(ctx, tx, session, digest); return e })
	return r, err
}

func bugDeliveryGuardTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview, expected int64) error {
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
	want, err := BugDeliveryReviewDigest(current, r.PR, r.Presentation...)
	if err != nil {
		return err
	}
	if want != r.Digest {
		return errors.New("bug delivery evidence changed")
	}
	var cancelled bool
	if err = tx.QueryRowContext(ctx, `SELECT cancelled FROM bug_pr_intents WHERE session_id=?`, r.SessionID).Scan(&cancelled); err != nil {
		return err
	}
	if cancelled {
		return errors.New("bug delivery cancelled")
	}
	return nil
}
