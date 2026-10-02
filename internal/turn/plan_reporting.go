package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// ReportPlanNotices recovers stable semantic deliveries without inventing a
// graph attempt or consuming parent discussion. Uncertain receipts remain debt
// while later independent notices and input can be serviced.
func (r *Reporter) ReportPlanNotices(ctx context.Context, parent naming.Ref, sessionID int64) (int, error) {
	if err := r.db.CapturePlanStoryNotices(ctx, parent.String()); err != nil {
		return 0, err
	}
	notices, err := r.db.PendingPlanStoryNotices(ctx, parent.String())
	if err != nil {
		return 0, err
	}
	pending := 0
	for _, notice := range notices {
		if err = r.db.AttemptPlanStoryNotice(ctx, parent.String(), notice.ID); err != nil {
			return pending, err
		}
		purpose := fmt.Sprintf("plan-observation:%d", notice.ID)
		intent := r.deliveryIntent(parent, parent, sessionID, 0, 0, purpose)
		intent.TriggerNamespace = "plan-observation"
		_, err = r.deliver(ctx, intent, func() (redact.Body, error) {
			return r.renderer.RenderReportDelivery(ctx, report.PlanStoryNoticeText(notice.State), "")
		})
		if errors.Is(err, ErrReportReceiptUncertain) {
			pending++
			continue
		}
		if err != nil {
			return pending, err
		}
	}
	return pending, nil
}

// ReportPlanReply persists one immutable explanation per original child input.
// Store completion advances only the child cursor and only after confirmation.
// The explanation is posted on the sub-issue the owner replied on, because that
// is where the owner acts. The delivery key keeps its original identity, so an
// explanation retained before this destination existed resumes unchanged.
func (r *Reporter) ReportPlanReply(ctx context.Context, parent naming.Ref, sessionID int64, key string, child int, commentID int64, reason string) (Posted, error) {
	intent := r.deliveryIntent(parent, parent, sessionID, commentID, 0, "plan-reply")
	intent.TriggerNamespace = "plan-reply"
	if child > 0 {
		destination := parent
		destination.Issue = child
		intent.Destination = destination.String()
	}
	explanation, err := r.db.RetainPlanReplyExplanation(ctx, store.PlanReplyExplanation{DeliveryKey: intent.Key, Parent: parent.String(), Key: key, ChildNumber: child, CommentID: commentID, Reason: reason})
	if err != nil {
		return Posted{}, err
	}
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		return r.renderer.RenderReportDelivery(ctx, report.PlanReplyExplanationText(explanation.Key, explanation.ChildNumber, explanation.CommentID, explanation.Reason), "")
	})
}
