package turn

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// continueOwner consumes only the published wait's authenticated reply. Domain
// stores establish authority; parent continuation merely selects its checkpoint.
func (r *BugRuntime) continueOwner(ctx context.Context, req Request, life store.BugLifecycle) (store.GraphInstance, error) {
	var zero store.GraphInstance
	if req.Trigger.CommentID <= 0 || store.IsGraphTrigger(req.Trigger.CommentID) || req.Trigger.AuthorID != r.cfg.OwnerID || strings.TrimSpace(req.Trigger.Body) == "" {
		return zero, errors.New("bug continuation requires an authenticated owner comment")
	}
	prior, replayErr := r.opts.DB.GetBugOwnerContinuation(ctx, req.SessionID, req.Trigger.CommentID, req.Trigger.AuthorID, req.Trigger.Body)
	if replayErr == nil {
		return prior, nil
	}
	if !errors.Is(replayErr, store.ErrNotFound) {
		return zero, replayErr
	}
	session, err := r.opts.DB.GetSession(ctx, req.SessionID)
	if err != nil {
		return zero, err
	}
	if strings.HasPrefix(strings.TrimSpace(req.Trigger.Body), "/retry-bug-tests") {
		fields := strings.SplitN(strings.TrimSpace(req.Trigger.Body), " ", 3)
		if len(fields) != 3 || fields[0] != "/retry-bug-tests" || strings.TrimSpace(fields[2]) == "" {
			return zero, errors.New("bug check recovery requires /retry-bug-tests <callid> <resolution>")
		}
		callID, e := strconv.ParseInt(fields[1], 10, 64)
		if e != nil || callID <= 0 {
			return zero, errors.New("invalid Bug test call identity")
		}
		at := r.opts.MCP.Now()
		reply := store.GraphOwnerReply{InstanceID: life.ParentInstance, OwnerID: r.cfg.OwnerID, AuthorID: req.Trigger.AuthorID, CommentID: req.Trigger.CommentID, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Body: req.Trigger.Body, At: at}
		if e = r.opts.DB.RecordBugTestResolution(ctx, req.SessionID, callID, reply); e != nil {
			return zero, e
		}
		return r.opts.DB.ClaimBugContinuation(ctx, store.BugParentContinuation{SessionID: req.SessionID, Operation: fmt.Sprintf("owner-reply:%d", reply.CommentID), Cause: "test-resolved", Reference: strconv.FormatInt(callID, 10), ExpectedRevision: life.Revision, At: at})
	}
	parent, err := r.opts.DB.GetGraphInstance(ctx, life.ParentInstance)
	if err != nil {
		return zero, err
	}
	// A parent that failed in an agent node, or ended on a blocked analysis,
	// has no owner wait. The owner's comment asks for another attempt at that
	// node; the store refuses any other stop, and the lifetime budgets still
	// apply.
	if parent.Status == store.GraphFailed || (parent.Status == store.GraphSucceeded && life.Disposition == "blocked") {
		return r.opts.DB.ClaimBugContinuation(ctx, store.BugParentContinuation{SessionID: req.SessionID, Operation: fmt.Sprintf("owner-reply:%d", req.Trigger.CommentID), Cause: "failure-retry", Reference: strconv.FormatInt(req.Trigger.CommentID, 10), ExpectedRevision: life.Revision, At: r.opts.MCP.Now()})
	}
	wait, err := r.opts.DB.GetGraphOwnerWait(ctx, life.ParentInstance)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, errors.New("this Bug task is not waiting for an owner reply; nothing can be continued from this comment")
		}
		return zero, err
	}
	delivery, err := r.opts.DB.GetReportDelivery(ctx, wait.DeliveryKey)
	if err != nil {
		return zero, err
	}
	if !delivery.Completed || delivery.CommentID <= 0 {
		return zero, errors.New("bug owner wait publication is unconfirmed")
	}
	// A comment written before the open review or question was posted can
	// never answer it (for example a retry request queued behind the run it
	// started). The daemon refuses it once and consumes it, so later
	// comments are not held behind it (#401).
	if req.Trigger.CommentID <= delivery.CommentID && wait.ReplyID != req.Trigger.CommentID {
		return zero, store.ErrGraphReplyPredatesQuestion
	}
	at := r.opts.MCP.Now()
	if wait.ReplyID == req.Trigger.CommentID && !wait.ClaimedAt.IsZero() {
		at = wait.ClaimedAt
	}
	reply := store.GraphOwnerReply{InstanceID: wait.InstanceID, OwnerID: r.cfg.OwnerID, AuthorID: req.Trigger.AuthorID, CommentID: req.Trigger.CommentID, QuestionCommentID: delivery.CommentID, TaskRef: session.TaskRef, SourceRef: session.TaskRef, DeliveryKey: wait.DeliveryKey, Body: req.Trigger.Body, At: at}
	var cause, reference string
	switch life.Disposition {
	case "analysis-review":
		state, _, _, e := r.opts.DB.CurrentBugAnalysis(ctx, req.SessionID)
		if e != nil {
			return zero, e
		}
		review, e := r.opts.DB.GetBugAnalysisReviewForWait(ctx, req.SessionID, wait.DeliveryKey)
		if e != nil {
			return zero, e
		}
		if strings.HasPrefix(strings.TrimSpace(reply.Body), "/approve-bug") {
			if _, e = r.opts.DB.ApproveBugAnalysis(ctx, req.SessionID, review.Review, reply, state.Revision); e != nil {
				return zero, e
			}
			cause, reference = "analysis-approved", review.Review
		} else {
			if e = r.opts.DB.ConsumeBugAnalysisReviewChange(ctx, req.SessionID, review.Review, reply, state.Revision); e != nil {
				return zero, e
			}
			cause, reference = "analysis-change", strconv.FormatInt(reply.CommentID, 10)
		}
	case "question":
		state, analysis, _, e := r.opts.DB.CurrentBugAnalysis(ctx, req.SessionID)
		if e != nil {
			return zero, e
		}
		ids := make([]string, 0, len(analysis.Analysis.Questions))
		for _, q := range analysis.Analysis.Questions {
			ids = append(ids, q.ID)
		}
		if e = r.opts.DB.ConsumeBugAnalysisAnswers(ctx, req.SessionID, reply, ids, state.Revision); e != nil {
			return zero, e
		}
		cause, reference = "analysis-answers", strconv.FormatInt(reply.CommentID, 10)
	case "delivery-review":
		review, e := r.opts.DB.GetBugDeliveryReviewForWait(ctx, req.SessionID, wait.DeliveryKey)
		if e != nil {
			return zero, e
		}
		if review.DeliveryKey != wait.DeliveryKey {
			return zero, errors.New("bug delivery review differs from active wait")
		}
		state, _, _, e := r.opts.DB.CurrentBugAnalysis(ctx, req.SessionID)
		if e != nil {
			return zero, e
		}
		decision, e := r.opts.DB.ApplyBugDeliveryReply(ctx, req.SessionID, review.Digest, reply, state.Revision)
		if e != nil {
			return zero, e
		}
		cause, reference = "delivery-feedback", strconv.FormatInt(reply.CommentID, 10)
		if decision.Kind == "approved" {
			cause, reference = "delivery-approved", review.Digest
		}
	default:
		return zero, fmt.Errorf("bug lifecycle %q has no owner review wait", life.Disposition)
	}
	return r.opts.DB.ClaimBugContinuation(ctx, store.BugParentContinuation{SessionID: req.SessionID, Operation: fmt.Sprintf("owner-reply:%d", reply.CommentID), Cause: cause, Reference: reference, ExpectedRevision: life.Revision, At: at})
}
