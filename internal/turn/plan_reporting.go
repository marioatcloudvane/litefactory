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
			text := report.PlanStoryNoticeText(notice.State)
			held := len(notice.State.RequestHolds) > 0 || notice.State.Hold != "" || notice.State.LifecycleHold != "" || notice.State.Effect != "" || notice.State.EffectHold != ""
			if held {
				draft, readErr := r.db.GetPlanStoryDraft(ctx, parent.String(), notice.State.Key)
				switch {
				case errors.Is(readErr, store.ErrNotFound), readErr == nil && (draft.IssueID < 1 || draft.IssueNumber < 1):
					text += "\n\nThe retained story has no confirmed issue binding; its inspection destination is unavailable."
				case readErr != nil:
					return redact.Body{}, readErr
				default:
					display, displayErr := lookupTicketDisplay(ctx, r.forge, parent, draft.IssueNumber)
					if displayErr != nil {
						return redact.Body{}, displayErr
					}
					text += "\n\nAffected story: " + display.Link() + ". Inspect the original bound issue before resolving its reported hold; this link supplies no approval."
				}
			}
			return r.renderer.RenderReportDelivery(ctx, text, "")
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
		display, err := lookupTicketDisplay(ctx, r.forge, parent, explanation.ChildNumber)
		if err != nil {
			return redact.Body{}, err
		}
		return r.renderer.RenderReportDelivery(ctx, report.PlanReplyExplanationText(explanation.Key, explanation.ChildNumber, explanation.CommentID, explanation.Reason, display.Reference()), "")
	})
}
